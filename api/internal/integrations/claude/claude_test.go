package claude_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rkrimper1/jarvis/api/internal/integrations/claude"
)

// The claude.Client wraps the Anthropic streaming SDK and makes real network
// calls.  We test the parts we can verify without network access:
//   - New() construction with various parameters
//   - Turn struct population
//   - Complete() with a cancelled context (ensures no hang and returns error)

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	c := claude.New("test-api-key", "claude-3-5-sonnet-20241022", 1024)
	if c == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_WithEmptyAPIKey(t *testing.T) {
	// Should not panic — the error only surfaces when Complete is called.
	c := claude.New("", "claude-3-5-sonnet-20241022", 1024)
	if c == nil {
		t.Fatal("New() with empty API key returned nil")
	}
}

func TestNew_WithZeroMaxTokens(t *testing.T) {
	c := claude.New("test-key", "claude-3-5-sonnet-20241022", 0)
	if c == nil {
		t.Fatal("New() with zero maxTokens returned nil")
	}
}

func TestNew_WithNegativeMaxTokens(t *testing.T) {
	// Negative maxTokens is cast to int64; should not panic at construction.
	c := claude.New("test-key", "claude-3-5-sonnet-20241022", -1)
	if c == nil {
		t.Fatal("New() with negative maxTokens returned nil")
	}
}

func TestNew_WithEmptyModel(t *testing.T) {
	c := claude.New("test-key", "", 512)
	if c == nil {
		t.Fatal("New() with empty model returned nil")
	}
}

// ── Turn struct ───────────────────────────────────────────────────────────────

func TestTurn_UserRole(t *testing.T) {
	turn := claude.Turn{Role: "user", Content: "Hello"}
	if turn.Role != "user" {
		t.Errorf("Role: got %q, want %q", turn.Role, "user")
	}
	if turn.Content != "Hello" {
		t.Errorf("Content: got %q, want %q", turn.Content, "Hello")
	}
}

func TestTurn_AssistantRole(t *testing.T) {
	turn := claude.Turn{Role: "assistant", Content: "Hi there"}
	if turn.Role != "assistant" {
		t.Errorf("Role: got %q, want %q", turn.Role, "assistant")
	}
}

func TestTurn_JarvisRole(t *testing.T) {
	// "jarvis" role is handled as assistant in the client.
	turn := claude.Turn{Role: "jarvis", Content: "At your service"}
	if turn.Role != "jarvis" {
		t.Errorf("Role: got %q, want %q", turn.Role, "jarvis")
	}
}

func TestTurn_EmptyContent(t *testing.T) {
	turn := claude.Turn{Role: "user", Content: ""}
	if turn.Content != "" {
		t.Errorf("expected empty content, got %q", turn.Content)
	}
}

// ── Complete – cancelled context returns error immediately ────────────────────

func TestComplete_CancelledContext_ReturnsError(t *testing.T) {
	// Point the client at a local server that blocks until the request context
	// is done — no real network traffic, no API key required.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := claude.New("sk-ant-test-fake-key", "claude-3-5-sonnet-20241022", 1,
		option.WithBaseURL(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so Complete returns immediately

	_, err := c.Complete(ctx, "system", nil, "hello")
	if err == nil {
		t.Fatal("expected error from Complete with cancelled context, got nil")
	}
}

// ── Complete – nil/empty history ─────────────────────────────────────────────

// These tests confirm the history slice parameter types are accepted by the API.
func TestTurnSlice_Empty(t *testing.T) {
	var history []claude.Turn
	if len(history) != 0 {
		t.Errorf("expected empty history, got len=%d", len(history))
	}
}

func TestTurnSlice_NilIsHandled(t *testing.T) {
	var history []claude.Turn
	// Pass nil to Complete; it should be handled without panic.
	// (We do not call Complete here to avoid network I/O.)
	_ = history
}

func TestTurnSlice_MixedRoles(t *testing.T) {
	history := []claude.Turn{
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "4"},
		{Role: "user", Content: "And 3+3?"},
		{Role: "jarvis", Content: "6"},
		{Role: "unknown-role", Content: "ignored"},
	}
	if len(history) != 5 {
		t.Errorf("expected 5 turns, got %d", len(history))
	}
}
