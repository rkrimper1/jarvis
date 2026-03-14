// Package dispatcher handles broadcasting commands to one or many agents.
// It resolves the target set from the registry and tracks delivery status.
package dispatcher

import (
	"log/slog"

	agentv1 "github.com/rkrimper1/jarvis/api/pb/agent"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/registry"
)

// BroadcastResult captures the outcome of a fan-out broadcast.
type BroadcastResult struct {
	AgentsReached      int32
	UnresponsiveAgents []string
}

// Dispatcher fans out messages to target agents.
type Dispatcher struct {
	registry *registry.Registry
	log      *slog.Logger
}

// New creates a new Dispatcher.
func New(reg *registry.Registry, log *slog.Logger) *Dispatcher {
	return &Dispatcher{registry: reg, log: log}
}

// Broadcast delivers a message to the target agents (or all agents if targets is empty).
// An agent is considered unresponsive if it is OFFLINE or in ERROR state.
func (d *Dispatcher) Broadcast(
	targetIDs []string,
	message string,
	priority agentv1.TaskPriority,
) BroadcastResult {

	targets := d.registry.List(targetIDs)

	var reached int32
	var unresponsive []string

	for _, agent := range targets {
		if agent.Status == agentv1.AgentStatus_AGENT_STATUS_OFFLINE ||
			agent.Status == agentv1.AgentStatus_AGENT_STATUS_ERROR {
			unresponsive = append(unresponsive, agent.AgentId)
			d.log.Warn("broadcast: agent unresponsive",
				slog.String("agent_id", agent.AgentId),
				slog.String("status", agent.Status.String()),
			)
			continue
		}

		// In production this would call the agent's own gRPC endpoint.
		// Here we log the delivery to illustrate the dispatch pattern cleanly.
		d.log.Info("broadcast: message delivered",
			slog.String("agent_id", agent.AgentId),
			slog.String("agent_name", agent.Name),
			slog.String("priority", priority.String()),
			slog.String("message", message),
		)
		reached++
	}

	return BroadcastResult{
		AgentsReached:      reached,
		UnresponsiveAgents: unresponsive,
	}
}
