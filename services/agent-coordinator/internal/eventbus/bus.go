// Package eventbus provides a pub/sub fan-out bus for CoordinationEvents.
//
// Subscribers receive a channel of events. The bus never blocks on a slow
// subscriber — it drops the event and logs a warning instead. This is the
// same back-pressure pattern used in the SecurityService AlertBroadcaster,
// and is worth teaching as a standard Go streaming idiom.
package eventbus

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	agentv1 "github.com/rkrimper1/jarvis/gen/agent"
	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Bus fans out CoordinationEvents to all active subscribers.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]chan *agentv1.CoordinationEvent
	bufferSize  int
	counter     atomic.Int64
	log         *slog.Logger
}

// New creates a new Bus.
func New(bufferSize int, log *slog.Logger) *Bus {
	return &Bus{
		subscribers: make(map[string]chan *agentv1.CoordinationEvent),
		bufferSize:  bufferSize,
		log:         log,
	}
}

// Subscribe registers a subscriber and returns a receive channel + unsubscribe func.
// The caller MUST call unsubscribe when the stream ends to release resources.
func (b *Bus) Subscribe(subscriberID string) (<-chan *agentv1.CoordinationEvent, func()) {
	ch := make(chan *agentv1.CoordinationEvent, b.bufferSize)

	b.mu.Lock()
	b.subscribers[subscriberID] = ch
	b.mu.Unlock()

	b.log.Info("eventbus: subscriber joined", slog.String("id", subscriberID))

	unsub := func() {
		b.mu.Lock()
		delete(b.subscribers, subscriberID)
		close(ch)
		b.mu.Unlock()
		b.log.Info("eventbus: subscriber left", slog.String("id", subscriberID))
	}
	return ch, unsub
}

// Publish delivers an event to all current subscribers.
// Slow subscribers receive a drop warning rather than blocking the publisher.
func (b *Bus) Publish(event *agentv1.CoordinationEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for id, ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			b.log.Warn("eventbus: dropped event for slow subscriber",
				slog.String("subscriber_id", id),
				slog.String("event_type", event.EventType),
			)
		}
	}
}

// NewEvent is a convenience constructor for a CoordinationEvent.
func (b *Bus) NewEvent(agentID, taskID, eventType, payload string, severity commonv1.Severity) *agentv1.CoordinationEvent {
	b.counter.Add(1)
	return &agentv1.CoordinationEvent{
		EventId:   fmt.Sprintf("evt-%06d", b.counter.Load()),
		AgentId:   agentID,
		TaskId:    taskID,
		EventType: eventType,
		Payload:   payload,
		Severity:  severity,
		Timestamp: timestamppb.Now(),
	}
}

// SimulateAgentActivity periodically emits synthetic events so streaming
// subscribers always have data to consume in dev / demo mode.
// Remove or gate behind an env flag in production.
func (b *Bus) SimulateAgentActivity(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		scenarios := []struct {
			agentID   string
			taskID    string
			eventType string
			payload   string
			severity  commonv1.Severity
		}{
			{"mark-vii", "patrol-001", "TASK_STARTED", `{"zone":"perimeter"}`, commonv1.Severity_SEVERITY_INFO},
			{"drone-01", "recon-002", "TASK_COMPLETED", `{"images_captured":42}`, commonv1.Severity_SEVERITY_INFO},
			{"mark-ii", "intercept-003", "TASK_STARTED", `{"target":"unknown-drone"}`, commonv1.Severity_SEVERITY_WARNING},
			{"turret-01", "alert-004", "AGENT_FAILED", `{"reason":"power_fluctuation"}`, commonv1.Severity_SEVERITY_CRITICAL},
			{"drone-02", "recon-005", "TASK_COMPLETED", `{"area_covered":"north-sector"}`, commonv1.Severity_SEVERITY_INFO},
		}

		i := 0
		for range ticker.C {
			s := scenarios[i%len(scenarios)]
			b.Publish(b.NewEvent(s.agentID, s.taskID, s.eventType, s.payload, s.severity))
			i++
		}
	}()
}

// SubscriberCount returns the number of active subscribers (useful for tests).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
