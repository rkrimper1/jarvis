// Package server implements the gRPC SecurityService.
package server

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"go.opencensus.io/trace"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	userv1 "github.com/rkrimper1/jarvis/api/pb/user"
	"github.com/rkrimper1/jarvis/api/internal/security/analyticsstore"
	"github.com/rkrimper1/jarvis/api/middleware"
	"github.com/rkrimper1/jarvis/api/internal/security/audit"
	authpkg "github.com/rkrimper1/jarvis/api/internal/security/auth"
	"github.com/rkrimper1/jarvis/api/internal/security/config"
	"github.com/rkrimper1/jarvis/api/internal/security/faceanalysis"
	"github.com/rkrimper1/jarvis/api/internal/security/protocol"
	"github.com/rkrimper1/jarvis/api/internal/security/threat"
	"github.com/rkrimper1/jarvis/api/internal/security/token"
)

// UserStore is the minimal interface the security server needs to verify passwords.
type UserStore interface {
	CheckPassword(ctx context.Context, username string, password []byte) (*userv1.User, error)
}

// SecurityServer implements securityv1.SecurityServiceServer.
type SecurityServer struct {
	securityv1.UnimplementedSecurityServiceServer

	cfg       *config.Config
	auth      *authpkg.Engine
	tokens    *token.Manager
	assessor  *threat.Assessor
	broadcast *threat.AlertBroadcaster
	protocols *protocol.Executor
	auditLog       *audit.Store
	analyticsStore *analyticsstore.Store // nil if not configured
	users          UserStore             // nil if user store not configured
	log            *slog.Logger

	// face analysis
	faceDetector       *faceanalysis.Detector // nil if not configured
	faceAnalyzer       *faceanalysis.Analyzer
	faceOutputDir      string
	faceAnnotateParams faceanalysis.AnnotateParams
	faceMaxImageBytes  int
}

// FaceConfig holds optional face-analysis configuration.
type FaceConfig struct {
	CascadePath      string
	OutputDir        string
	AnthropicKey     string
	ClaudeModel      string
	DetectParams     faceanalysis.DetectParams
	AnnotateParams   faceanalysis.AnnotateParams
	AnalyticsDBPath  string // path to analytics SQLite DB; leave empty to disable
	MaxImageBytes    int    // max accepted image_data size; 0 → default 5 MiB
}

// New wires all dependencies and returns a ready SecurityServer.
func New(cfg *config.Config, log *slog.Logger) *SecurityServer {
	return NewWithUserStore(cfg, log, nil)
}

// NewWithUserStore wires all dependencies including the optional user store.
func NewWithUserStore(cfg *config.Config, log *slog.Logger, users UserStore) *SecurityServer {
	return NewWithUserStoreFace(cfg, log, users, FaceConfig{})
}

// NewWithUserStoreFace wires all dependencies including user store and face analysis config.
func NewWithUserStoreFace(cfg *config.Config, log *slog.Logger, users UserStore, face FaceConfig) *SecurityServer {
	assessor := threat.New()
	broadcaster := threat.NewBroadcaster()

	// Start the background patrol scanner so streaming subscribers always
	// receive data — remove in production and replace with real sensor input.
	broadcaster.SimulatePatrolScan(assessor, cfg.Threat.AlertBroadcastInterval)

	var aStore *analyticsstore.Store
	auditStore := audit.New() // default: in-memory
	if face.AnalyticsDBPath != "" {
		var err error
		aStore, err = analyticsstore.New(face.AnalyticsDBPath, log)
		if err != nil {
			log.Error("analyticsstore: failed to open DB — analytics disabled", slog.Any("err", err))
		} else {
			sqliteAudit, err := audit.NewSQLite(aStore.DB(), log)
			if err != nil {
				log.Error("audit: failed to init SQLite store — falling back to in-memory", slog.Any("err", err))
			} else {
				auditStore = sqliteAudit
			}
		}
	}

	srv := &SecurityServer{
		cfg:              cfg,
		auth:             authpkg.New(),
		tokens:           token.New(cfg.Token.Secret, cfg.Token.AccessTokenTTL, cfg.Token.Issuer),
		assessor:         assessor,
		broadcast:        broadcaster,
		protocols:        protocol.New(log),
		auditLog:         auditStore,
		analyticsStore:   aStore,
		users:            users,
		log:              log,
		faceAnalyzer:       faceanalysis.NewAnalyzer(face.AnthropicKey, face.ClaudeModel),
		faceOutputDir:      face.OutputDir,
		faceAnnotateParams: face.AnnotateParams,
		faceMaxImageBytes:  face.MaxImageBytes,
	}

	if face.CascadePath != "" {
		detector, err := faceanalysis.NewDetector(face.CascadePath, face.DetectParams)
		if err != nil {
			log.Error("faceanalysis: failed to load cascade — face detection disabled", slog.Any("err", err))
		} else {
			srv.faceDetector = detector
		}
	}

	if face.OutputDir != "" {
		// Annotated PNGs accumulate in this directory with no automatic cleanup.
		// Monitor disk usage and prune old files as needed (e.g. a cron job or
		// logrotate-style script targeting files older than your retention window).
		log.Info("faceanalysis: annotated images will be written to disk",
			slog.String("output_dir", face.OutputDir),
		)
	}

	return srv
}

