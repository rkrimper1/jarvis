package messaging_test

import (
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/business-ops/messaging"
	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestRouter(t *testing.T) *messaging.Router {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return messaging.New(logger)
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	r := newTestRouter(t)
	if r == nil {
		t.Fatal("New() returned nil")
	}
}

// ── Send – happy paths ────────────────────────────────────────────────────────

func TestSend_SingleRecipient_Delivered(t *testing.T) {
	r := newTestRouter(t)
	msgID, delivered, failed := r.Send(
		[]string{"pepper@stark.com"},
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Test Subject", "Test body", false,
	)
	if msgID == "" {
		t.Error("expected non-empty messageID")
	}
	if len(delivered) != 1 {
		t.Errorf("delivered: want 1, got %d", len(delivered))
	}
	if len(failed) != 0 {
		t.Errorf("failed: want 0, got %d: %v", len(failed), failed)
	}
	if delivered[0] != "pepper@stark.com" {
		t.Errorf("delivered[0]: got %q, want %q", delivered[0], "pepper@stark.com")
	}
}

func TestSend_MultipleRecipients_AllDelivered(t *testing.T) {
	r := newTestRouter(t)
	recipients := []string{"tony@stark.com", "pepper@stark.com", "happy@stark.com"}
	_, delivered, failed := r.Send(
		recipients,
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Subject", "Body", false,
	)
	if len(delivered) != len(recipients) {
		t.Errorf("delivered: want %d, got %d", len(recipients), len(delivered))
	}
	if len(failed) != 0 {
		t.Errorf("failed: want 0, got %d: %v", len(failed), failed)
	}
}

func TestSend_AllChannels_NoError(t *testing.T) {
	r := newTestRouter(t)
	channels := []businessv1.MessageChannel{
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		businessv1.MessageChannel_MESSAGE_CHANNEL_SMS,
		businessv1.MessageChannel_MESSAGE_CHANNEL_SLACK,
		businessv1.MessageChannel_MESSAGE_CHANNEL_SECURE,
		businessv1.MessageChannel_MESSAGE_CHANNEL_UNSPECIFIED,
	}
	for _, ch := range channels {
		_, delivered, failed := r.Send(
			[]string{"tony@stark.com"},
			ch,
			"Test", "Body", false,
		)
		if len(delivered) != 1 {
			t.Errorf("channel %v: expected 1 delivered, got %d", ch, len(delivered))
		}
		if len(failed) != 0 {
			t.Errorf("channel %v: expected 0 failed, got %d", ch, len(failed))
		}
	}
}

func TestSend_EncryptedMessage_Delivered(t *testing.T) {
	r := newTestRouter(t)
	_, delivered, failed := r.Send(
		[]string{"secure@stark.com"},
		businessv1.MessageChannel_MESSAGE_CHANNEL_SECURE,
		"Classified", "Top secret body", true,
	)
	if len(delivered) != 1 {
		t.Errorf("expected 1 delivered for encrypted message, got %d", len(delivered))
	}
	if len(failed) != 0 {
		t.Errorf("expected 0 failed for encrypted message, got %d", len(failed))
	}
}

// ── Send – error paths ────────────────────────────────────────────────────────

func TestSend_EmptyRecipient_Fails(t *testing.T) {
	r := newTestRouter(t)
	_, delivered, failed := r.Send(
		[]string{""},
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Subject", "Body", false,
	)
	if len(failed) != 1 {
		t.Errorf("failed: want 1, got %d", len(failed))
	}
	if len(delivered) != 0 {
		t.Errorf("delivered: want 0, got %d: %v", len(delivered), delivered)
	}
	if len(failed) > 0 && failed[0] != "" {
		t.Errorf("failed[0]: expected empty string recipient, got %q", failed[0])
	}
}

func TestSend_MixedValidAndEmptyRecipients(t *testing.T) {
	r := newTestRouter(t)
	_, delivered, failed := r.Send(
		[]string{"valid@stark.com", "", "also-valid@stark.com"},
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Subject", "Body", false,
	)
	if len(delivered) != 2 {
		t.Errorf("delivered: want 2, got %d", len(delivered))
	}
	if len(failed) != 1 {
		t.Errorf("failed: want 1, got %d", len(failed))
	}
}

// ── Send – empty recipients slice ────────────────────────────────────────────

func TestSend_EmptyRecipientSlice_ReturnsMessageID(t *testing.T) {
	r := newTestRouter(t)
	msgID, delivered, failed := r.Send(
		[]string{},
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Subject", "Body", false,
	)
	if msgID == "" {
		t.Error("expected non-empty messageID even with no recipients")
	}
	if len(delivered) != 0 {
		t.Errorf("delivered: want 0, got %d", len(delivered))
	}
	if len(failed) != 0 {
		t.Errorf("failed: want 0, got %d", len(failed))
	}
}

func TestSend_NilRecipientSlice_ReturnsMessageID(t *testing.T) {
	r := newTestRouter(t)
	msgID, delivered, failed := r.Send(
		nil,
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"Subject", "Body", false,
	)
	if msgID == "" {
		t.Error("expected non-empty messageID even with nil recipients")
	}
	_ = delivered
	_ = failed
}

// ── MessageIDs are unique ─────────────────────────────────────────────────────

func TestSend_MessageIDsAreUnique(t *testing.T) {
	r := newTestRouter(t)
	ids := make(map[string]bool)
	for i := 0; i < 10; i++ {
		id, _, _ := r.Send([]string{"a@b.com"}, businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL, "S", "B", false)
		if ids[id] {
			t.Errorf("duplicate messageID: %q", id)
		}
		ids[id] = true
	}
}

func TestSend_MessageIDHasMsgPrefix(t *testing.T) {
	r := newTestRouter(t)
	id, _, _ := r.Send([]string{"a@b.com"}, businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL, "S", "B", false)
	if len(id) < 4 || id[:4] != "msg-" {
		t.Errorf("expected ID to start with 'msg-', got %q", id)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestSend_ConcurrentSafe(t *testing.T) {
	r := newTestRouter(t)
	var wg sync.WaitGroup
	const goroutines = 30
	ids := make([]string, goroutines)
	var mu sync.Mutex

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id, _, _ := r.Send(
				[]string{"user@stark.com"},
				businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
				"Concurrent", "body", false,
			)
			mu.Lock()
			ids[n] = id
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// All IDs must be non-empty and unique.
	seen := make(map[string]bool)
	for _, id := range ids {
		if id == "" {
			t.Error("got empty messageID in concurrent test")
		}
		if seen[id] {
			t.Errorf("duplicate messageID in concurrent test: %q", id)
		}
		seen[id] = true
	}
}

// ── Subject and body edge cases ───────────────────────────────────────────────

func TestSend_EmptySubjectAndBody_OK(t *testing.T) {
	r := newTestRouter(t)
	id, delivered, failed := r.Send(
		[]string{"a@b.com"},
		businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		"", "", false,
	)
	if id == "" {
		t.Error("expected non-empty messageID")
	}
	if len(delivered) != 1 {
		t.Errorf("expected 1 delivered, got %d", len(delivered))
	}
	_ = failed
}
