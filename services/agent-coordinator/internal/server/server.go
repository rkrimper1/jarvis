// Package server implements the gRPC AgentCoordinatorService.
package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/rkrimper1/jarvis/gen/agent"
	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/config"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/dispatcher"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/eventbus"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/registry"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/scheduler"
)

// CoordinatorServer implements agentv1.AgentCoordinatorServiceServer.
type CoordinatorServer struct {
	agentv1.UnimplementedAgentCoordinatorServiceServer

	cfg        *config.Config
	registry   *registry.Registry
	scheduler  *scheduler.Scheduler
	dispatcher *dispatcher.Dispatcher
	bus        *eventbus.Bus
	log        *slog.Logger
}

// New wires all dependencies and returns a ready CoordinatorServer.
func New(cfg *config.Config, log *slog.Logger) *CoordinatorServer {
	reg := registry.New(cfg.Registry.HeartbeatTimeout, cfg.Registry.GCSweepInterval)
	sched := scheduler.New(reg, cfg.Scheduler.MaxQueueDepth, cfg.Scheduler.TaskTimeout)
	disp := dispatcher.New(reg, log)
	bus := eventbus.New(cfg.EventBus.SubscriberBuffer, log)

	bus.SimulateAgentActivity(cfg.EventBus.SimulationInterval)

	return &CoordinatorServer{
		cfg:        cfg,
		registry:   reg,
		scheduler:  sched,
		dispatcher: disp,
		bus:        bus,
		log:        log,
	}
}

// ── DispatchTask ──────────────────────────────────────────────────────

func (s *CoordinatorServer) DispatchTask(
	ctx context.Context,
	req *agentv1.DispatchTaskRequest,
) (*agentv1.DispatchTaskResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.TaskDescription == "" {
		return nil, status.Error(codes.InvalidArgument, "task_description is required")
	}

	taskID := req.TaskId
	if taskID == "" {
		taskID = scheduler.NewTaskID("task")
	}

	s.log.InfoContext(ctx, "DispatchTask",
		slog.String("task_id", taskID),
		slog.String("priority", req.Priority.String()),
		slog.Int("target_agents", len(req.TargetAgentIds)),
	)

	assigned, eta, err := s.scheduler.Dispatch(
		taskID,
		req.TaskDescription,
		req.Priority,
		req.Parameters,
		req.TargetAgentIds,
	)
	if err != nil {
		return nil, status.Errorf(codes.ResourceExhausted, "dispatch failed: %v", err)
	}

	// Publish a TASK_STARTED event to all streaming subscribers
	s.bus.Publish(s.bus.NewEvent(
		joined(assigned),
		taskID,
		"TASK_STARTED",
		fmt.Sprintf(`{"description":%q,"priority":%q}`, req.TaskDescription, req.Priority.String()),
		commonv1.Severity_SEVERITY_INFO,
	))

	return &agentv1.DispatchTaskResponse{
		Meta:                metaOK(req.Meta.RequestId),
		TaskId:              taskID,
		AssignedAgents:      assigned,
		EstimatedCompletion: timestamppb.New(eta),
	}, nil
}

// ── GetAgentStatus ────────────────────────────────────────────────────

func (s *CoordinatorServer) GetAgentStatus(
	ctx context.Context,
	req *agentv1.AgentStatusRequest,
) (*agentv1.AgentStatusResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}

	s.log.InfoContext(ctx, "GetAgentStatus",
		slog.Int("filter_count", len(req.AgentIds)),
	)

	agents := s.registry.List(req.AgentIds)

	return &agentv1.AgentStatusResponse{
		Meta:   metaOK(req.Meta.RequestId),
		Agents: agents,
	}, nil
}

// ── Broadcast ─────────────────────────────────────────────────────────

func (s *CoordinatorServer) Broadcast(
	ctx context.Context,
	req *agentv1.BroadcastRequest,
) (*agentv1.BroadcastResponse, error) {

	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	if req.Message == "" {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}

	s.log.InfoContext(ctx, "Broadcast",
		slog.Int("targets", len(req.TargetAgentIds)),
		slog.String("priority", req.Priority.String()),
	)

	result := s.dispatcher.Broadcast(req.TargetAgentIds, req.Message, req.Priority)

	return &agentv1.BroadcastResponse{
		Meta:               metaOK(req.Meta.RequestId),
		AgentsReached:      result.AgentsReached,
		UnresponsiveAgents: result.UnresponsiveAgents,
	}, nil
}

