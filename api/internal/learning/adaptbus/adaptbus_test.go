package adaptbus_test

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/learning/adaptbus"
	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// newTestBus creates a Bus with a very long simulation interval so the
// background goroutine never fires during tests, and a reasonable buffer size.
func newTestBus(t *testing.T) *adaptbus.Bus {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Use a 24-hour simulation interval so the simulator never fires in tests.
	b := adaptbus.New(8, 24*time.Hour, logger)
	t.Cleanup(b.Stop)
	return b
}

func newTestEvent(domain learningv1.ModelDomain, desc string, delta float32) *learningv1.AdaptationEvent {
	return &learningv1.AdaptationEvent{
		EventId:       "test-event-1",
		Domain:        domain,
		Description:   desc,
		DeltaAccuracy: delta,
		Timestamp:     timestamppb.Now(),
	}
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNil(t *testing.T) {
	b := newTestBus(t)
	if b == nil {
		t.Fatal("New() returned nil")
	}
}

// ── Subscribe / Unsubscribe ───────────────────────────────────────────────────

func TestSubscribe_ReturnsChannel(t *testing.T) {
	b := newTestBus(t)
	ch, unsub := b.Subscribe("sub-1")
	defer unsub()
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
}

func TestSubscribe_UnsubscribeClosesChannel(t *testing.T) {
	b := newTestBus(t)
	ch, unsub := b.Subscribe("sub-close")
	unsub()
	// After unsubscribe the channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsub")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("channel was not closed within timeout after unsub")
	}
}

func TestSubscribe_MultipleSubscribersGetEvents(t *testing.T) {
	b := newTestBus(t)
	ch1, unsub1 := b.Subscribe("sub-a")
	ch2, unsub2 := b.Subscribe("sub-b")
	defer unsub1()
	defer unsub2()

	ev := newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "test publish", 0.01)
	b.Publish(ev)

	for label, ch := range map[string]<-chan *learningv1.AdaptationEvent{
		"sub-a": ch1,
		"sub-b": ch2,
	} {
		select {
		case got := <-ch:
			if got == nil {
				t.Errorf("%s: received nil event", label)
			}
		case <-time.After(500 * time.Millisecond):
			t.Errorf("%s: timed out waiting for event", label)
		}
	}
}

// ── Publish ───────────────────────────────────────────────────────────────────

func TestPublish_DeliverEventToSubscriber(t *testing.T) {
	b := newTestBus(t)
	ch, unsub := b.Subscribe("sub-deliver")
	defer unsub()

	ev := newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_THREAT, "threat recalibrated", 0.031)
	b.Publish(ev)

	select {
	case got := <-ch:
		if got.Description != ev.Description {
			t.Errorf("description: got %q, want %q", got.Description, ev.Description)
		}
		if got.DeltaAccuracy != ev.DeltaAccuracy {
			t.Errorf("delta: got %v, want %v", got.DeltaAccuracy, ev.DeltaAccuracy)
		}
		if got.Domain != ev.Domain {
			t.Errorf("domain: got %v, want %v", got.Domain, ev.Domain)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for published event")
	}
}

func TestPublish_NoSubscribers_NoBlock(t *testing.T) {
	b := newTestBus(t)
	// Should not block when there are no subscribers.
	done := make(chan struct{})
	go func() {
		b.Publish(newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "no subs", 0.01))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Publish blocked with no subscribers")
	}
}

func TestPublish_SlowSubscriber_DropsEventDoesNotBlock(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	// Buffer size of 1 so it fills up quickly.
	b := adaptbus.New(1, 24*time.Hour, logger)
	t.Cleanup(b.Stop)
	ch, unsub := b.Subscribe("slow-sub")
	defer unsub()

	// Fill the buffer.
	b.Publish(newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "first", 0.01))
	// This second publish should not block even though the buffer is full.
	done := make(chan struct{})
	go func() {
		b.Publish(newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "second dropped", 0.01))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("Publish blocked on full subscriber buffer")
	}
	// Drain one event so the subscriber cleanup does not deadlock.
	select {
	case <-ch:
	default:
	}
}

func TestPublish_NilEvent_DoesNotPanic(t *testing.T) {
	b := newTestBus(t)
	ch, unsub := b.Subscribe("nil-sub")
	defer unsub()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Publish(nil) panicked: %v", r)
		}
	}()
	b.Publish(nil)

	// Drain if something was sent.
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
	}
}

// ── Subscribe – multiple calls same ID ───────────────────────────────────────

func TestSubscribe_SameID_OverwritesPrevious(t *testing.T) {
	b := newTestBus(t)
	_, unsub1 := b.Subscribe("dup-id")
	ch2, unsub2 := b.Subscribe("dup-id")
	defer unsub2()
	// unsub1 closes the old channel without evicting ch2 from the map, because
	// Subscribe guards the delete with a channel-identity check.
	defer unsub1()

	// Publishing should reach ch2.
	ev := newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "dup test", 0.01)
	b.Publish(ev)

	select {
	case got := <-ch2:
		if got == nil {
			t.Error("expected non-nil event on ch2")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for event on ch2 after dup subscribe")
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestPublish_ConcurrentSafe(t *testing.T) {
	b := newTestBus(t)
	const numSubs = 5
	unsubs := make([]func(), numSubs)
	for i := 0; i < numSubs; i++ {
		_, u := b.Subscribe("concurrent-sub-" + string(rune('A'+i)))
		unsubs[i] = u
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR, "concurrent", 0.005))
		}()
	}
	wg.Wait()
}

func TestSubscribeUnsubscribe_ConcurrentSafe(t *testing.T) {
	b := newTestBus(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			id := "goroutine-sub-" + string(rune('a'+n%26))
			ch, unsub := b.Subscribe(id)
			// Immediately publish one event from another goroutine.
			go func() {
				defer wg.Done()
				b.Publish(newTestEvent(learningv1.ModelDomain_MODEL_DOMAIN_NLP, "race", 0.001))
			}()
			// Drain briefly.
			select {
			case <-ch:
			case <-time.After(50 * time.Millisecond):
			}
			unsub()
		}(i)
	}
	wg.Wait()
}
