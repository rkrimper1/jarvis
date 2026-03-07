// Package feedback stores interaction feedback for model training pipelines.
package feedback

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	learningv1 "github.com/rkrimper1/jarvis/gen/learning"
)

// Entry is one feedback record.
type Entry struct {
	ID             string
	InteractionID  string
	Type           learningv1.FeedbackType
	Correction     string
	Rating         float32
	Notes          string
	QueuedAt       time.Time
	TrainingSent   bool
}

// Store is a thread-safe feedback queue.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*Entry
	seq     atomic.Int64
}

// New creates a new Store.
func New() *Store {
	return &Store{entries: make(map[string]*Entry)}
}

// Submit records feedback and returns the feedback ID.
// Returns queued=true when the entry will be forwarded to the training pipeline.
func (s *Store) Submit(
	interactionID string,
	fbType learningv1.FeedbackType,
	correction string,
	rating float32,
	notes string,
) (id string, queued bool) {
	s.seq.Add(1)
	id = fmt.Sprintf("fb-%06d", s.seq.Load())

	// Queue for training if it's a correction or a very low rating
	queued = fbType == learningv1.FeedbackType_FEEDBACK_CORRECTION || rating < 0.4

	s.mu.Lock()
	s.entries[id] = &Entry{
		ID:            id,
		InteractionID: interactionID,
		Type:          fbType,
		Correction:    correction,
		Rating:        rating,
		Notes:         notes,
		QueuedAt:      time.Now(),
		TrainingSent:  queued,
	}
	s.mu.Unlock()
	return id, queued
}

// Count returns total feedback entries by type.
func (s *Store) Count() map[learningv1.FeedbackType]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[learningv1.FeedbackType]int)
	for _, e := range s.entries {
		counts[e.Type]++
	}
	return counts
}
