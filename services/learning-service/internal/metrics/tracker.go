// Package metrics tracks performance metrics for each model domain.
package metrics

import (
	"math/rand"
	"sync"
	"time"

	learningv1 "github.com/rkrimper1/jarvis/gen/learning"
)

// Snapshot holds current performance metrics for one domain.
type Snapshot struct {
	Domain              learningv1.ModelDomain
	Accuracy            float32
	Precision           float32
	Recall              float32
	TotalInferences     int64
	DegradationWarnings []string
	RecordedAt          time.Time
}

// Tracker maintains rolling performance snapshots per domain.
type Tracker struct {
	mu        sync.RWMutex
	snapshots map[learningv1.ModelDomain]*Snapshot
}

// New creates a Tracker with seeded baselines.
func New() *Tracker {
	t := &Tracker{snapshots: make(map[learningv1.ModelDomain]*Snapshot)}
	t.seed()
	return t
}

// Get returns the current metrics snapshot for a domain.
// Applies a small random drift to simulate a live model.
func (t *Tracker) Get(domain learningv1.ModelDomain, from, to time.Time) *Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.snapshots[domain]
	if !ok {
		// Unknown domain — return a neutral baseline
		s = &Snapshot{
			Domain:          domain,
			Accuracy:        0.75,
			Precision:       0.74,
			Recall:          0.76,
			TotalInferences: 0,
			RecordedAt:      time.Now(),
		}
		t.snapshots[domain] = s
	}

	// Simulate live metric drift (±1%)
	drift := func(v float32) float32 {
		delta := float32(rand.Float64()-0.5) * 0.02
		r := v + delta
		if r > 1.0 {
			r = 1.0
		}
		if r < 0 {
			r = 0
		}
		return r
	}

	s.Accuracy = drift(s.Accuracy)
	s.Precision = drift(s.Precision)
	s.Recall = drift(s.Recall)
	s.TotalInferences += int64(rand.Intn(100))
	s.RecordedAt = time.Now()

	// Warn if accuracy drops below threshold
	s.DegradationWarnings = nil
	if s.Accuracy < 0.80 {
		s.DegradationWarnings = append(s.DegradationWarnings,
			"accuracy below 80% threshold — retraining recommended")
	}
	if s.Recall < 0.75 {
		s.DegradationWarnings = append(s.DegradationWarnings,
			"recall degradation detected — review training data distribution")
	}

	return s
}

func (t *Tracker) seed() {
	baselines := []Snapshot{
		{Domain: learningv1.ModelDomain_MODEL_DOMAIN_NLP, Accuracy: 0.94, Precision: 0.93, Recall: 0.95, TotalInferences: 1_284_002},
		{Domain: learningv1.ModelDomain_MODEL_DOMAIN_THREAT, Accuracy: 0.97, Precision: 0.96, Recall: 0.98, TotalInferences: 482_910},
		{Domain: learningv1.ModelDomain_MODEL_DOMAIN_HARDWARE, Accuracy: 0.91, Precision: 0.90, Recall: 0.92, TotalInferences: 210_445},
		{Domain: learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR, Accuracy: 0.88, Precision: 0.87, Recall: 0.89, TotalInferences: 95_330},
	}
	for i := range baselines {
		b := baselines[i]
		b.RecordedAt = time.Now()
		t.snapshots[b.Domain] = &b
	}
}
