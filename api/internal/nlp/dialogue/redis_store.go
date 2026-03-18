// Package dialogue — Redis-backed session store.
package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore persists dialogue sessions in Redis.
// Each session is stored as a JSON-encoded slice of turns under the key
// "dialogue:session:<sessionID>", with a sliding TTL.
type RedisStore struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewRedisStore creates a RedisStore backed by the given Redis client.
func NewRedisStore(rdb *redis.Client, ttl time.Duration) *RedisStore {
	return &RedisStore{rdb: rdb, ttl: ttl}
}

type storedTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func sessionKey(sessionID string) string {
	return fmt.Sprintf("dialogue:session:%s", sessionID)
}

// Load retrieves the conversation history for a session.
// Returns an empty slice if the session does not exist.
func (s *RedisStore) Load(ctx context.Context, sessionID string) ([]storedTurn, error) {
	data, err := s.rdb.Get(ctx, sessionKey(sessionID)).Bytes()
	if err == redis.Nil {
		return []storedTurn{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get session: %w", err)
	}

	var turns []storedTurn
	if err := json.Unmarshal(data, &turns); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return turns, nil
}

// Append adds a user + assistant turn pair and resets the TTL.
func (s *RedisStore) Append(ctx context.Context, sessionID, userText, assistantText string, maxTurns int) error {
	turns, err := s.Load(ctx, sessionID)
	if err != nil {
		return err
	}

	turns = append(turns,
		storedTurn{Role: "user", Content: userText},
		storedTurn{Role: "assistant", Content: assistantText},
	)

	// Trim to window: maxTurns * 2 messages (user + assistant each)
	if maxTurns > 0 && len(turns) > maxTurns*2 {
		turns = turns[len(turns)-maxTurns*2:]
	}

	data, err := json.Marshal(turns)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	return s.rdb.Set(ctx, sessionKey(sessionID), data, s.ttl).Err()
}

// Delete removes a session from Redis.
func (s *RedisStore) Delete(ctx context.Context, sessionID string) error {
	return s.rdb.Del(ctx, sessionKey(sessionID)).Err()
}