// ── Authenticate ─────────────────────────────────────────────────────

func (s *SecurityServer) Authenticate(
	ctx context.Context,
	req *securityv1.AuthenticateRequest,
) (*securityv1.AuthenticateResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "security/Authenticate")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.SubjectId)

	if req.SubjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "subject_id is required")
	}

	s.log.InfoContext(ctx, "Authenticate",
		slog.String("request_id", req.Meta.RequestId),
		slog.String("subject_id", req.SubjectId),
		slog.String("method", req.Method.String()),
	)

	// ── Try users DB first (bcrypt password check) ───────────────────
	var role string
	var result authpkg.VerifyResult
	if s.users != nil {
		u, err := s.users.CheckPassword(ctx, req.SubjectId, req.CredentialPayload)
		if err == nil {
			// DB user authenticated successfully
			role = u.Role.String()
			result = authpkg.VerifyResult{Valid: true, GrantedScopes: []string{role}}
		} else {
			result = authpkg.VerifyResult{Valid: false, Reason: err.Error()}
		}
	} else {
		// Fall back to hardcoded auth engine
		result = s.auth.Verify(req.SubjectId, req.Method, req.CredentialPayload)
	}

	s.auditLog.Append(req.SubjectId, "authenticate:"+req.Method.String(), "security/auth", result.Valid)

	if !result.Valid {
		s.log.WarnContext(ctx, "authentication failed",
			slog.String("subject_id", req.SubjectId),
			slog.String("reason", result.Reason),
		)
		return &securityv1.AuthenticateResponse{
			Meta:          metaError(req.Meta.RequestId, "AUTH_FAILED", result.Reason),
			Authenticated: false,
		}, nil
	}

	accessToken, expiresAt, err := s.tokens.Issue(req.SubjectId, result.GrantedScopes, role)
	if err != nil {
		s.log.ErrorContext(ctx, "token issue failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "token generation failed")
	}

	s.log.InfoContext(ctx, "authentication successful",
		slog.String("subject_id", req.SubjectId),
		slog.Any("scopes", result.GrantedScopes),
	)

	return &securityv1.AuthenticateResponse{
		Meta:          metaOK(req.Meta.RequestId),
		Authenticated: true,
		AccessToken:   accessToken,
		GrantedScopes: result.GrantedScopes,
		ExpiresAt:     timestamppb.New(expiresAt),
	}, nil
}

// ── AssessThreat ──────────────────────────────────────────────────────

func (s *SecurityServer) AssessThreat(
	ctx context.Context,
	req *securityv1.AssessThreatRequest,
) (*securityv1.AssessThreatResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "security/AssessThreat")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "AssessThreat",
		slog.String("subject_id", req.SubjectId),
		slog.String("location", req.Location),
		slog.Int("signals", len(req.ObservedSignals)),
	)

	result := s.assessor.Assess(req.SubjectId, req.Location, req.ObservedSignals)

	s.auditLog.Append(
		req.SubjectId,
		fmt.Sprintf("threat_assessment:%s", result.Level.String()),
		"security/threat",
		true,
	)

	// Analytics recording is best-effort: a storage failure does not fail the
	// RPC because the threat assessment result has already been computed and
	// returned. Loss of analytics events is acceptable; loss of the response
	// to the caller is not.
	if s.analyticsStore != nil {
		if err := s.analyticsStore.InsertThreat(ctx, analyticsstore.ThreatEvent{
			SubjectID:   req.SubjectId,
			Location:    req.Location,
			Signals:     req.ObservedSignals,
			ThreatLevel: result.Level.String(),
		}); err != nil {
			s.log.WarnContext(ctx, "analyticsstore: insert threat failed", slog.Any("err", err))
		}
	}

	// Broadcast high+ threats to all streaming subscribers
	if result.Level >= securityv1.ThreatLevel_THREAT_LEVEL_HIGH {
		s.broadcast.Publish(result)
	}

	return &securityv1.AssessThreatResponse{
		Meta:               metaOK(req.Meta.RequestId),
		Level:              result.Level,
		Confidence:         result.Confidence,
		ThreatSummary:      result.Summary,
		RecommendedActions: result.RecommendedActions,
	}, nil
}

