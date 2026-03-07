// Package server implements the gRPC SecurityService.
package server

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	securityv1 "github.com/rkrimper1/jarvis/gen/security"
	"github.com/rkrimper1/jarvis/services/security-service/internal/audit"
	authpkg "github.com/rkrimper1/jarvis/services/security-service/internal/auth"
	"github.com/rkrimper1/jarvis/services/security-service/internal/config"
	"github.com/rkrimper1/jarvis/services/security-service/internal/protocol"
	"github.com/rkrimper1/jarvis/services/security-service/internal/threat"
	"github.com/rkrimper1/jarvis/services/security-service/internal/token"
)

// SecurityServer implements securityv1.SecurityServiceServer.
type SecurityServer struct {
	securityv1.UnimplementedSecurityServiceServer

	cfg       *config.Config
	auth      *authpkg.Engine
	tokens    *token.Manager
	assessor  *threat.Assessor
	broadcast *threat.AlertBroadcaster
	protocols *protocol.Executor
	auditLog  *audit.Store
	log       *slog.Logger
}

// New wires all dependencies and returns a ready SecurityServer.
func New(cfg *config.Config, log *slog.Logger) *SecurityServer {
	assessor := threat.New()
	broadcaster := threat.NewBroadcaster()

	// Start the background patrol scanner so streaming subscribers always
	// receive data — remove in production and replace with real sensor input.
	broadcaster.SimulatePatrolScan(assessor, cfg.Threat.AlertBroadcastInterval)

	return &SecurityServer{
		cfg:       cfg,
		auth:      authpkg.New(),
		tokens:    token.New(cfg.Token.Secret, cfg.Token.AccessTokenTTL, cfg.Token.Issuer),
		assessor:  assessor,
		broadcast: broadcaster,
		protocols: protocol.New(log),
		auditLog:  audit.New(),
		log:       log,
	}
}

// ── Authenticate ─────────────────────────────────────────────────────

func (s *SecurityServer) Authenticate(
	ctx context.Context,
	req *securityv1.AuthRequest,
) (*securityv1.AuthResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.SubjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "subject_id is required")
	}

	s.log.InfoContext(ctx, "Authenticate",
		slog.String("request_id", req.Meta.RequestId),
		slog.String("subject_id", req.SubjectId),
		slog.String("method", req.Method.String()),
	)

	result := s.auth.Verify(req.SubjectId, req.Method, req.CredentialPayload)

	s.auditLog.Append(req.SubjectId, "authenticate:"+req.Method.String(), "security/auth", result.Valid)

	if !result.Valid {
		s.log.WarnContext(ctx, "authentication failed",
			slog.String("subject_id", req.SubjectId),
			slog.String("reason", result.Reason),
		)
		return &securityv1.AuthResponse{
			Meta:          metaError(req.Meta.RequestId, "AUTH_FAILED", result.Reason),
			Authenticated: false,
		}, nil
	}

	accessToken, expiresAt, err := s.tokens.Issue(req.SubjectId, result.GrantedScopes)
	if err != nil {
		s.log.ErrorContext(ctx, "token issue failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "token generation failed")
	}

	s.log.InfoContext(ctx, "authentication successful",
		slog.String("subject_id", req.SubjectId),
		slog.Any("scopes", result.GrantedScopes),
	)

	return &securityv1.AuthResponse{
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
	req *securityv1.ThreatAssessmentRequest,
) (*securityv1.ThreatAssessmentResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}

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

	// Broadcast high+ threats to all streaming subscribers
	if result.Level >= securityv1.ThreatLevel_THREAT_HIGH {
		s.broadcast.Publish(result)
	}

	return &securityv1.ThreatAssessmentResponse{
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
	req *securityv1.ProtocolRequest,
) (*securityv1.ProtocolResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.Protocol == securityv1.ProtocolType_PROTOCOL_UNSPECIFIED {
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

	return &securityv1.ProtocolResponse{
		Meta:             metaOK(req.Meta.RequestId),
		ProtocolExecuted: result.Protocol,
		ActionsTaken:     result.ActionsTaken,
	}, nil
}

// ── GetAuditLog ───────────────────────────────────────────────────────

func (s *SecurityServer) GetAuditLog(
	ctx context.Context,
	req *securityv1.AuditLogRequest,
) (*securityv1.AuditLogResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}

	pageSize := int(req.PageSize)
	if pageSize <= 0 || pageSize > s.cfg.Audit.MaxPageSize {
		pageSize = s.cfg.Audit.MaxPageSize
	}

	var fromT, toT interface{ AsTime() interface{ IsZero() bool } }
	_ = fromT
	_ = toT

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

	return &securityv1.AuditLogResponse{
		Meta:          metaOK(req.Meta.RequestId),
		Entries:       entries,
		NextPageToken: nextToken,
	}, nil
}

// ── StreamSecurityAlerts ──────────────────────────────────────────────

func (s *SecurityServer) StreamSecurityAlerts(
	meta *commonv1.RequestMeta,
	stream securityv1.SecurityService_StreamSecurityAlertsServer,
) error {

	subscriberID := meta.GetRequestId()
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
			resp := &securityv1.ThreatAssessmentResponse{
				Meta:               metaOK(subscriberID),
				Level:              result.Level,
				Confidence:         result.Confidence,
				ThreatSummary:      result.Summary,
				RecommendedActions: result.RecommendedActions,
			}
			if err := stream.Send(resp); err != nil {
				s.log.Error("StreamSecurityAlerts: send error", slog.Any("err", err))
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
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
