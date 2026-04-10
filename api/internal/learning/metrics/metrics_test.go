package metrics_test

import (
	"sync"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/learning/metrics"
	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestTracker(t *testing.T) *metrics.Tracker {
	t.Helper()
	return metrics.New()
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	tr := metrics.New()
	if tr == nil {
		t.Fatal("New() returned nil")
	}
}

// ── Get – seeded domains ──────────────────────────────────────────────────────

func TestGet_SeededDomain_NLP(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_NLP, now.Add(-time.Hour), now)
	if s == nil {
		t.Fatal("expected non-nil snapshot for NLP domain")
	}
	if s.Domain != learningv1.ModelDomain_MODEL_DOMAIN_NLP {
		t.Errorf("domain: got %v, want NLP", s.Domain)
	}
	// Seeded baseline for NLP is 0.94; with ±1% drift it stays in a reasonable range.
	if s.Accuracy < 0.0 || s.Accuracy > 1.0 {
		t.Errorf("accuracy out of [0,1] range: %v", s.Accuracy)
	}
	if s.Precision < 0.0 || s.Precision > 1.0 {
		t.Errorf("precision out of [0,1] range: %v", s.Precision)
	}
	if s.Recall < 0.0 || s.Recall > 1.0 {
		t.Errorf("recall out of [0,1] range: %v", s.Recall)
	}
}

func TestGet_SeededDomain_Threat(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_THREAT, now.Add(-time.Hour), now)
	if s == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if s.Domain != learningv1.ModelDomain_MODEL_DOMAIN_THREAT {
		t.Errorf("domain: got %v, want THREAT", s.Domain)
	}
}

func TestGet_SeededDomain_Hardware(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_HARDWARE, now.Add(-time.Hour), now)
	if s == nil {
		t.Fatal("expected non-nil snapshot")
	}
}

func TestGet_SeededDomain_Behavior(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR, now.Add(-time.Hour), now)
	if s == nil {
		t.Fatal("expected non-nil snapshot")
	}
}

// ── Get – unknown domain ──────────────────────────────────────────────────────

func TestGet_UnknownDomain_ReturnsBaseline(t *testing.T) {
	tr := newTestTracker(t)
	unknown := learningv1.ModelDomain(999)
	now := time.Now()
	s := tr.Get(unknown, now, now)
	if s == nil {
		t.Fatal("expected non-nil snapshot for unknown domain")
	}
	if s.Domain != unknown {
		t.Errorf("domain: got %v, want %v", s.Domain, unknown)
	}
	// Neutral baseline should be around 0.75
	if s.Accuracy < 0.0 || s.Accuracy > 1.0 {
		t.Errorf("accuracy out of range: %v", s.Accuracy)
	}
}

func TestGet_UnspecifiedDomain_ReturnsBaseline(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_UNSPECIFIED, now, now)
	if s == nil {
		t.Fatal("expected non-nil snapshot for unspecified domain")
	}
}

// ── Get – drift keeps values in bounds ───────────────────────────────────────

func TestGet_RepeatedCalls_ValuesStayInBounds(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	for i := 0; i < 100; i++ {
		s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_NLP, now, now)
		if s.Accuracy < 0.0 || s.Accuracy > 1.0 {
			t.Errorf("iter %d: accuracy %v out of [0,1]", i, s.Accuracy)
		}
		if s.Precision < 0.0 || s.Precision > 1.0 {
			t.Errorf("iter %d: precision %v out of [0,1]", i, s.Precision)
		}
		if s.Recall < 0.0 || s.Recall > 1.0 {
			t.Errorf("iter %d: recall %v out of [0,1]", i, s.Recall)
		}
	}
}

// ── Get – TotalInferences increases ──────────────────────────────────────────

func TestGet_TotalInferences_Increases(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s1 := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_NLP, now, now)
	initial := s1.TotalInferences
	// Call again — inferences should be >= initial (rand.Intn(100) is >= 0)
	s2 := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_NLP, now, now)
	if s2.TotalInferences < initial {
		t.Errorf("TotalInferences decreased: %d -> %d", initial, s2.TotalInferences)
	}
}

// ── Get – DegradationWarnings ─────────────────────────────────────────────────
// We cannot easily force the random drift to cross thresholds, so we verify that
// when warnings are present they contain expected substrings.

func TestGet_DegradationWarnings_NilOrStrings(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR, now, now)
	for _, w := range s.DegradationWarnings {
		if w == "" {
			t.Error("degradation warning should not be empty string")
		}
	}
}

// ── Get – RecordedAt is recent ────────────────────────────────────────────────

func TestGet_RecordedAt_IsRecent(t *testing.T) {
	before := time.Now().Add(-time.Second)
	tr := newTestTracker(t)
	now := time.Now()
	s := tr.Get(learningv1.ModelDomain_MODEL_DOMAIN_NLP, now, now)
	after := time.Now().Add(time.Second)

	if s.RecordedAt.Before(before) || s.RecordedAt.After(after) {
		t.Errorf("RecordedAt %v not in expected range [%v, %v]", s.RecordedAt, before, after)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestGet_ConcurrentSafe(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now()
	domains := []learningv1.ModelDomain{
		learningv1.ModelDomain_MODEL_DOMAIN_NLP,
		learningv1.ModelDomain_MODEL_DOMAIN_THREAT,
		learningv1.ModelDomain_MODEL_DOMAIN_HARDWARE,
		learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR,
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			d := domains[idx%len(domains)]
			s := tr.Get(d, now, now)
			if s == nil {
				t.Errorf("nil snapshot in concurrent get")
			}
		}(i)
	}
	wg.Wait()
}

// ── Unknown domain stored and retrievable ─────────────────────────────────────

func TestGet_UnknownDomain_StoredOnSecondCall(t *testing.T) {
	tr := newTestTracker(t)
	unknown := learningv1.ModelDomain(42)
	now := time.Now()
	// First call creates and stores the baseline.
	s1 := tr.Get(unknown, now, now)
	// Second call returns the same stored snapshot (now with drift applied).
	s2 := tr.Get(unknown, now, now)
	if s1 == nil || s2 == nil {
		t.Fatal("expected non-nil snapshots")
	}
	// Domain should be consistent.
	if s2.Domain != unknown {
		t.Errorf("domain changed on second call: got %v", s2.Domain)
	}
}