// ── ExecuteProtocol ───────────────────────────────────────────────────

func (s *SecurityServer) ExecuteProtocol(
	ctx context.Context,
	req *securityv1.ExecuteProtocolRequest,
) (*securityv1.ExecuteProtocolResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "security/ExecuteProtocol")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.Protocol == securityv1.ProtocolType_PROTOCOL_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "protocol must be specified")
	}

	s.log.WarnContext(ctx, "ExecuteProtocol",
		slog.String("protocol", req.Protocol.String()),
		slog.String("reason", req.Reason),
		slog.Bool("requires_confirmation", req.RequiresConfirmation),
	)

	if req.RequiresConfirmation {
		s.auditLog.Append(req.Meta.UserId, "protocol:confirmation_pending:"+req.Protocol.String(), "security/protocol", false)
		return nil, status.Errorf(codes.FailedPrecondition,
			"protocol %s requires explicit confirmation — re-submit with requires_confirmation=false",
			req.Protocol.String(),
		)
	}

	result, err := s.protocols.Execute(req.Protocol, req.Reason)
	if err != nil {
		s.auditLog.Append(req.Meta.UserId, "protocol:failed:"+req.Protocol.String(), "security/protocol", false)
		return nil, status.Errorf(codes.Internal, "protocol execution failed: %v", err)
	}

	s.auditLog.Append(req.Meta.UserId, "protocol:executed:"+req.Protocol.String(), "security/protocol", true)

	return &securityv1.ExecuteProtocolResponse{
		Meta:             metaOK(req.Meta.RequestId),
		ProtocolExecuted: result.Protocol,
		ActionsTaken:     result.ActionsTaken,
	}, nil
}

// ── GetAuditLog ───────────────────────────────────────────────────────

func (s *SecurityServer) GetAuditLog(
	ctx context.Context,
	req *securityv1.GetAuditLogRequest,
) (*securityv1.GetAuditLogResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "security/GetAuditLog")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > s.cfg.Audit.MaxPageSize {
		pageSize = s.cfg.Audit.MaxPageSize
	}

	// Convert proto timestamps (nil-safe)
	from := req.GetFrom().AsTime()
	to := req.GetTo().AsTime()

	s.log.InfoContext(ctx, "GetAuditLog",
		slog.String("subject_id", req.SubjectId),
		slog.Int("page_size", pageSize),
	)

	entries, nextToken, err := s.auditLog.Query(req.SubjectId, from, to, pageSize, req.PageToken)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "query error: %v", err)
	}

	resp := &securityv1.GetAuditLogResponse{
		Meta:          metaOK(req.Meta.RequestId),
		Entries:       entries,
		NextPageToken: nextToken,
	}

	if s.analyticsStore != nil {
		ss, err := s.analyticsStore.ComputeStatus(ctx)
		if err != nil {
			s.log.WarnContext(ctx, "analyticsstore: compute status failed", slog.Any("err", err))
		} else {
			resp.SurroundingsStatus = &securityv1.SurroundingsStatus{
				Score:  ss.Score,
				Color:  ss.Color,
				Status: ss.Status,
			}
		}
	}

	return resp, nil
}

// ── StreamSecurityAlerts ──────────────────────────────────────────────

