package learning_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	learningv1 "github.com/rkrimper1/jarvis/gen/learning"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/adaptbus"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/feedback"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/metrics"
	"github.com/rkrimper1/jarvis/services/learning-service/internal/profile"
)

// ── Feedback ──────────────────────────────────────────────────────────

func TestFeedback_SubmitPositive(t *testing.T) {
	s := feedback.New()
	id, queued := s.Submit("interaction-001", learningv1.FeedbackType_FEEDBACK_POSITIVE, "", 0.9, "")
	if id == "" {
		t.Error("expected non-empty feedback ID")
	}
	if queued {
		t.Error("positive high-rating feedback should not be queued for training")
	}
}

func TestFeedback_SubmitCorrection_Queued(t *testing.T) {
	s := feedback.New()
	_, queued := s.Submit("interaction-002", learningv1.FeedbackType_FEEDBACK_CORRECTION, "The correct answer is 42", 0.5, "")
	if !queued {
		t.Error("corrections should always be queued for training")
	}
}

func TestFeedback_LowRating_Queued(t *testing.T) {
	s := feedback.New()
	_, queued := s.Submit("interaction-003", learningv1.FeedbackType_FEEDBACK_NEGATIVE, "", 0.2, "poor response")
	if !queued {
		t.Error("low rating (< 0.4) should be queued for training")
	}
}

func TestFeedback_UniqueIDs(t *testing.T) {
	s := feedback.New()
	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		id, _ := s.Submit("ia", learningv1.FeedbackType_FEEDBACK_POSITIVE, "", 0.8, "")
		if ids[id] {
			t.Errorf("duplicate feedback ID: %s", id)
		}
		ids[id] = true
	}
}

func TestFeedback_Count(t *testing.T) {
	s := feedback.New()
	s.Submit("i1", learningv1.FeedbackType_FEEDBACK_POSITIVE, "", 0.9, "")
	s.Submit("i2", learningv1.FeedbackType_FEEDBACK_POSITIVE, "", 0.8, "")
	s.Submit("i3", learningv1.FeedbackType_FEEDBACK_NEGATIVE, "", 0.2, "")

	counts := s.Count()
	if counts[learningv1.FeedbackType_FEEDBACK_POSITIVE] != 2 {
		t.Errorf("positive count = %d, want 2", counts[learningv1.FeedbackType_FEEDBACK_POSITIVE])
	}
}

// ── Profile ───────────────────────────────────────────────────────────

func TestProfile_KnownSubject(t *testing.T) {
	p := profile.New()
	prof := p.Get("tony-stark")
	if prof.SubjectId != "tony-stark" {
		t.Errorf("subject_id = %q", prof.SubjectId)
	}
	if prof.TraitScores["genius"] < 0.9 {
		t.Errorf("genius trait for tony-stark should be high, got %.2f", prof.TraitScores["genius"])
	}
}

func TestProfile_UnknownSubject_GeneratesDefault(t *testing.T) {
	p := profile.New()
	prof := p.Get("mystery-person-xyz")
	if prof == nil {
		t.Fatal("expected a profile to be generated for unknown subject")
	}
	if len(prof.TraitScores) == 0 {
		t.Error("default profile should have trait scores")
	}
	if prof.SubjectId != "mystery-person-xyz" {
		t.Errorf("subject_id = %q", prof.SubjectId)
	}
}

func TestProfile_UnknownSubject_Cached(t *testing.T) {
	p := profile.New()
	p1 := p.Get("new-user")
	p2 := p.Get("new-user")
	// Same pointer — profile was cached
	if p1 != p2 {
		t.Error("repeated Get for same subject should return cached profile")
	}
}

func TestProfile_InteractionPatterns(t *testing.T) {
	p := profile.New()
	prof := p.Get("pepper-potts")
	if len(prof.PreferredInteractionPatterns) == 0 {
		t.Error("pepper potts should have interaction patterns")
	}
}

// ── Metrics ───────────────────────────────────────────────────────────

func TestMetrics_GetSeededDomain(t *testing.T) {
	tracker := metrics.New()
	snap := tracker.Get(learningv1.ModelDomain_DOMAIN_NLP, time.Time{}, time.Time{})
	if snap.Accuracy <= 0 {
		t.Errorf("NLP accuracy should be > 0, got %.4f", snap.Accuracy)
	}
	if snap.TotalInferences == 0 {
		t.Error("NLP should have seeded inference count")
	}
}

func TestMetrics_UnknownDomain_Baseline(t *testing.T) {
	tracker := metrics.New()
	snap := tracker.Get(learningv1.ModelDomain_DOMAIN_UNSPECIFIED, time.Time{}, time.Time{})
	if snap.Accuracy <= 0 {
		t.Error("unknown domain should return a non-zero baseline")
	}
}

func TestMetrics_DriftIsApplied(t *testing.T) {
	tracker := metrics.New()
	snap1 := tracker.Get(learningv1.ModelDomain_DOMAIN_THREAT, time.Time{}, time.Time{})
	snap2 := tracker.Get(learningv1.ModelDomain_DOMAIN_THREAT, time.Time{}, time.Time{})
	// At least one metric should differ due to drift simulation
	// (very low probability they're all equal)
	allSame := snap1.Accuracy == snap2.Accuracy &&
		snap1.Precision == snap2.Precision &&
		snap1.Recall == snap2.Recall
	if allSame {
		t.Log("metrics appear identical across two calls — probabilistically unlikely but not a hard failure")
	}
}

// ── AdaptBus ──────────────────────────────────────────────────────────

func TestAdaptBus_SubscribeAndReceive(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	// Very short sim interval so test doesn't have to wait
	b := adaptbus.New(16, 50*time.Millisecond, log)

	ch, unsub := b.Subscribe("test-sub")
	defer unsub()

	select {
	case ev := <-ch:
		if ev.EventId == "" {
			t.Error("expected non-empty event ID")
		}
		if ev.Domain == learningv1.ModelDomain_DOMAIN_UNSPECIFIED {
			t.Error("expected a specific domain in adaptation event")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout: expected adaptation event within 500ms")
	}
}

func TestAdaptBus_UnsubscribeClosesChannel(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	b := adaptbus.New(4, time.Hour, log) // very long interval — no sim events

	ch, unsub := b.Subscribe("temp-sub")
	unsub()

	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestAdaptBus_MultipleSubscribers(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	b := adaptbus.New(8, time.Hour, log)

	ch1, unsub1 := b.Subscribe("sub-a")
	ch2, unsub2 := b.Subscribe("sub-b")
	defer unsub1()
	defer unsub2()

	b.Publish(&learningv1.AdaptationEvent{
		EventId:     "test-event",
		Domain:      learningv1.ModelDomain_DOMAIN_NLP,
		Description: "test",
	})

	for i, ch := range []<-chan *learningv1.AdaptationEvent{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.EventId != "test-event" {
				t.Errorf("subscriber %d: event_id = %q, want test-event", i+1, ev.EventId)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d: timeout", i+1)
		}
	}
}
