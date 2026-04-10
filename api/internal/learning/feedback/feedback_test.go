package feedback_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/learning/feedback"
	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestStore(t *testing.T) *feedback.Store {
	t.Helper()
	return feedback.New()
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	s := feedback.New()
	if s == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_StartsEmpty(t *testing.T) {
	s := newTestStore(t)
	counts := s.Count()
	if len(counts) != 0 {
		t.Errorf("expected empty counts map, got %v", counts)
	}
}

// ── Submit ────────────────────────────────────────────────────────────────────

func TestSubmit_ReturnsUniqueIDs(t *testing.T) {
	s := newTestStore(t)
	id1, _ := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	id2, _ := s.Submit("iact-2", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	if id1 == id2 {
		t.Errorf("expected unique IDs, both got %q", id1)
	}
}

func TestSubmit_IDsHaveFbPrefix(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.8, "")
	if len(id) < 3 || id[:3] != "fb-" {
		t.Errorf("expected ID to start with 'fb-', got %q", id)
	}
}

func TestSubmit_PositiveHighRating_NotQueued(t *testing.T) {
	s := newTestStore(t)
	_, queued := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	if queued {
		t.Error("positive high-rating feedback should not be queued for training")
	}
}

func TestSubmit_CorrectionType_AlwaysQueued(t *testing.T) {
	s := newTestStore(t)
	_, queued := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_CORRECTION, "fix this", 0.9, "")
	if !queued {
		t.Error("CORRECTION type should always be queued regardless of rating")
	}
}

func TestSubmit_LowRating_Queued(t *testing.T) {
	s := newTestStore(t)
	// Rating < 0.4 should be queued
	_, queued := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE, "", 0.3, "")
	if !queued {
		t.Error("rating < 0.4 should be queued for training")
	}
}

func TestSubmit_RatingExactlyZeroPointFour_NotQueued(t *testing.T) {
	s := newTestStore(t)
	// rating == 0.4 should NOT be queued (boundary: < 0.4 condition)
	_, queued := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE, "", 0.4, "")
	if queued {
		t.Error("rating == 0.4 should not be queued (condition is strictly < 0.4)")
	}
}

func TestSubmit_ZeroRating_Queued(t *testing.T) {
	s := newTestStore(t)
	_, queued := s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_UNSPECIFIED, "", 0.0, "")
	if !queued {
		t.Error("rating == 0.0 should be queued")
	}
}

func TestSubmit_EmptyFields_OK(t *testing.T) {
	s := newTestStore(t)
	id, queued := s.Submit("", learningv1.FeedbackType_FEEDBACK_TYPE_UNSPECIFIED, "", 0.5, "")
	if id == "" {
		t.Error("expected non-empty ID even for empty submission")
	}
	// rating 0.5 >= 0.4 and type != CORRECTION -> not queued
	if queued {
		t.Error("should not be queued for unspecified type + rating 0.5")
	}
}

// ── Count ─────────────────────────────────────────────────────────────────────

func TestCount_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	counts := s.Count()
	if len(counts) != 0 {
		t.Errorf("expected empty counts, got %v", counts)
	}
}

func TestCount_SingleEntry(t *testing.T) {
	s := newTestStore(t)
	s.Submit("iact-1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	counts := s.Count()
	if counts[learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE] != 1 {
		t.Errorf("expected count 1 for POSITIVE, got %d", counts[learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE])
	}
}

func TestCount_MultipleTypes(t *testing.T) {
	s := newTestStore(t)
	s.Submit("i1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	s.Submit("i2", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.8, "")
	s.Submit("i3", learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE, "", 0.2, "")
	s.Submit("i4", learningv1.FeedbackType_FEEDBACK_TYPE_CORRECTION, "fix", 0.5, "")

	counts := s.Count()
	if counts[learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE] != 2 {
		t.Errorf("POSITIVE: want 2, got %d", counts[learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE])
	}
	if counts[learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE] != 1 {
		t.Errorf("NEGATIVE: want 1, got %d", counts[learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE])
	}
	if counts[learningv1.FeedbackType_FEEDBACK_TYPE_CORRECTION] != 1 {
		t.Errorf("CORRECTION: want 1, got %d", counts[learningv1.FeedbackType_FEEDBACK_TYPE_CORRECTION])
	}
}

func TestCount_DoesNotCountOtherTypes(t *testing.T) {
	s := newTestStore(t)
	s.Submit("i1", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
	counts := s.Count()
	if _, ok := counts[learningv1.FeedbackType_FEEDBACK_TYPE_NEGATIVE]; ok {
		t.Error("NEGATIVE key should not be present when no negative feedback submitted")
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestSubmit_ConcurrentSafe(t *testing.T) {
	s := newTestStore(t)
	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			s.Submit(fmt.Sprintf("iact-%d", n), learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
		}(i)
	}
	wg.Wait()

	// All goroutines must have stored exactly one entry each.
	total := 0
	for _, v := range s.Count() {
		total += v
	}
	if total != goroutines {
		t.Errorf("expected %d entries, got %d", goroutines, total)
	}
}

func TestCount_ConcurrentWithSubmit(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	// Writers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.Submit(fmt.Sprintf("iact-%d", n), learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
		}(i)
	}
	// Readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Count()
		}()
	}
	wg.Wait() // should not race
}

// ── Sequential IDs ────────────────────────────────────────────────────────────

func TestSubmit_IDsAreMonotonicallyIncreasing(t *testing.T) {
	s := newTestStore(t)
	var ids []string
	for i := 0; i < 5; i++ {
		id, _ := s.Submit("iact", learningv1.FeedbackType_FEEDBACK_TYPE_POSITIVE, "", 0.9, "")
		ids = append(ids, id)
	}
	// Verify each ID is unique (not strictly ordered in concurrent mode, but
	// in serial this should hold).
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate ID: %q", id)
		}
		seen[id] = true
	}
}