// ── StreamCoordinationEvents ──────────────────────────────────────────

// StreamCoordinationEvents opens a server-streaming RPC that fans out all
// CoordinationEvents from the bus to the calling subscriber.
func (s *CoordinatorServer) StreamCoordinationEvents(
	meta *commonv1.RequestMeta,
	stream agentv1.AgentCoordinatorService_StreamCoordinationEventsServer,
) error {

	subscriberID := meta.GetRequestId()
	s.log.Info("StreamCoordinationEvents: subscriber connected",
		slog.String("subscriber_id", subscriberID),
	)

	ch, unsub := s.bus.Subscribe(subscriberID)
	defer unsub()

	for {
		select {
		case <-stream.Context().Done():
			s.log.Info("StreamCoordinationEvents: client disconnected",
				slog.String("subscriber_id", subscriberID),
			)
			return nil

		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(event); err != nil {
				return status.Errorf(codes.Internal, "stream send: %v", err)
			}
		}
	}
}

// ── AgentHeartbeat ────────────────────────────────────────────────────

// AgentHeartbeat is a bidirectional stream.
//
// Flow:
//   - Agent → JARVIS : CoordinationEvent (heartbeat, task status updates)
//   - JARVIS → Agent : CoordinationEvent (new commands, acknowledgements)
//
// Each incoming event updates the agent registry. Events of type
// TASK_COMPLETED or TASK_FAILED trigger task state transitions in the scheduler.
// JARVIS acknowledges each event and may inject new commands back to the agent.
func (s *CoordinatorServer) AgentHeartbeat(
	stream agentv1.AgentCoordinatorService_AgentHeartbeatServer,
) error {

	ctx := stream.Context()
	s.log.Info("AgentHeartbeat: stream opened")

	for {
		event, err := stream.Recv()
		if err == io.EOF {
			s.log.Info("AgentHeartbeat: agent closed stream")
			return nil
		}
		if err != nil {
			s.log.ErrorContext(ctx, "AgentHeartbeat: recv error", slog.Any("err", err))
			return status.Errorf(codes.Internal, "heartbeat recv: %v", err)
		}

		s.log.InfoContext(ctx, "AgentHeartbeat: received",
			slog.String("agent_id", event.AgentId),
			slog.String("event_type", event.EventType),
			slog.String("task_id", event.TaskId),
		)

		// Update the agent's last-seen timestamp in the registry
		if agent, ok := s.registry.Get(event.AgentId); ok {
			s.registry.Upsert(agent)
		}

		// Handle task lifecycle transitions
		switch event.EventType {
		case "TASK_COMPLETED":
			if event.TaskId != "" {
				_ = s.scheduler.Complete(event.TaskId)
			}
		case "TASK_FAILED", "AGENT_FAILED":
			if event.TaskId != "" {
				_ = s.scheduler.Fail(event.TaskId)
			}
			// Mark agent as ERROR on failure
			_ = s.registry.UpdateStatus(event.AgentId, agentv1.AgentStatus_STATUS_ERROR)
		}

		// Fan-out the event to all StreamCoordinationEvents subscribers
		s.bus.Publish(event)

		// Send acknowledgement back to the agent
		ack := s.bus.NewEvent(
			"jarvis",
			event.TaskId,
			"ACK",
			fmt.Sprintf(`{"acked_event":%q}`, event.EventId),
			commonv1.Severity_SEVERITY_INFO,
		)
		if err := stream.Send(ack); err != nil {
			s.log.ErrorContext(ctx, "AgentHeartbeat: send ack error", slog.Any("err", err))
			return status.Errorf(codes.Internal, "heartbeat send: %v", err)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────

func validateMeta(meta *commonv1.RequestMeta) error {
	if meta == nil {
		return status.Error(codes.InvalidArgument, "meta is required")
	}
	if meta.RequestId == "" {
		return status.Error(codes.InvalidArgument, "meta.request_id is required")
	}
	return nil
}

func metaOK(requestID string) *commonv1.ResponseMeta {
	return &commonv1.ResponseMeta{
		RequestId: requestID,
		Success:   true,
		Timestamp: timestamppb.Now(),
	}
}

// joined returns a comma-joined agent ID string for logging/payloads.
func joined(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	result := ids[0]
	for _, id := range ids[1:] {
		result += "," + id
	}
	return result
}
