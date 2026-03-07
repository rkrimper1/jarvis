package eventbus_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/eventbus"
)

func newBus() *eventbus.Bus {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return eventbus.New(16, log)
}

func TestBus_SubscribeAndReceive(t *testing.T) {
	b := newBus()

	ch, unsub := b.Subscribe("sub-1")
	defer unsub()

	event := b.NewEvent("mark-vii", "task-1", "TASK_STARTED", `{}`, commonv1.Severity_SEVERITY_INFO)
	b.Publish(event)

	select {
	case received := <-ch:
		if received.AgentId != "mark-vii" {
			t.Errorf("agent_id = %q, want %q", received.AgentId, "mark-vii")
		}
		if received.EventType != "TASK_STARTED" {
			t.Errorf("event_type = %q, want %q", received.EventType, "TASK_STARTED")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout: expected to receive event")
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	b := newBus()

	ch1, unsub1 := b.Subscribe("sub-1")
	ch2, unsub2 := b.Subscribe("sub-2")
	defer unsub1()
	defer unsub2()

	b.Publish(b.NewEvent("drone-01", "task-2", "TASK_COMPLETED", `{}`, commonv1.Severity_SEVERITY_INFO))

	for _, ch := range []<-chan interface{ GetEventType() string }{} {
		_ = ch
	}

	// Both channels should receive the event
	for i, ch := range []<-chan interface{}{
		func() <-chan interface{} {
			out := make(chan interface{}, 1)
			go func() {
				if e, ok := <-ch1; ok {
					out <- e
				}
			}()
			return out
		}(),
		func() <-chan interface{} {
			out := make(chan interface{}, 1)
			go func() {
				if e, ok := <-ch2; ok {
					out <- e
				}
			}()
			return out
		}(),
	} {
		select {
		case <-ch:
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d: timeout waiting for event", i+1)
		}
	}
}

func TestBus_UnsubscribeClosesChannel(t *testing.T) {
	b := newBus()

	ch, unsub := b.Subscribe("sub-temp")
	unsub()

	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsubscribe")
	}
}

func TestBus_SubscriberCount(t *testing.T) {
	b := newBus()

	if b.SubscriberCount() != 0 {
		t.Errorf("initial subscriber count = %d, want 0", b.SubscriberCount())
	}

	_, unsub1 := b.Subscribe("s1")
	_, unsub2 := b.Subscribe("s2")

	if b.SubscriberCount() != 2 {
		t.Errorf("subscriber count = %d, want 2", b.SubscriberCount())
	}

	unsub1()
	if b.SubscriberCount() != 1 {
		t.Errorf("after unsub1: count = %d, want 1", b.SubscriberCount())
	}

	unsub2()
	if b.SubscriberCount() != 0 {
		t.Errorf("after unsub2: count = %d, want 0", b.SubscriberCount())
	}
}

func TestBus_NewEvent_UniqueIDs(t *testing.T) {
	b := newBus()

	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		e := b.NewEvent("agent", "task", "TYPE", "{}", commonv1.Severity_SEVERITY_INFO)
		if seen[e.EventId] {
			t.Errorf("duplicate event ID: %s", e.EventId)
		}
		seen[e.EventId] = true
	}
}

func TestBus_SlowSubscriberDropsEvent(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	// Buffer of 1 — will fill immediately
	b := eventbus.New(1, log)

	ch, unsub := b.Subscribe("slow-sub")
	defer unsub()

	// Fill the buffer
	b.Publish(b.NewEvent("a", "t", "E1", "{}", commonv1.Severity_SEVERITY_INFO))
	// This should be dropped without blocking
	b.Publish(b.NewEvent("a", "t", "E2", "{}", commonv1.Severity_SEVERITY_INFO))

	// Only 1 event should be in the channel (second was dropped)
	count := len(ch)
	if count != 1 {
		t.Errorf("channel depth = %d, want 1 (second event should be dropped)", count)
	}
}
