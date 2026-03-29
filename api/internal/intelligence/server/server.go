package server

import (
	"context"
	"log/slog"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/knowledge"
	"github.com/rkrimper1/jarvis/api/middleware"
)

type IntelligenceServer struct {
	intelligv1.UnimplementedIntelligenceServiceServer
	kb  *knowledge.Base
	log *slog.Logger
}

func New(log *slog.Logger) *IntelligenceServer {
	return &IntelligenceServer{kb: knowledge.New(), log: log}
}

func (s *IntelligenceServer) QueryIntel(ctx context.Context, req *intelligv1.QueryIntelRequest) (*intelligv1.QueryIntelResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "intelligence/QueryIntel")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "QueryIntel", slog.String("query", req.Query), slog.String("depth", req.Depth.String()))

	record := s.kb.Query(req.Query, req.Depth)
	confidence := float32(0.85)
	if len(record.Facts) == 0 {
		confidence = 0.3
	}

	return &intelligv1.QueryIntelResponse{
		Meta:            metaOK(req.Meta.RequestId),
		SubjectId:       record.ID,
		Summary:         record.Summary,
		Facts:           record.Facts,
		RelatedSubjects: record.Related,
		Confidence:      confidence,
	}, nil
}

func (s *IntelligenceServer) AnalyzeArtifact(ctx context.Context, req *intelligv1.AnalyzeArtifactRequest) (*intelligv1.AnalyzeArtifactResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "intelligence/AnalyzeArtifact")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "AnalyzeArtifact", slog.String("artifact_id", req.ArtifactId))

	composition, isKnown, isHostile, anomalies, elements :=
		knowledge.AnalyzeArtifact(req.ArtifactId, req.ScanData, req.ArtifactDescription)

	return &intelligv1.AnalyzeArtifactResponse{
		Meta:                metaOK(req.Meta.RequestId),
		ArtifactId:          req.ArtifactId,
		CompositionSummary:  composition,
		IsKnownTechnology:   isKnown,
		IsPotentiallyHostile: isHostile,
		Anomalies:           anomalies,
		ElementBreakdown:    elements,
	}, nil
}

func (s *IntelligenceServer) CrossReference(ctx context.Context, req *intelligv1.CrossReferenceRequest) (*intelligv1.CrossReferenceResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "intelligence/CrossReference")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if len(req.SubjectIds) < 2 {
		return nil, status.Error(codes.InvalidArgument, "at least 2 subject_ids required for cross-reference")
	}
	s.log.InfoContext(ctx, "CrossReference", slog.Int("subjects", len(req.SubjectIds)))

	rels := s.kb.CrossReference(req.SubjectIds, req.RelationshipHint)
	return &intelligv1.CrossReferenceResponse{
		Meta:          metaOK(req.Meta.RequestId),
		Relationships: rels,
	}, nil
}

// StreamIntelUpdates periodically re-queries and pushes updates to the client.
func (s *IntelligenceServer) StreamIntelUpdates(req *intelligv1.StreamIntelUpdatesRequest, stream intelligv1.IntelligenceService_StreamIntelUpdatesServer) error {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			record := s.kb.Query("", intelligv1.AnalysisDepth_ANALYSIS_DEPTH_UNSPECIFIED)
			resp := &intelligv1.StreamIntelUpdatesResponse{
				Meta: metaOK(req.Meta.GetRequestId()),
				Update: &intelligv1.QueryIntelResponse{
					SubjectId: record.ID,
					Summary:   record.Summary,
					Facts:     record.Facts,
				},
			}
			if err := stream.Send(resp); err != nil {
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
}

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
	return &commonv1.ResponseMeta{RequestId: requestID, Success: true, Timestamp: timestamppb.Now()}
}
