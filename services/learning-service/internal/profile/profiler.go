// Package profile manages behavioral profiles for users and entities.
// Profiles are used by JARVIS to personalise responses and predict intent.
package profile

import (
	"sync"
	"time"

	learningv1 "github.com/rkrimper1/jarvis/gen/learning"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Profiler manages subject behavioral profiles.
type Profiler struct {
	mu       sync.RWMutex
	profiles map[string]*learningv1.BehaviorProfileResponse
}

// New creates a Profiler seeded with known subjects.
func New() *Profiler {
	p := &Profiler{profiles: make(map[string]*learningv1.BehaviorProfileResponse)}
	p.seed()
	return p
}

// Get returns a subject's profile, generating a default if none exists.
func (p *Profiler) Get(subjectID string) *learningv1.BehaviorProfileResponse {
	p.mu.RLock()
	prof, ok := p.profiles[subjectID]
	p.mu.RUnlock()

	if ok {
		return prof
	}

	// Generate a default profile for unknown subjects
	newProf := &learningv1.BehaviorProfileResponse{
		SubjectId: subjectID,
		TraitScores: map[string]float32{
			"curiosity":    0.5,
			"aggression":   0.1,
			"cooperation":  0.7,
			"risk_taking":  0.3,
			"adaptability": 0.6,
		},
		PreferredInteractionPatterns: []string{"direct_query", "summary_first"},
		ProfileUpdatedAt:             timestamppb.New(time.Now()),
	}

	p.mu.Lock()
	p.profiles[subjectID] = newProf
	p.mu.Unlock()
	return newProf
}

func (p *Profiler) seed() {
	p.profiles["tony-stark"] = &learningv1.BehaviorProfileResponse{
		SubjectId: "tony-stark",
		TraitScores: map[string]float32{
			"curiosity":    0.98,
			"aggression":   0.65,
			"cooperation":  0.45,
			"risk_taking":  0.89,
			"adaptability": 0.92,
			"sarcasm":      0.97,
			"genius":       0.99,
		},
		PreferredInteractionPatterns: []string{
			"brief_technical", "sarcasm_allowed", "skip_disclaimers",
			"direct_answers", "mission_critical_interrupts_ok",
		},
		ProfileUpdatedAt: timestamppb.New(time.Now().Add(-24 * time.Hour)),
	}

	p.profiles["pepper-potts"] = &learningv1.BehaviorProfileResponse{
		SubjectId: "pepper-potts",
		TraitScores: map[string]float32{
			"curiosity":    0.72,
			"aggression":   0.15,
			"cooperation":  0.95,
			"risk_taking":  0.25,
			"adaptability": 0.85,
			"patience":     0.80,
		},
		PreferredInteractionPatterns: []string{
			"formal_tone", "full_context", "calendar_sync", "email_summaries",
		},
		ProfileUpdatedAt: timestamppb.New(time.Now().Add(-12 * time.Hour)),
	}

	p.profiles["happy-hogan"] = &learningv1.BehaviorProfileResponse{
		SubjectId: "happy-hogan",
		TraitScores: map[string]float32{
			"curiosity":    0.40,
			"aggression":   0.55,
			"cooperation":  0.70,
			"risk_taking":  0.50,
			"vigilance":    0.95,
		},
		PreferredInteractionPatterns: []string{
			"security_alerts_priority", "plain_english", "action_items_only",
		},
		ProfileUpdatedAt: timestamppb.New(time.Now().Add(-6 * time.Hour)),
	}
}
