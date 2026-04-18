package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/learning/server"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer creates a LearningServer with no DB (knowledge store disabled).
func newTestServer(t *testing.T) *server.LearningServer {
	t.Helper()
	return server.New(discardLogger(), server.Config{})
}

func metaFor(id string) *commonv1.RequestMeta { return &commonv1.RequestMeta{RequestId: id} }

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != want {
		t.Errorf("code = %v, want %v (message: %q)", st.Code(), want, st.Message())
	}
}

// ── SubmitFeedback ────────────────────────────────────────────────────────────

func TestSubmitFeedback_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSubmitFeedback_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSubmitFeedback_EmptyInteractionID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta:   metaFor("fb-bad"),
		Rating: 0.8,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSubmitFeedback_RatingAboveOne(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta:          metaFor("fb-bad2"),
		InteractionId: "interaction-1",
		Rating:        1.5,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSubmitFeedback_NegativeRating(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta:          metaFor("fb-bad3"),
		InteractionId: "interaction-1",
		Rating:        -0.1,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSubmitFeedback_HappyPath_Positive(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta:          metaFor("fb-001"),
		InteractionId: "int-001",
		FeedbackType:  learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE,
		Rating:        0.9,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FeedbackId == "" {
		t.Error("expected non-empty FeedbackId")
	}
	if resp.QueuedForTraining {
		t.Error("positive high-rating feedback should not be queued for training")
	}
}

func TestSubmitFeedback_HappyPath_Correction_QueuesTraining(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.SubmitFeedback(context.Background(), &learningv1.SubmitFeedbackRequest{
		Meta:          metaFor("fb-002"),
		InteractionId: "int-002",
		FeedbackType:  learningv1.FeedbackType_FEEDBACK_TYPE_CORRECTION,
		Correction:    "should have said X not Y",
		Rating:        0.2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.QueuedForTraining {
		t.Error("correction feedback should be queued for training")
	}
}

// ── GetBehaviorProfile ────────────────────────────────────────────────────────

func TestGetBehaviorProfile_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GetBehaviorProfile(context.Background(), &learningv1.GetBehaviorProfileRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGetBehaviorProfile_EmptySubjectID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GetBehaviorProfile(context.Background(), &learningv1.GetBehaviorProfileRequest{
		Meta: metaFor("bp-bad"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGetBehaviorProfile_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.GetBehaviorProfile(context.Background(), &learningv1.GetBehaviorProfileRequest{
		Meta:      metaFor("bp-001"),
		SubjectId: "user-42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta")
	}
}

// ── GetModelPerformance ───────────────────────────────────────────────────────

func TestGetModelPerformance_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GetModelPerformance(context.Background(), &learningv1.GetModelPerformanceRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGetModelPerformance_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.GetModelPerformance(context.Background(), &learningv1.GetModelPerformanceRequest{
		Meta:   metaFor("mp-001"),
		Domain: learningv1.ModelDomain_MODEL_DOMAIN_NLP,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Domain != learningv1.ModelDomain_MODEL_DOMAIN_NLP {
		t.Errorf("domain = %v, want NLP", resp.Domain)
	}
}

// ── AddKnowledge (no DB → FailedPrecondition) ─────────────────────────────────

func TestAddKnowledge_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AddKnowledge(context.Background(), &learningv1.AddKnowledgeRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAddKnowledge_EmptyQuery(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AddKnowledge(context.Background(), &learningv1.AddKnowledgeRequest{
		Meta:    metaFor("ak-bad"),
		Summary: "some summary",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAddKnowledge_EmptySummary(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AddKnowledge(context.Background(), &learningv1.AddKnowledgeRequest{
		Meta:  metaFor("ak-bad2"),
		Query: "what is X",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAddKnowledge_NoDB_FailedPrecondition(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AddKnowledge(context.Background(), &learningv1.AddKnowledgeRequest{
		Meta:    metaFor("ak-001"),
		Query:   "what is AI",
		Summary: "AI is artificial intelligence",
	})
	assertCode(t, err, codes.FailedPrecondition)
}

// ── ListKnowledge (no DB → FailedPrecondition) ────────────────────────────────

func TestListKnowledge_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ListKnowledge(context.Background(), &learningv1.ListKnowledgeRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestListKnowledge_NoDB_FailedPrecondition(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ListKnowledge(context.Background(), &learningv1.ListKnowledgeRequest{
		Meta:  metaFor("lk-001"),
		Limit: 5,
	})
	assertCode(t, err, codes.FailedPrecondition)
}

// ── SearchKnowledge (no DB → FailedPrecondition) ─────────────────────────────

func TestSearchKnowledge_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SearchKnowledge(context.Background(), &learningv1.SearchKnowledgeRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSearchKnowledge_EmptyQuery(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SearchKnowledge(context.Background(), &learningv1.SearchKnowledgeRequest{
		Meta: metaFor("sk-bad"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSearchKnowledge_NoDB_FailedPrecondition(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SearchKnowledge(context.Background(), &learningv1.SearchKnowledgeRequest{
		Meta:  metaFor("sk-001"),
		Query: "machine learning",
	})
	assertCode(t, err, codes.FailedPrecondition)
}
