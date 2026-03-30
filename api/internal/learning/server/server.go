package server

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	"github.com/rkrimper1/jarvis/api/internal/learning/adaptbus"
	"github.com/rkrimper1/jarvis/api/internal/learning/feedback"
	"github.com/rkrimper1/jarvis/api/internal/learning/knowledge"
	"github.com/rkrimper1/jarvis/api/internal/learning/metrics"
	"github.com/rkrimper1/jarvis/api/internal/learning/profile"
	"github.com/rkrimper1/jarvis/api/middleware"
)

// Config holds optional knowledge-store settings injected from env vars.
type Config struct {
	KnowledgeDBPath    string
	KnowledgeStaleDays int
	WebSearchMaxUses   int
	AnthropicAPIKey    string
	ClaudeModel        string
}

// LearningServer implements learningv1.LearningServiceServer.
type LearningServer struct {
	learningv1.UnimplementedLearningServiceServer
	feedback       *feedback.Store
	profiler       *profile.Profiler
	metrics        *metrics.Tracker
	bus            *adaptbus.Bus
	knowledge      *knowledge.Store
	webSearchMax   int
	webSearchUsed  atomic.Int32
	log            *slog.Logger
}

// New wires all learning-service dependencies.
func New(log *slog.Logger, cfg Config) *LearningServer {
	s := &LearningServer{
		feedback:     feedback.New(),
		profiler:     profile.New(),
		metrics:      metrics.New(),
		bus:          adaptbus.New(32, 7*time.Second, log),
		webSearchMax: cfg.WebSearchMaxUses,
		log:          log,
	}
	if cfg.KnowledgeDBPath != "" {
		ks, err := knowledge.New(cfg.KnowledgeDBPath, cfg.KnowledgeStaleDays, cfg.AnthropicAPIKey, cfg.ClaudeModel)
		if err != nil {
			log.Error("learning: knowledge store init failed", slog.Any("err", err))
		} else {
			s.knowledge = ks
		}
	}
	return s
}

// SubmitFeedback records an interaction rating or correction.
// Corrections and low ratings (< 0.4) are automatically queued for retraining.
func (s *LearningServer) SubmitFeedback(ctx context.Context, req *learningv1.SubmitFeedbackRequest) (*learningv1.SubmitFeedbackResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/SubmitFeedback")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.InteractionId == "" {
		return nil, status.Error(codes.InvalidArgument, "interaction_id is required")
	}
	if req.Rating < 0 || req.Rating > 1 {
		return nil, status.Error(codes.InvalidArgument, "rating must be between 0.0 and 1.0")
	}

	s.log.InfoContext(ctx, "SubmitFeedback",
		slog.String("interaction_id", req.InteractionId),
		slog.String("type", req.FeedbackType.String()),
		slog.Float64("rating", float64(req.Rating)),
	)

	fbID, queued := s.feedback.Submit(
		req.InteractionId, req.FeedbackType, req.Correction, req.Rating, req.Notes,
	)

	// If queued, publish an adaptation event so StreamAdaptationEvents subscribers know
	if queued {
		s.bus.Publish(&learningv1.AdaptationEvent{
			EventId:       "adapt-fb-" + fbID,
			Domain:        learningv1.ModelDomain_MODEL_DOMAIN_NLP,
			Description:   "Feedback correction queued for training pipeline: " + req.InteractionId,
			DeltaAccuracy: 0,
			Timestamp:     timestamppb.Now(),
		})
	}

	return &learningv1.SubmitFeedbackResponse{
		Meta:               metaOK(req.Meta.RequestId),
		QueuedForTraining:  queued,
		FeedbackId:         fbID,
	}, nil
}

// GetBehaviorProfile returns the behavioral profile for a subject.
func (s *LearningServer) GetBehaviorProfile(ctx context.Context, req *learningv1.GetBehaviorProfileRequest) (*learningv1.GetBehaviorProfileResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/GetBehaviorProfile")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.SubjectId == "" {
		return nil, status.Error(codes.InvalidArgument, "subject_id is required")
	}
	s.log.InfoContext(ctx, "GetBehaviorProfile", slog.String("subject_id", req.SubjectId))

	prof := s.profiler.Get(req.SubjectId)
	prof.Meta = metaOK(req.Meta.RequestId)
	return prof, nil
}

// GetModelPerformance returns live accuracy/precision/recall for a model domain.
func (s *LearningServer) GetModelPerformance(ctx context.Context, req *learningv1.GetModelPerformanceRequest) (*learningv1.GetModelPerformanceResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/GetModelPerformance")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	s.log.InfoContext(ctx, "GetModelPerformance", slog.String("domain", req.Domain.String()))

	snap := s.metrics.Get(req.Domain, req.GetFrom().AsTime(), req.GetTo().AsTime())
	return &learningv1.GetModelPerformanceResponse{
		Meta:                metaOK(req.Meta.RequestId),
		Domain:              snap.Domain,
		Accuracy:            snap.Accuracy,
		Precision:           snap.Precision,
		Recall:              snap.Recall,
		TotalInferences:     snap.TotalInferences,
		DegradationWarnings: snap.DegradationWarnings,
	}, nil
}

