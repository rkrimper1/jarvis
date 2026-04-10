package profile_test

import (
	"sync"
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/learning/profile"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestProfiler(t *testing.T) *profile.Profiler {
	t.Helper()
	return profile.New()
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	p := profile.New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
}

// ── Get – seeded subjects ─────────────────────────────────────────────────────

func TestGet_TonyStark_HasExpectedTraits(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("tony-stark")
	if resp == nil {
		t.Fatal("expected non-nil response for tony-stark")
	}
	if resp.SubjectId != "tony-stark" {
		t.Errorf("SubjectId: got %q, want %q", resp.SubjectId, "tony-stark")
	}
	if resp.TraitScores == nil {
		t.Fatal("expected non-nil TraitScores")
	}
	if len(resp.TraitScores) == 0 {
		t.Error("expected non-empty TraitScores for tony-stark")
	}
	// Tony should have "genius" trait.
	if _, ok := resp.TraitScores["genius"]; !ok {
		t.Error("expected 'genius' trait in tony-stark's profile")
	}
	if len(resp.PreferredInteractionPatterns) == 0 {
		t.Error("expected preferred interaction patterns for tony-stark")
	}
}

func TestGet_PepperPotts_HasExpectedTraits(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("pepper-potts")
	if resp == nil {
		t.Fatal("expected non-nil response for pepper-potts")
	}
	if resp.SubjectId != "pepper-potts" {
		t.Errorf("SubjectId: got %q, want %q", resp.SubjectId, "pepper-potts")
	}
	if _, ok := resp.TraitScores["patience"]; !ok {
		t.Error("expected 'patience' trait in pepper-potts's profile")
	}
}

func TestGet_HappyHogan_HasExpectedTraits(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("happy-hogan")
	if resp == nil {
		t.Fatal("expected non-nil response for happy-hogan")
	}
	if resp.SubjectId != "happy-hogan" {
		t.Errorf("SubjectId: got %q, want %q", resp.SubjectId, "happy-hogan")
	}
	if _, ok := resp.TraitScores["vigilance"]; !ok {
		t.Error("expected 'vigilance' trait in happy-hogan's profile")
	}
}

// ── Get – unknown subject generates default ───────────────────────────────────

func TestGet_UnknownSubject_GeneratesDefault(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("unknown-subject")
	if resp == nil {
		t.Fatal("expected non-nil default profile")
	}
	if resp.SubjectId != "unknown-subject" {
		t.Errorf("SubjectId: got %q, want %q", resp.SubjectId, "unknown-subject")
	}
	// Default has standard traits.
	expectedTraits := []string{"curiosity", "aggression", "cooperation", "risk_taking", "adaptability"}
	for _, trait := range expectedTraits {
		if _, ok := resp.TraitScores[trait]; !ok {
			t.Errorf("expected trait %q in default profile", trait)
		}
	}
}

func TestGet_UnknownSubject_HasInteractionPatterns(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("brand-new-person")
	if len(resp.PreferredInteractionPatterns) == 0 {
		t.Error("expected at least one interaction pattern in default profile")
	}
}

func TestGet_UnknownSubject_ProfileUpdatedAtSet(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("someone-new")
	if resp.ProfileUpdatedAt == nil {
		t.Error("expected ProfileUpdatedAt to be set on default profile")
	}
}

// ── Get – idempotent for same subject ─────────────────────────────────────────

func TestGet_SameSubject_ReturnsSameInstance(t *testing.T) {
	p := newTestProfiler(t)
	r1 := p.Get("tony-stark")
	r2 := p.Get("tony-stark")
	if r1 != r2 {
		t.Error("expected same pointer for repeated Get of seeded subject")
	}
}

func TestGet_UnknownSubject_StoredOnSecondCall(t *testing.T) {
	p := newTestProfiler(t)
	r1 := p.Get("new-person-xyz")
	r2 := p.Get("new-person-xyz")
	// The second call should return the stored instance.
	if r1 != r2 {
		t.Error("expected same pointer on second call for newly created profile")
	}
}

// ── Get – empty string subject ────────────────────────────────────────────────

func TestGet_EmptySubjectID_ReturnsProfile(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("")
	if resp == nil {
		t.Fatal("expected non-nil profile for empty subject ID")
	}
	if resp.SubjectId != "" {
		t.Errorf("SubjectId: got %q, want empty string", resp.SubjectId)
	}
}

// ── Get – seeded subjects have ProfileUpdatedAt set ──────────────────────────

func TestGet_SeededSubjects_ProfileUpdatedAtSet(t *testing.T) {
	p := newTestProfiler(t)
	for _, subject := range []string{"tony-stark", "pepper-potts", "happy-hogan"} {
		resp := p.Get(subject)
		if resp.ProfileUpdatedAt == nil {
			t.Errorf("subject %q: expected ProfileUpdatedAt to be non-nil", subject)
		}
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestGet_ConcurrentSafe(t *testing.T) {
	p := newTestProfiler(t)
	subjects := []string{"tony-stark", "pepper-potts", "happy-hogan", "new-alpha", "new-beta"}

	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			subject := subjects[idx%len(subjects)]
			resp := p.Get(subject)
			if resp == nil {
				t.Errorf("nil response for subject %q", subject)
			}
		}(i)
	}
	wg.Wait()
}

// ── Trait values are in expected range ───────────────────────────────────────

func TestGet_TonyStark_TraitValuesInRange(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("tony-stark")
	for trait, score := range resp.TraitScores {
		if score < 0.0 || score > 1.0 {
			t.Errorf("trait %q score %v out of [0,1] range", trait, score)
		}
	}
}

func TestGet_DefaultProfile_TraitValuesInRange(t *testing.T) {
	p := newTestProfiler(t)
	resp := p.Get("any-unknown-person")
	for trait, score := range resp.TraitScores {
		if score < 0.0 || score > 1.0 {
			t.Errorf("trait %q score %v out of [0,1] range", trait, score)
		}
	}
}
