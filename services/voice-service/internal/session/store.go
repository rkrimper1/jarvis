// Package session manages in-memory voice session state.
// Each active Converse stream owns one Session that tracks the stream's
// current state, audio buffer, and NLP session ID.
package session

import (
	"sync"
	"time"

	voicev1 "github.com/rkrimper1/jarvis/gen/voice"
)

// State mirrors VoiceResponse StatusEvent states.
type State int

const (
	StateIdle       State = iota
	StateListening
	StateProcessing
	StateSpeaking
	StateError
	StateEnded
)

// Session holds the runtime state for a single Converse stream.
type Session struct {
	ID         string
	UserID     string
	NLPSession string // session_id forwarded to nlp-service ProcessDialogueTurn
	State      State
	CreatedAt  time.Time
	LastActive time.Time
	// ContextTags forwarded to NLPService.ParseIntent on each utterance.
	ContextTags []string
}

// Store is a thread-safe in-memory session registry.
// In a production deployment this would be backed by Redis so sessions
// survive voice-service restarts and horizontal scaling.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	maxSize  int
}

// NewStore creates a Store with the given TTL and capacity cap.
func NewStore(ttl time.Duration, maxSize int) *Store {
	s := &Store{
		sessions: make(map[string]*Session, 64),
		ttl:      ttl,
		maxSize:  maxSize,
	}
	go s.reap()
	return s
}

// Create initialises a new Session from a StreamConfig.
// Returns nil if the store is at capacity.
func (s *Store) Create(cfg *voicev1.StreamConfig) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) >= s.maxSize {
		return nil
	}

	sess := &Session{
		ID:          cfg.GetMeta().GetSessionId(),
		UserID:      cfg.GetMeta().GetUserId(),
		NLPSession:  cfg.GetMeta().GetSessionId(), // share the same session_id with NLP
		State:       StateIdle,
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
		ContextTags: cfg.GetContextTags(),
	}
	s.sessions[sess.ID] = sess
	return sess
}

// Get retrieves a session by ID.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// Touch updates LastActive for a session.
func (s *Store) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.LastActive = time.Now()
	}
}

// SetState transitions a session's State field.
func (s *Store) SetState(id string, state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.State = state
		sess.LastActive = time.Now()
	}
}

// Delete removes a session immediately.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// reap periodically evicts sessions that have exceeded their TTL.
func (s *Store) reap() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		cutoff := time.Now().Add(-s.ttl)
		for id, sess := range s.sessions {
			if sess.LastActive.Before(cutoff) {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}
