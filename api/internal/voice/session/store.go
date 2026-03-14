// Package session manages voice session state.
//
// Architecture — two-tier storage:
//
//	Provider "memory" (default):
//	  MemoryStore — sync.RWMutex-guarded map.
//	  Zero dependencies, works out-of-the-box with docker compose up.
//	  Sessions are lost on process restart; fine for single-replica dev.
//
//	Provider "redis":
//	  RedisStore — go-redis/v9 backed.
//	  Sessions survive voice-service restarts and are shared across
//	  horizontal replicas. Keys use the pattern "jarvis:session:{id}".
//	  Expiry is set on every write so Redis auto-evicts stale sessions.
//
// Both implementations satisfy the Store interface — server.go is unaware
// of which backend is in use.
package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	voicev1 "github.com/rkrimper1/jarvis/api/pb/voice"
)

// ── State ────────────────────────────────────────────────────────────────────

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

// ── Session ───────────────────────────────────────────────────────────────────

// Session holds the runtime state for a single Converse stream.
type Session struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	NLPSession  string    `json:"nlp_session"` // session_id forwarded to nlp-service ProcessDialogueTurn
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	LastActive  time.Time `json:"last_active"`
																										 
	ContextTags []string  `json:"context_tags,omitempty"`
}

// ── Store interface ───────────────────────────────────────────────────────────

// Store is the contract both MemoryStore and RedisStore satisfy.
// All methods are safe for concurrent use.
type Store interface {
	// Create initialises a new Session from a StreamConfig.
	// Returns nil when the store is at capacity (MemoryStore) or if the
	// key already exists with a conflicting user (RedisStore uses NX).
	Create(cfg *voicev1.StreamConfig) *Session

	// Get retrieves a session by ID.
	Get(id string) (*Session, bool)

	// Touch updates LastActive and resets the Redis TTL.
	Touch(id string)

	// SetState transitions the session state and updates LastActive.
	SetState(id string, state State)

	// Delete removes a session immediately.
	Delete(id string)
}

// ── MemoryStore ───────────────────────────────────────────────────────────────

// MemoryStore is a thread-safe in-process session registry.
// Suitable for single-replica deployments and all automated tests.
type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	maxSize  int
}

// NewMemoryStore creates a MemoryStore with the given TTL and capacity cap.
// A background goroutine reaps expired sessions every minute.
func NewMemoryStore(ttl time.Duration, maxSize int) *MemoryStore {
	s := &MemoryStore{
		sessions: make(map[string]*Session, 64),
		ttl:      ttl,
		maxSize:  maxSize,
	}
	go s.reap()
	return s
}

																					
										   
func (s *MemoryStore) Create(cfg *voicev1.StreamConfig) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.sessions) >= s.maxSize {
		return nil
	}

	sess := &Session{
		ID:          cfg.GetMeta().GetSessionId(),
		UserID:      cfg.GetMeta().GetUserId(),
		NLPSession:  cfg.GetMeta().GetSessionId(),
		State:       StateIdle,
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
		ContextTags: cfg.GetContextTags(),
	}
	s.sessions[sess.ID] = sess
	return sess
}

								 
func (s *MemoryStore) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

										  
func (s *MemoryStore) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.LastActive = time.Now()
	}
}

																		
func (s *MemoryStore) SetState(id string, state State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.State = state
		sess.LastActive = time.Now()
	}
}

										
func (s *MemoryStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// reap evicts sessions whose LastActive is older than the TTL.
func (s *MemoryStore) reap() {
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

// ── RedisStore ────────────────────────────────────────────────────────────────

const redisKeyPrefix = "jarvis:session:"

// RedisStore persists sessions in Redis using JSON-encoded values.
//
// Key layout:   jarvis:session:{sessionID}   → JSON(Session)
// TTL policy:   every write (Create/Touch/SetState) resets the TTL to cfg.ttl
//               so active sessions never expire mid-conversation.
//
// Horizontal scaling: all voice-service replicas share the same Redis
// keyspace — any replica can serve GetSession for any session ID.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisStore dials Redis and returns a RedisStore.
// addr format: "host:port" (e.g. "redis:6379").
func NewRedisStore(addr, password string, db int, ttl time.Duration) (*RedisStore, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping %s: %w", addr, err)
	}

	return &RedisStore{client: rdb, ttl: ttl}, nil
}

func (s *RedisStore) key(id string) string {
	return redisKeyPrefix + id
}

func (s *RedisStore) Create(cfg *voicev1.StreamConfig) *Session {
	sess := &Session{
		ID:          cfg.GetMeta().GetSessionId(),
		UserID:      cfg.GetMeta().GetUserId(),
		NLPSession:  cfg.GetMeta().GetSessionId(),
		State:       StateIdle,
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
		ContextTags: cfg.GetContextTags(),
	}

	data, err := json.Marshal(sess)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// SetNX: only create if the key does not already exist.
	// This prevents duplicate session creation when replicas race.
	ok, err := s.client.SetNX(ctx, s.key(sess.ID), data, s.ttl).Result()
	if err != nil || !ok {
		return nil
	}
	return sess
}

func (s *RedisStore) Get(id string) (*Session, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := s.client.Get(ctx, s.key(id)).Bytes()
	if err != nil {
		return nil, false
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, false
	}
	return &sess, true
}

func (s *RedisStore) Touch(id string) {
	sess, ok := s.Get(id)
	if !ok {
		return
	}
	sess.LastActive = time.Now()
	s.set(sess)
}

func (s *RedisStore) SetState(id string, state State) {
	sess, ok := s.Get(id)
	if !ok {
		return
	}
	sess.State = state
	sess.LastActive = time.Now()
	s.set(sess)
}

func (s *RedisStore) Delete(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.client.Del(ctx, s.key(id)) //nolint:errcheck — delete is best-effort
}

// set serialises sess and writes it back with a fresh TTL.
func (s *RedisStore) set(sess *Session) {
	data, err := json.Marshal(sess)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.client.Set(ctx, s.key(sess.ID), data, s.ttl) //nolint:errcheck
}

// ── Factory ───────────────────────────────────────────────────────────────────

// StoreConfig holds everything NewStore needs to select and initialise a backend.
type StoreConfig struct {
	Provider string        // "memory" | "redis"
	TTL      time.Duration
	MaxSize  int           // MemoryStore only
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

// NewStore returns a Store backed by either MemoryStore or RedisStore.
// An error is returned only when Provider == "redis" and the dial fails.
func NewStore(cfg StoreConfig) (Store, error) {
	switch cfg.Provider {
	case "redis":
		return NewRedisStore(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.TTL)
	default:
		return NewMemoryStore(cfg.TTL, cfg.MaxSize), nil
	}
}
