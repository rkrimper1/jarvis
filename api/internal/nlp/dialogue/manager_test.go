package dialogue_test

import (
	"context"
	"testing"

	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
	claudeclient "github.com/rkrimper1/jarvis/api/internal/integrations/claude"
	"github.com/rkrimper1/jarvis/api/internal/nlp/dialogue"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"log/slog"
	"os"
	"time"
)

// newTestManager spins up a miniredis instance and returns a Manager
// wired to it. claudeClient is nil — deterministic intents don't need it,
// and Claude-routed intents are tested via a real key only in integration tests.
func newTestManager(t *testing.T) *dialogue.Manager {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := dialogue.NewRedisStore(rdb, 30*time.Minute)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// nil claude client — only safe for deterministic intents
	return dialogue.NewManager(store, nil, 20, 0.6, log)
}

func TestBuildReply_Deterministic(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	tests := []struct {
		intent           nlpv1.Intent
		confidence       float32
		wantConfirmation bool
	}{
		{nlpv1.Intent_INTENT_EMERGENCY, 0.97, false},
		{nlpv1.Intent_INTENT_COMMAND, 0.90, false},
		{nlpv1.Intent_INTENT_COMMAND, 0.40, true}, // below threshold → confirm
	}

	for _, tt := range tests {
		reply, requiresConfirmation, err := m.BuildReply(ctx, "sess-det", "test utterance", tt.intent, tt.confidence)
		if err != nil {
			t.Errorf("intent=%v: unexpected error: %v", tt.intent, err)
		}
		if reply == "" {
			t.Errorf("intent=%v: expected non-empty reply", tt.intent)
		}
		if requiresConfirmation != tt.wantConfirmation {
			t.Errorf("intent=%v confidence=%.2f: requiresConfirmation=%v, want %v",
				tt.intent, tt.confidence, requiresConfirmation, tt.wantConfirmation)
		}
	}
}

func TestBuildReply_ClaudeIntent_NilClient(t *testing.T) {
	// When the Claude client is nil, BuildReply should return a graceful fallback
	// rather than panicking for Claude-routed intents.
	m := newTestManager(t)
	ctx := context.Background()

	claudeIntents := []nlpv1.Intent{
		nlpv1.Intent_INTENT_ANALYSIS_REQUEST,
		nlpv1.Intent_INTENT_QUERY,
		nlpv1.Intent_INTENT_SMALL_TALK,
	}
	for _, intent := range claudeIntents {
		reply, _, _ := m.BuildReply(ctx, "sess-claude", "hello", intent, 0.9)
		if reply == "" {
			t.Errorf("intent=%v: expected fallback reply, got empty string", intent)
		}
	}
}

func TestRedisStore_AppendAndLoad(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := dialogue.NewRedisStore(rdb, 30*time.Minute)
	ctx := context.Background()

	// Empty session returns no turns
	turns, err := store.Load(ctx, "new-session")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turns) != 0 {
		t.Errorf("expected 0 turns, got %d", len(turns))
	}

	// Append a turn
	if err := store.Append(ctx, "new-session", "Hello", "Good evening, sir.", 20); err != nil {
		t.Fatalf("Append: %v", err)
	}

	turns, err = store.Load(ctx, "new-session")
	if err != nil {
		t.Fatalf("Load after Append: %v", err)
	}
	if len(turns) != 2 {
		t.Errorf("expected 2 turns (user+assistant), got %d", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Content != "Hello" {
		t.Errorf("unexpected turn[0]: %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Content != "Good evening, sir." {
		t.Errorf("unexpected turn[1]: %+v", turns[1])
	}
}

func TestRedisStore_TrimHistory(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store := dialogue.NewRedisStore(rdb, 30*time.Minute)
	ctx := context.Background()

	// Append 4 turns with maxTurns=2 — should keep only the last 2 (4 messages)
	for i := 0; i < 4; i++ {
		if err := store.Append(ctx, "trim-sess", "question", "answer", 2); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	turns, err := store.Load(ctx, "trim-sess")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(turns) != 4 { // 2 turns × 2 messages each
		t.Errorf("expected 4 messages after trim, got %d", len(turns))
	}
}

func TestMetaSuccess(t *testing.T) {
	meta := dialogue.MetaSuccess("req-123")
	if !meta.Success {
		t.Error("MetaSuccess should set Success=true")
	}
	if meta.RequestId != "req-123" {
		t.Errorf("RequestId = %q, want %q", meta.RequestId, "req-123")
	}
	if meta.Timestamp == nil {
		t.Error("Timestamp should not be nil")
	}
}

func TestMetaError(t *testing.T) {
	meta := dialogue.MetaError("req-456", "NLP_001", "classifier failed")
	if meta.Success {
		t.Error("MetaError should set Success=false")
	}
	if meta.ErrorCode != "NLP_001" {
		t.Errorf("ErrorCode = %q, want %q", meta.ErrorCode, "NLP_001")
	}
	if meta.ErrorMessage != "classifier failed" {
		t.Errorf("ErrorMessage = %q, want %q", meta.ErrorMessage, "classifier failed")
	}
}

// Ensure claudeclient import is used (compile check for the integration path).
var _ = (*claudeclient.Client)(nil)
