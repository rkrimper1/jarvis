package dialogue_test

import (
	"testing"
	"time"

	nlpv1 "github.com/rkrimper1/jarvis/gen/nlp"
	"github.com/rkrimper1/jarvis/services/nlp-service/internal/dialogue"
)

func TestManager_GetOrCreate(t *testing.T) {
	m := dialogue.NewManager(10, 30*time.Minute)

	s1 := m.GetOrCreate("session-1", "tony")
	if s1 == nil {
		t.Fatal("expected session, got nil")
	}
	if s1.ID != "session-1" {
		t.Errorf("session ID = %q, want %q", s1.ID, "session-1")
	}

	// Second call with same ID should return the same session
	s2 := m.GetOrCreate("session-1", "tony")
	if s1 != s2 {
		t.Error("expected same session pointer for same session ID")
	}
}

func TestManager_AppendTurn(t *testing.T) {
	m := dialogue.NewManager(3, 30*time.Minute)
	m.GetOrCreate("sess", "tony")

	m.AppendTurn("sess", "Hello", "Good evening, sir.")
	m.AppendTurn("sess", "Status?", "All systems nominal.")

	// Internal state check is indirect via BuildReply continuity — just verify no panic
}

func TestManager_BuildReply(t *testing.T) {
	m := dialogue.NewManager(10, 30*time.Minute)
	sess := m.GetOrCreate("test-sess", "tony")

	tests := []struct {
		intent               nlpv1.Intent
		confidence           float32
		confidenceThresh     float32
		wantConfirmation     bool
		wantNonEmptyReply    bool
	}{
		{nlpv1.Intent_INTENT_EMERGENCY, 0.97, 0.60, false, true},
		{nlpv1.Intent_INTENT_COMMAND, 0.90, 0.60, false, true},
		{nlpv1.Intent_INTENT_COMMAND, 0.40, 0.60, true, true},  // low confidence → confirm
		{nlpv1.Intent_INTENT_QUERY, 0.80, 0.60, false, true},
		{nlpv1.Intent_INTENT_SMALL_TALK, 0.85, 0.60, false, true},
		{nlpv1.Intent_INTENT_UNSPECIFIED, 0.00, 0.60, false, true},
	}

	for _, tt := range tests {
		reply, requiresConfirmation := m.BuildReply(sess, "test utterance", tt.intent, tt.confidenceThresh, tt.confidence)

		if tt.wantNonEmptyReply && reply == "" {
			t.Errorf("intent=%v: expected non-empty reply", tt.intent)
		}
		if requiresConfirmation != tt.wantConfirmation {
			t.Errorf("intent=%v confidence=%.2f: requiresConfirmation=%v, want %v",
				tt.intent, tt.confidence, requiresConfirmation, tt.wantConfirmation)
		}
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
