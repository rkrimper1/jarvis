package session_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	voicev1  "github.com/rkrimper1/jarvis/gen/voice"
	"github.com/rkrimper1/jarvis/services/voice-service/internal/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func makeConfig(sessionID, userID string, tags ...string) *voicev1.StreamConfig {
	return &voicev1.StreamConfig{
		Meta: &commonv1.RequestMeta{
			SessionId: sessionID,
			UserId:    userID,
			Timestamp: timestamppb.Now(),
		},
		ContextTags: tags,
	}
}

// ── Create / Get ──────────────────────────────────────────────────────────────

func TestStore_CreateAndGet(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)

	sess := store.Create(makeConfig("sess-1", "tony"))
	if sess == nil {
		t.Fatal("Create returned nil for first session")
	}
	if sess.ID != "sess-1" {
		t.Errorf("ID = %q, want %q", sess.ID, "sess-1")
	}
	if sess.UserID != "tony" {
		t.Errorf("UserID = %q, want %q", sess.UserID, "tony")
	}
	if sess.State != session.StateIdle {
		t.Errorf("initial state = %v, want StateIdle", sess.State)
	}

	got, ok := store.Get("sess-1")
	if !ok {
		t.Fatal("Get returned false for existing session")
	}
	if got.ID != "sess-1" {
		t.Errorf("got ID = %q, want %q", got.ID, "sess-1")
	}
}

func TestStore_GetNonExistent(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	_, ok := store.Get("does-not-exist")
	if ok {
		t.Error("Get should return false for unknown session ID")
	}
}

func TestStore_ContextTagsPreserved(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	sess := store.Create(makeConfig("sess-tags", "pepper", "hardware", "security"))
	if sess == nil {
		t.Fatal("Create returned nil")
	}
	if len(sess.ContextTags) != 2 {
		t.Errorf("ContextTags len = %d, want 2", len(sess.ContextTags))
	}
}

// ── Capacity cap ─────────────────────────────────────────────────────────────

func TestStore_CapacityCap(t *testing.T) {
	store := session.NewStore(10*time.Minute, 3)

	for i := range 3 {
		id := fmt.Sprintf("sess-%d", i)
		if sess := store.Create(makeConfig(id, "user")); sess == nil {
			t.Fatalf("Create returned nil for session %d (under cap)", i)
		}
	}

	overflow := store.Create(makeConfig("sess-overflow", "user"))
	if overflow != nil {
		t.Error("Create should return nil when store is at capacity")
	}
}

// ── Touch ─────────────────────────────────────────────────────────────────────

func TestStore_Touch_UpdatesLastActive(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-touch", "user"))

	before, _ := store.Get("sess-touch")
	t1 := before.LastActive

	time.Sleep(5 * time.Millisecond)
	store.Touch("sess-touch")

	after, _ := store.Get("sess-touch")
	if !after.LastActive.After(t1) {
		t.Error("Touch did not advance LastActive")
	}
}

func TestStore_Touch_UnknownID_NoOp(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	store.Touch("ghost") // must not panic
}

// ── SetState ──────────────────────────────────────────────────────────────────

func TestStore_SetState(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-state", "user"))

	transitions := []session.State{
		session.StateListening,
		session.StateProcessing,
		session.StateSpeaking,
		session.StateIdle,
	}

	for _, want := range transitions {
		store.SetState("sess-state", want)
		got, ok := store.Get("sess-state")
		if !ok {
			t.Fatal("Get returned false after SetState")
		}
		if got.State != want {
			t.Errorf("state = %v, want %v", got.State, want)
		}
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestStore_Delete(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	store.Create(makeConfig("sess-del", "user"))

	store.Delete("sess-del")
	_, ok := store.Get("sess-del")
	if ok {
		t.Error("session should not exist after Delete")
	}
}

func TestStore_Delete_UnknownID_NoOp(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	store.Delete("never-existed") // must not panic
}

// ── NLPSession shares session_id ─────────────────────────────────────────────

func TestStore_NLPSessionMatchesSessionID(t *testing.T) {
	store := session.NewStore(10*time.Minute, 100)
	sess := store.Create(makeConfig("sess-nlp", "user"))
	if sess.NLPSession != "sess-nlp" {
		t.Errorf("NLPSession = %q, want %q", sess.NLPSession, "sess-nlp")
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestStore_ConcurrentAccess(t *testing.T) {
	store := session.NewStore(10*time.Minute, 1000)
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", n)
			store.Create(makeConfig(id, "user"))
			store.Touch(id)
			store.SetState(id, session.StateListening)
			store.SetState(id, session.StateIdle)
			store.Delete(id)
		}(i)
	}

	wg.Wait()
}