func (s *SecurityServer) StreamSecurityAlerts(
	req *securityv1.StreamSecurityAlertsRequest,
	stream securityv1.SecurityService_StreamSecurityAlertsServer,
) error {

	// RequestId is intentionally used as the subscriber key here: each streaming
	// call gets a unique per-call UUID, making it a suitable deduplication key
	// for the broadcaster. This is distinct from UserId, which identifies the
	// caller and is not guaranteed unique across concurrent stream connections.
	subscriberID := req.GetMeta().GetRequestId()
	s.log.Info("StreamSecurityAlerts subscriber connected",
		slog.String("subscriber_id", subscriberID),
	)

	ch, unsub := s.broadcast.Subscribe(subscriberID)
	defer unsub()

	for {
		select {
		case <-stream.Context().Done():
			s.log.Info("StreamSecurityAlerts: client disconnected",
				slog.String("subscriber_id", subscriberID),
			)
			return nil

		case result, ok := <-ch:
			if !ok {
				return nil
			}
			resp := &securityv1.StreamSecurityAlertsResponse{
				Meta: metaOK(subscriberID),
				Alert: &securityv1.SecurityAlert{
					Level:   result.Level,
					Message: result.Summary,
				},
			}
			_, alertSpan := trace.StartSpan(stream.Context(), "security/StreamSecurityAlerts/send_alert")
			alertSpan.AddAttributes(trace.StringAttribute("threat_level", result.Level.String()))
			err := stream.Send(resp)
			alertSpan.End()
			if err != nil {
				s.log.Error("StreamSecurityAlerts: send error", slog.Any("err", err))
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
}

// ── AnalyzeFaces ──────────────────────────────────────────────────────

func (s *SecurityServer) AnalyzeFaces(
	ctx context.Context,
	req *securityv1.AnalyzeFacesRequest,
) (*securityv1.AnalyzeFacesResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "security/AnalyzeFaces")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if len(req.ImageData) == 0 {
		return nil, status.Error(codes.InvalidArgument, "image_data is required")
	}
	maxBytes := s.faceMaxImageBytes
	if maxBytes <= 0 {
		maxBytes = 5 * 1024 * 1024 // 5 MiB default
	}
	if len(req.ImageData) > maxBytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"image_data exceeds maximum allowed size of %d bytes", maxBytes)
	}
	if s.faceDetector == nil || s.faceOutputDir == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"face analysis not configured (set FACE_CASCADE_PATH and FACE_OUTPUT_DIR)")
	}

	// Decode image (preserve raw bytes for EXIF orientation correction)
	img, _, err := image.Decode(bytes.NewReader(req.ImageData))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode image: %v", err)
	}
	img = faceanalysis.ApplyOrientation(img, req.ImageData)

	// Detect faces
	dets, err := s.faceDetector.Detect(ctx, img)
	if err != nil {
		s.log.ErrorContext(ctx, "face detect failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "face detection: %v", err)
	}

	s.log.InfoContext(ctx, "AnalyzeFaces",
		slog.Int("faces_detected", len(dets)),
		slog.String("request_id", req.Meta.RequestId),
	)

	// Analyze each face via Claude
	results := make([]faceanalysis.FaceResult, len(dets))
	for i, det := range dets {
		results[i] = s.faceAnalyzer.Analyze(ctx, img, det)
	}

	// Annotate and save
	filename, err := faceanalysis.Annotate(img, dets, results, s.faceOutputDir, s.faceAnnotateParams)
	if err != nil {
		s.log.ErrorContext(ctx, "face annotate failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "annotate image: %v", err)
	}

	// Build response
	faces := make([]*securityv1.FaceAnalysis, len(dets))
	for i, det := range dets {
		faces[i] = &securityv1.FaceAnalysis{
			FaceIndex:  int32(i + 1),
			Sentiment:  results[i].Sentiment,
			Commentary: results[i].Commentary,
			BoundingBox: &securityv1.BoundingBox{
				X: int32(det.X), Y: int32(det.Y),
				Width: int32(det.W), Height: int32(det.H),
			},
		}
	}

	s.auditLog.Append(req.Meta.UserId, fmt.Sprintf("face_analysis:faces=%d", len(dets)), "security/faces", true)

	// Analytics recording is best-effort — see equivalent comment in AssessThreat.
	if s.analyticsStore != nil {
		sentiments := make([]string, len(results))
		for i, r := range results {
			sentiments[i] = r.Sentiment
		}
		if err := s.analyticsStore.InsertFaces(ctx, analyticsstore.FacesEvent{
			Filename:   filename,
			FaceCount:  len(dets),
			Sentiments: sentiments,
			ImagePath:  s.faceOutputDir + "/" + filename,
		}); err != nil {
			s.log.WarnContext(ctx, "analyticsstore: insert faces failed", slog.Any("err", err))
		}
	}

	return &securityv1.AnalyzeFacesResponse{
		Meta:      metaOK(req.Meta.RequestId),
		ImageUrl:  "/faces/" + filename,
		FaceCount: int32(len(dets)),
		Faces:     faces,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────

func validateMeta(meta *commonv1.RequestMeta) error {
	if meta == nil {
		return status.Error(codes.InvalidArgument, "meta is required")
	}
	if meta.RequestId == "" {
		return status.Error(codes.InvalidArgument, "meta.request_id is required")
	}
	return nil
}

func metaOK(requestID string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{
		RequestId: requestID,
		Success:   true,
		Timestamp: timestamppb.Now(),
	}
}

func metaError(requestID, code, msg string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{
		RequestId:    requestID,
		Success:      false,
		ErrorCode:    code,
		ErrorMessage: msg,
		Timestamp:    timestamppb.Now(),
	}
}
