package registry_test

import (
	"testing"
	"time"

	agentv1 "github.com/rkrimper1/jarvis/gen/agent"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/registry"
)

func newRegistry() *registry.Registry {
	// Short GC interval for tests; long heartbeat timeout so GC doesn't fire
	return registry.New(30*time.Second, time.Hour)
}

func TestRegistry_UpsertAndGet(t *testing.T) {
	r := newRegistry()

	agent := &agentv1.Agent{
		AgentId:      "test-agent-1",
		Name:         "Test Agent",
		Type:         agentv1.AgentType_AGENT_TYPE_DRONE,
		Status:       agentv1.AgentStatus_AGENT_STATUS_IDLE,
		Capabilities: []string{"recon"},
	}
	r.Upsert(agent)

	got, ok := r.Get("test-agent-1")
	if !ok {
		t.Fatal("expected agent to be found after upsert")
	}
	if got.Name != "Test Agent" {
		t.Errorf("name = %q, want %q", got.Name, "Test Agent")
	}
	if got.LastSeen == nil {
		t.Error("LastSeen should be set on upsert")
	}
}

func TestRegistry_DefaultsStatusToActive(t *testing.T) {
	r := newRegistry()
	r.Upsert(&agentv1.Agent{
		AgentId: "agent-x",
		// Status intentionally left as UNSPECIFIED (zero value)
	})
	a, _ := r.Get("agent-x")
	if a.Status != agentv1.AgentStatus_AGENT_STATUS_ACTIVE {
		t.Errorf("status = %v, want STATUS_ACTIVE", a.Status)
	}
}

func TestRegistry_UpdateStatus(t *testing.T) {
	r := newRegistry()
	r.Upsert(&agentv1.Agent{AgentId: "agent-y", Status: agentv1.AgentStatus_AGENT_STATUS_IDLE})

	if err := r.UpdateStatus("agent-y", agentv1.AgentStatus_AGENT_STATUS_BUSY); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	a, _ := r.Get("agent-y")
	if a.Status != agentv1.AgentStatus_AGENT_STATUS_BUSY {
		t.Errorf("status = %v, want STATUS_BUSY", a.Status)
	}
}

func TestRegistry_UpdateStatus_UnknownAgent(t *testing.T) {
	r := newRegistry()
	err := r.UpdateStatus("ghost-agent", agentv1.AgentStatus_AGENT_STATUS_BUSY)
	if err == nil {
		t.Error("expected error for unknown agent")
	}
}

func TestRegistry_ListAll(t *testing.T) {
	r := newRegistry()
	// Seeded registry already has 7 agents
	agents := r.List(nil)
	if len(agents) == 0 {
		t.Error("expected seeded agents in List(nil)")
	}
}

func TestRegistry_ListFiltered(t *testing.T) {
	r := newRegistry()
	r.Upsert(&agentv1.Agent{AgentId: "a1"})
	r.Upsert(&agentv1.Agent{AgentId: "a2"})

	result := r.List([]string{"a1", "a2"})
	if len(result) != 2 {
		t.Errorf("filtered list len = %d, want 2", len(result))
	}
}

func TestRegistry_Available_ByCapability(t *testing.T) {
	r := newRegistry()
	r.Upsert(&agentv1.Agent{
		AgentId:      "capable-agent",
		Status:       agentv1.AgentStatus_AGENT_STATUS_IDLE,
		Capabilities: []string{"stealth", "combat"},
	})

	available := r.Available("stealth")
	found := false
	for _, a := range available {
		if a.AgentId == "capable-agent" {
			found = true
		}
	}
	if !found {
		t.Error("expected capable-agent in Available('stealth')")
	}
}

func TestRegistry_Available_ExcludesBusy(t *testing.T) {
	r := newRegistry()
	r.Upsert(&agentv1.Agent{
		AgentId:      "busy-agent",
		Status:       agentv1.AgentStatus_AGENT_STATUS_BUSY,
		Capabilities: []string{"combat"},
	})

	available := r.Available("combat")
	for _, a := range available {
		if a.AgentId == "busy-agent" {
			t.Error("busy agent should not appear in Available()")
		}
	}
}

func TestRegistry_StaleAgentMarkedOffline(t *testing.T) {
	// heartbeatTimeout=50ms, gcInterval=20ms — agent becomes stale in 50ms,
	// GC will mark it OFFLINE within the next 20ms tick.
	r := registry.New(50*time.Millisecond, 10*time.Millisecond)

	r.Upsert(&agentv1.Agent{
		AgentId: "stale-agent",
		Status:  agentv1.AgentStatus_AGENT_STATUS_ACTIVE,
	})

	// Wait for the agent to exceed the heartbeat timeout and for GC to run.
	time.Sleep(200 * time.Millisecond)

	got, _ := r.Get("stale-agent")
	if got != nil && got.Status != agentv1.AgentStatus_AGENT_STATUS_OFFLINE {
		t.Errorf("stale agent status = %v, want AGENT_STATUS_OFFLINE", got.Status)
	}
}
