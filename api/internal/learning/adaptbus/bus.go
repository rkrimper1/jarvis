// Package adaptbus streams real-time model adaptation events to subscribers.
// Each time JARVIS retrains or adjusts a model, an AdaptationEvent is published
// and fanned out to all active StreamAdaptationEvents RPC consumers.
package adaptbus

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	learningv1 "github.com/rkrimper1/jarvis/api/pb/learning"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Bus fans out AdaptationEvents to streaming subscribers.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]chan *learningv1.AdaptationEvent
	bufferSize  int
	counter     atomic.Int64
	log         *slog.Logger
	stopOnce    sync.Once
	done        chan struct{}
}

// New creates a Bus and starts the background adaptation simulator.
func New(bufferSize int, simulationInterval time.Duration, log *slog.Logger) *Bus {
	b := &Bus{
		subscribers: make(map[string]chan *learningv1.AdaptationEvent),
		bufferSize:  bufferSize,
		log:         log,
		done:        make(chan struct{}),
	}
	go b.simulate(simulationInterval)
	return b
}

// Stop shuts down the background simulator goroutine. Safe to call multiple times.
func (b *Bus) Stop() {
	b.stopOnce.Do(func() { close(b.done) })
}

// Subscribe returns a channel of events and an unsubscribe function.
func (b *Bus) Subscribe(id string) (<-chan *learningv1.AdaptationEvent, func()) {
	ch := make(chan *learningv1.AdaptationEvent, b.bufferSize)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		// Only remove the map entry if it still points to our channel.
		// A caller may have re-subscribed with the same ID, in which case we
		// must not evict the newer subscriber.
		if b.subscribers[id] == ch {
			delete(b.subscribers, id)
		}
		close(ch)
		b.mu.Unlock()
	}
}

// Publish fans an event out to all subscribers.
func (b *Bus) Publish(ev *learningv1.AdaptationEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for id, ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
			b.log.Warn("adaptbus: dropped event for slow subscriber", slog.String("id", id))
		}
	}
}

// newEvent is a convenience constructor.
func (b *Bus) newEvent(domain learningv1.ModelDomain, description string, delta float32) *learningv1.AdaptationEvent {
	b.counter.Add(1)
	return &learningv1.AdaptationEvent{
		EventId:       fmt.Sprintf("adapt-%05d", b.counter.Load()),
		Domain:        domain,
		Description:   description,
		DeltaAccuracy: delta,
		Timestamp:     timestamppb.New(time.Now()),
	}
}

// simulate periodically publishes synthetic adaptation events for dev/demo use.
func (b *Bus) simulate(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	scenarios := []struct {
		domain      learningv1.ModelDomain
		description string
		delta       float32
	}{
		{learningv1.ModelDomain_MODEL_DOMAIN_NLP, "Intent classifier updated with 1,200 new voice samples", +0.012},
		{learningv1.ModelDomain_MODEL_DOMAIN_THREAT, "Threat scoring recalibrated after Vanko incident data", +0.031},
		{learningv1.ModelDomain_MODEL_DOMAIN_BEHAVIOR, "Pepper Potts interaction model fine-tuned", +0.008},
		{learningv1.ModelDomain_MODEL_DOMAIN_HARDWARE, "Arc reactor thermal model retrained on new sensor data", +0.005},
		{learningv1.ModelDomain_MODEL_DOMAIN_NLP, "Sarcasm detection accuracy improved", +0.019},
		{learningv1.ModelDomain_MODEL_DOMAIN_THREAT, "False positive rate reduced after SHIELD intel integration", +0.024},
	}

	i := 0
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			s := scenarios[i%len(scenarios)]
			b.Publish(b.newEvent(s.domain, s.description, s.delta))
			i++
		}
	}
}