// StreamAdaptationEvents fans out model improvement events to the caller.
func (s *LearningServer) StreamAdaptationEvents(req *learningv1.StreamAdaptationEventsRequest, stream learningv1.LearningService_StreamAdaptationEventsServer) error {
	subscriberID := req.GetMeta().GetRequestId()
	middleware.AddRequestAttributes(stream.Context(), req.GetMeta().GetRequestId(), req.GetMeta().GetUserId())
	s.log.Info("StreamAdaptationEvents: subscriber connected", slog.String("id", subscriberID))

	ch, unsub := s.bus.Subscribe(subscriberID)
	defer unsub()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			_, evSpan := trace.StartSpan(stream.Context(), "learning/StreamAdaptationEvents/send_event")
			err := stream.Send(&learningv1.StreamAdaptationEventsResponse{Event: ev})
			evSpan.End()
			if err != nil {
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
}

// AddKnowledge manually inserts an entry into the knowledge store.
func (s *LearningServer) AddKnowledge(ctx context.Context, req *learningv1.AddKnowledgeRequest) (*learningv1.AddKnowledgeResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/AddKnowledge")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	if req.Summary == "" {
		return nil, status.Error(codes.InvalidArgument, "summary is required")
	}
	if s.knowledge == nil {
		return nil, status.Error(codes.FailedPrecondition, "knowledge store not configured (set KNOWLEDGE_DB_PATH)")
	}
	confidence := float64(req.Confidence)
	if confidence <= 0 {
		confidence = 1.0
	}
	entry, err := s.knowledge.Save(ctx, req.Query, req.Summary, "manual", confidence, req.Tags)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "add knowledge: %v", err)
	}
	s.log.InfoContext(ctx, "AddKnowledge: entry saved", slog.String("query", req.Query))
	return &learningv1.AddKnowledgeResponse{
		Meta:  metaOK(req.Meta.RequestId),
		Entry: entry,
	}, nil
}

// ListKnowledge returns the most recently updated knowledge entries.
func (s *LearningServer) ListKnowledge(ctx context.Context, req *learningv1.ListKnowledgeRequest) (*learningv1.ListKnowledgeResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/ListKnowledge")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if s.knowledge == nil {
		return nil, status.Error(codes.FailedPrecondition, "knowledge store not configured (set KNOWLEDGE_DB_PATH)")
	}
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 5
	}
	entries, err := s.knowledge.List(ctx, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list knowledge: %v", err)
	}
	return &learningv1.ListKnowledgeResponse{
		Meta:    metaOK(req.Meta.RequestId),
		Entries: entries,
	}, nil
}

// SearchKnowledge searches the local knowledge DB. When nothing fresh is found
// it returns needs_confirmation=true so the caller can prompt the user.
// On a confirmed call with a preferred_source it executes the external search,
// saves the result, and returns it.
func (s *LearningServer) SearchKnowledge(ctx context.Context, req *learningv1.SearchKnowledgeRequest) (*learningv1.SearchKnowledgeResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "learning/SearchKnowledge")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	if s.knowledge == nil {
		return nil, status.Error(codes.FailedPrecondition, "knowledge store not configured (set KNOWLEDGE_DB_PATH)")
	}

	remaining := int32(s.webSearchMax) - s.webSearchUsed.Load()

	// ── Confirmed external search ─────────────────────────────────────
	if req.Confirmed {
		if remaining <= 0 {
			return nil, status.Errorf(codes.ResourceExhausted,
				"external search limit reached (%d uses per session)", s.webSearchMax)
		}
		src := req.PreferredSource
		if src == learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_UNSPECIFIED {
			src = learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_CLAUDE_API
		}
		entry, err := s.knowledge.SearchWithClaude(ctx, req.Query, src)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "external search failed: %v", err)
		}
		s.webSearchUsed.Add(1)
		remaining--
		s.log.InfoContext(ctx, "SearchKnowledge: external search saved",
			slog.String("query", req.Query),
			slog.String("source", src.String()),
		)
		return &learningv1.SearchKnowledgeResponse{
			Meta:              metaOK(req.Meta.RequestId),
			Results:           []*learningv1.KnowledgeEntry{entry},
			SearchesRemaining: remaining,
		}, nil
	}

	// ── Local DB search ───────────────────────────────────────────────
	results, found, err := s.knowledge.Search(ctx, req.Query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "knowledge search: %v", err)
	}
	if found {
		s.log.InfoContext(ctx, "SearchKnowledge: DB hit", slog.String("query", req.Query))
		return &learningv1.SearchKnowledgeResponse{
			Meta:              metaOK(req.Meta.RequestId),
			Results:           results,
			SearchesRemaining: remaining,
		}, nil
	}

	// ── No fresh results — ask the user ──────────────────────────────
	suggested := learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_CLAUDE_API
	if req.PreferredSource != learningv1.KnowledgeSource_KNOWLEDGE_SOURCE_UNSPECIFIED {
		suggested = req.PreferredSource
	}
	s.log.InfoContext(ctx, "SearchKnowledge: no DB results, prompting user", slog.String("query", req.Query))
	return &learningv1.SearchKnowledgeResponse{
		Meta:                metaOK(req.Meta.RequestId),
		NeedsConfirmation:   true,
		SuggestedSource:     suggested,
		SearchesRemaining:   remaining,
	}, nil
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
