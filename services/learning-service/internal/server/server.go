package server

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	learningv1 "github.com/rkrimper1/jarvis/gen/learning"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/adaptbus"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/feedback"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/metrics"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/profile"
)

// LearningServer implements learningv1.LearningServiceServer.
type LearningServer struct {
	learningv1.UnimplementedLearningServiceServer
	feedback *feedback.Store
	profiler *profile.Profiler
	metrics  *metrics.Tracker
	bus      *adaptbus.Bus
	log      *slog.Logger
}

// New wires all learning-service dependencies.
func New(log *slog.Logger) *LearningServer {
	return &LearningServer{
		feedback: feedback.New(),
		profiler: profile.New(),
		metrics:  metrics.New(),
		bus:      adaptbus.New(32, 7*time.Second, log),
		log:      log,
	}
}

// SubmitFeedback records an interaction rating or correction.
// Corrections and low ratings (< 0.4) are automatically queued for retraining.
func (s *LearningServer) SubmitFeedback(ctx context.Context, req *learningv1.SubmitFeedbackRequest) (*learningv1.SubmitFeedbackResponse, error) {
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
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
			if err := stream.Send(&learningv1.StreamAdaptationEventsResponse{Event: ev}); err != nil {
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
