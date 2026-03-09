// Package registry maintains the live state of every agent known to JARVIS.
// Agents register themselves via the heartbeat stream; the registry tracks
// their status, capabilities, and last-seen timestamp, and marks agents
// OFFLINE when they stop sending heartbeats.
package registry

import (
	"fmt"
	"sync"
	"time"

	agentv1 "github.com/rkrimper1/jarvis/gen/agent"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Registry is a thread-safe store of Agent records.
type Registry struct {
	mu               sync.RWMutex
	agents           map[string]*agentv1.Agent
	heartbeatTimeout time.Duration
}

// New creates a new Registry and starts the stale-agent GC sweep.
func New(heartbeatTimeout, gcInterval time.Duration) *Registry {
	r := &Registry{
		agents:           make(map[string]*agentv1.Agent),
		heartbeatTimeout: heartbeatTimeout,
	}
	r.seed()
	go r.runGC(gcInterval)
	return r
}

// Upsert inserts or updates an agent record.
// Called on every heartbeat received from an agent.
func (r *Registry) Upsert(agent *agentv1.Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent.LastSeen = timestamppb.Now()
	if agent.Status == agentv1.AgentStatus_AGENT_STATUS_UNSPECIFIED {
		agent.Status = agentv1.AgentStatus_AGENT_STATUS_ACTIVE
	}
	r.agents[agent.AgentId] = agent
}

// UpdateStatus changes the status of a single agent.
func (r *Registry) UpdateStatus(agentID string, status agentv1.AgentStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	a, ok := r.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %q not found", agentID)
	}
	a.Status = status
	a.LastSeen = timestamppb.Now()
	return nil
}

// Get returns a single agent by ID.
func (r *Registry) Get(agentID string) (*agentv1.Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[agentID]
	return a, ok
}

// List returns agents matching the optional ID filter.
// An empty filter returns all agents.
func (r *Registry) List(agentIDs []string) []*agentv1.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(agentIDs) == 0 {
		all := make([]*agentv1.Agent, 0, len(r.agents))
		for _, a := range r.agents {
			all = append(all, a)
		}
		return all
	}

	result := make([]*agentv1.Agent, 0, len(agentIDs))
	for _, id := range agentIDs {
		if a, ok := r.agents[id]; ok {
			result = append(result, a)
		}
	}
	return result
}

// Available returns all agents that are IDLE and have the given capability.
// Used by the scheduler for auto-assignment.
func (r *Registry) Available(capability string) []*agentv1.Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*agentv1.Agent
	for _, a := range r.agents {
		if a.Status != agentv1.AgentStatus_AGENT_STATUS_IDLE {
			continue
		}
		if capability == "" {
			result = append(result, a)
			continue
		}
		for _, cap := range a.Capabilities {
			if cap == capability {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

// Count returns the total number of registered agents.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// runGC periodically marks agents OFFLINE if they have missed their heartbeat window.
func (r *Registry) runGC(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.Lock()
		cutoff := time.Now().Add(-r.heartbeatTimeout)
		for _, a := range r.agents {
			if a.Status == agentv1.AgentStatus_AGENT_STATUS_OFFLINE {
				continue
			}
			if a.LastSeen.AsTime().Before(cutoff) {
				a.Status = agentv1.AgentStatus_AGENT_STATUS_OFFLINE
			}
		}
		r.mu.Unlock()
	}
}

// seed pre-populates the registry with the Iron Legion for dev / demo use.
// In production, agents self-register via the heartbeat stream.
func (r *Registry) seed() {
	suits := []struct {
		id       string
		name     string
		agType   agentv1.AgentType
		location string
		caps     []string
	}{
		{"mark-ii", "Mark II", agentv1.AgentType_AGENT_TYPE_IRON_SUIT, "hangar-a",
			[]string{"flight", "repulsor", "combat", "recon"}},
		{"mark-iii", "Mark III", agentv1.AgentType_AGENT_TYPE_IRON_SUIT, "hangar-a",
			[]string{"flight", "repulsor", "combat", "weapons"}},
		{"mark-vii", "Mark VII", agentv1.AgentType_AGENT_TYPE_IRON_SUIT, "hangar-b",
			[]string{"flight", "repulsor", "combat", "weapons", "stealth"}},
		{"drone-01", "Recon Drone Alpha", agentv1.AgentType_AGENT_TYPE_DRONE, "perimeter",
			[]string{"recon", "surveillance", "flight"}},
		{"drone-02", "Recon Drone Beta", agentv1.AgentType_AGENT_TYPE_DRONE, "perimeter",
			[]string{"recon", "surveillance", "flight"}},
		{"turret-01", "Perimeter Turret North", agentv1.AgentType_AGENT_TYPE_TURRET, "gate-north",
			[]string{"defense", "target-lock"}},
		{"turret-02", "Perimeter Turret South", agentv1.AgentType_AGENT_TYPE_TURRET, "gate-south",
			[]string{"defense", "target-lock"}},
	}

	for _, s := range suits {
		r.agents[s.id] = &agentv1.Agent{
			AgentId:      s.id,
			Name:         s.name,
			Type:         s.agType,
			Status:       agentv1.AgentStatus_AGENT_STATUS_IDLE,
			Location:     s.location,
			Capabilities: s.caps,
			LastSeen:     timestamppb.Now(),
		}
	}
}
