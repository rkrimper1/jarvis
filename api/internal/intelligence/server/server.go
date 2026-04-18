package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	"github.com/rkrimper1/jarvis/api/internal/intelligence/knowledge"
	intelstore "github.com/rkrimper1/jarvis/api/internal/intelligence/store"
	"github.com/rkrimper1/jarvis/api/middleware"
)

type IntelligenceServer struct {
	intelligv1.UnimplementedIntelligenceServiceServer
	kb     *knowledge.Base
	store  *intelstore.Store
	engine fusion.Engine
	log    *slog.Logger
}

// New returns an IntelligenceServer with the in-memory knowledge base only.
// Intel Hunt RPCs will return FailedPrecondition until NewWithIntelHunt is used.
func New(log *slog.Logger) *IntelligenceServer {
	return &IntelligenceServer{kb: knowledge.New(), log: log}
}

// NewWithIntelHunt returns an IntelligenceServer with the Intel Hunt store and
// FusionEngine wired in. store and engine may be nil to disable the feature.
func NewWithIntelHunt(log *slog.Logger, store *intelstore.Store, engine fusion.Engine) *IntelligenceServer {
	return &IntelligenceServer{kb: knowledge.New(), store: store, engine: engine, log: log}
}

func (s *IntelligenceServer) intelHuntRequired() error {
	if s.store == nil || s.engine == nil {
		return status.Error(codes.FailedPrecondition, "intel hunt unavailable (set JARVIS_DB_PATH and ANTHROPIC_API_KEY)")
	}
	return nil
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

// ── Intel Hunt handlers ───────────────────────────────────────────────────────

func (s *IntelligenceServer) IngestSignal(ctx context.Context, req *intelligv1.IngestSignalRequest) (*intelligv1.IngestSignalResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if err := s.intelHuntRequired(); err != nil {
		return nil, err
	}
	if req.RawContent == "" {
		return nil, status.Error(codes.InvalidArgument, "raw_content is required")
	}

	ctx, span := trace.StartSpan(ctx, "intelligence/IngestSignal")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	signal, err := s.store.SaveSignal(ctx, req.SourceType, req.RawContent, req.SourceUri)
	if err != nil {
		s.log.ErrorContext(ctx, "intel: save signal failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "save signal: %v", err)
	}

	result, err := s.engine.Fuse(ctx, req.RawContent)
	if err != nil {
		s.log.ErrorContext(ctx, "intel: fusion failed", slog.String("signal_id", signal.Id), slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "fusion: %v", err)
	}

	card, err := s.store.SaveCard(ctx,
		result.Title,
		result.Summary,
		result.OpportunityType,
		result.ConfidenceScore,
		result.SuggestedAction,
		[]string{signal.Id},
	)
	if err != nil {
		s.log.ErrorContext(ctx, "intel: save card failed", slog.String("signal_id", signal.Id), slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "save card: %v", err)
	}

	s.log.InfoContext(ctx, "intel: signal ingested",
		slog.String("signal_id", signal.Id),
		slog.String("card_id", card.Id),
		slog.Float64("confidence", float64(card.ConfidenceScore)),
	)
	return &intelligv1.IngestSignalResponse{
		Meta:   metaOK(req.Meta.RequestId),
		Signal: signal,
		Card:   card,
	}, nil
}

func (s *IntelligenceServer) ListIntelCards(ctx context.Context, req *intelligv1.ListIntelCardsRequest) (*intelligv1.ListIntelCardsResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if err := s.intelHuntRequired(); err != nil {
		return nil, err
	}

	ctx, span := trace.StartSpan(ctx, "intelligence/ListIntelCards")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	cards, total, nextToken, err := s.store.ListCards(ctx, req.StatusFilter, req.PageSize, req.PageToken)
	if err != nil {
		s.log.ErrorContext(ctx, "intel: list cards failed", slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "list cards: %v", err)
	}

	return &intelligv1.ListIntelCardsResponse{
		Meta:          metaOK(req.Meta.RequestId),
		Cards:         cards,
		TotalCount:    total,
		NextPageToken: nextToken,
	}, nil
}

func (s *IntelligenceServer) ConfirmAction(ctx context.Context, req *intelligv1.ConfirmActionRequest) (*intelligv1.ConfirmActionResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if err := s.intelHuntRequired(); err != nil {
		return nil, err
	}
	if req.CardId == "" {
		return nil, status.Error(codes.InvalidArgument, "card_id is required")
	}
	if req.NewStatus != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED &&
		req.NewStatus != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED {
		return nil, status.Error(codes.InvalidArgument, "new_status must be CONFIRMED or DISMISSED")
	}

	ctx, span := trace.StartSpan(ctx, "intelligence/ConfirmAction")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	card, err := s.store.UpdateCardStatus(ctx, req.CardId, req.NewStatus)
	if err != nil {
		if isNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "card %s not found", req.CardId)
		}
		s.log.ErrorContext(ctx, "intel: confirm action failed", slog.String("card_id", req.CardId), slog.Any("err", err))
		return nil, status.Errorf(codes.Internal, "update card: %v", err)
	}

	s.log.InfoContext(ctx, "intel: card status updated",
		slog.String("card_id", card.Id),
		slog.String("status", req.NewStatus.String()),
	)
	return &intelligv1.ConfirmActionResponse{
		Meta: metaOK(req.Meta.RequestId),
		Card: card,
	}, nil
}

// SearchHandler returns an HTTP handler for GET /v1/intel/cards/search.
//
//	Query params:
//	  q         — search term (matched against title, summary, suggested_action)
//	  page_size — max results to return (default 20)
//
// Returns JSON: {"cards": [...], "total": N}
func (s *IntelligenceServer) SearchHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		if s.store == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"error": "intel hunt unavailable"})
			return
		}

		q := r.URL.Query().Get("q")
		pageSize := int32(20)
		if ps := r.URL.Query().Get("page_size"); ps != "" {
			if v, err := strconv.Atoi(ps); err == nil && v > 0 {
				pageSize = int32(v)
			}
		}

		cards, err := s.store.SearchCards(r.Context(), q, pageSize)
		if err != nil {
			s.log.ErrorContext(r.Context(), "intel: search cards failed", slog.Any("err", err))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if cards == nil {
			cards = []*intelligv1.IntelCard{}
		}

		// Marshal each card with protojson so field names are camelCase,
		// matching grpc-gateway and the TypeScript IntelCard interface.
		marshaler := protojson.MarshalOptions{EmitUnpopulated: false}
		rawCards := make([]json.RawMessage, len(cards))
		for i, c := range cards {
			b, err := marshaler.Marshal(c)
			if err != nil {
				s.log.ErrorContext(r.Context(), "intel: marshal card failed", slog.Any("err", err))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "marshal failed"})
				return
			}
			rawCards[i] = json.RawMessage(b)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"cards": rawCards,
			"total": len(cards),
		}); err != nil {
			s.log.ErrorContext(r.Context(), "intel: search encode failed", slog.Any("err", err))
		}
	})
}

func isNotFound(err error) bool {
	return errors.Is(err, intelstore.ErrNotFound)
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
