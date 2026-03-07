// Package scheduler manages the task queue and agent assignment logic.
// It is responsible for:
//   - Accepting incoming tasks with a priority and optional target agents
//   - Auto-assigning tasks to suitable idle agents when no target is given
//   - Tracking task lifecycle (PENDING → RUNNING → DONE / TIMEOUT)
//   - Enforcing queue depth limits and task timeouts
package scheduler

import (
	"fmt"
	"sync"
	"time"

	agentv1 "github.com/rkrimper1/jarvis/gen/agent"
	"github.com/rkrimper1/jarvis/services/agent-coordinator/internal/registry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	TaskPending   TaskState = "PENDING"
	TaskRunning   TaskState = "RUNNING"
	TaskCompleted TaskState = "COMPLETED"
	TaskFailed    TaskState = "FAILED"
	TaskTimeout   TaskState = "TIMEOUT"
)

// Task is the internal representation of a dispatched task.
type Task struct {
	ID              string
	Description     string
	Priority        agentv1.TaskPriority
	Parameters      map[string]string
	AssignedAgents  []string
	State           TaskState
	CreatedAt       time.Time
	UpdatedAt       time.Time
	TimeoutDeadline time.Time
}

// Scheduler manages the task queue and assignment.
type Scheduler struct {
	mu            sync.RWMutex
	tasks         map[string]*Task
	registry      *registry.Registry
	maxQueueDepth int
	taskTimeout   time.Duration
}

// New creates a new Scheduler and starts the timeout watcher.
func New(reg *registry.Registry, maxQueue int, taskTimeout time.Duration) *Scheduler {
	s := &Scheduler{
		tasks:         make(map[string]*Task),
		registry:      reg,
		maxQueueDepth: maxQueue,
		taskTimeout:   taskTimeout,
	}
	go s.runTimeoutWatcher()
	return s
}

// Dispatch accepts a task, assigns agents, and enqueues it.
// If targetAgentIDs is empty, the scheduler auto-assigns based on availability.
// Returns the list of assigned agent IDs and estimated completion time.
func (s *Scheduler) Dispatch(
	taskID, description string,
	priority agentv1.TaskPriority,
	params map[string]string,
	targetAgentIDs []string,
) (assignedAgents []string, estimatedCompletion time.Time, err error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	// Back-pressure: reject if queue is full
	pendingCount := s.countByState(TaskPending)
	if pendingCount >= s.maxQueueDepth {
		return nil, time.Time{}, fmt.Errorf(
			"task queue at capacity (%d/%d) — retry later",
			pendingCount, s.maxQueueDepth,
		)
	}

	// Resolve target agents
	if len(targetAgentIDs) > 0 {
		// Validate that all specified agents exist and are reachable
		for _, id := range targetAgentIDs {
			a, ok := s.registry.Get(id)
			if !ok {
				return nil, time.Time{}, fmt.Errorf("agent %q not found", id)
			}
			if a.Status == agentv1.AgentStatus_STATUS_OFFLINE {
				return nil, time.Time{}, fmt.Errorf("agent %q is offline", id)
			}
		}
		assignedAgents = targetAgentIDs
	} else {
		// Auto-assign: pick idle agents, preferring those with matching capabilities
		candidates := s.registry.Available("")
		if len(candidates) == 0 {
			return nil, time.Time{}, fmt.Errorf("no available agents for auto-assignment")
		}

		// For critical tasks, assign up to 3 agents; otherwise just 1
		limit := 1
		if priority == agentv1.TaskPriority_PRIORITY_CRITICAL {
			limit = 3
		}
		for i, c := range candidates {
			if i >= limit {
				break
			}
			assignedAgents = append(assignedAgents, c.AgentId)
		}
	}

	// Mark agents as BUSY
	for _, id := range assignedAgents {
		_ = s.registry.UpdateStatus(id, agentv1.AgentStatus_STATUS_BUSY)
	}

	now := time.Now()
	task := &Task{
		ID:              taskID,
		Description:     description,
		Priority:        priority,
		Parameters:      params,
		AssignedAgents:  assignedAgents,
		State:           TaskRunning,
		CreatedAt:       now,
		UpdatedAt:       now,
		TimeoutDeadline: now.Add(s.taskTimeout),
	}
	s.tasks[taskID] = task

	estimatedCompletion = now.Add(estimateDuration(priority))
	return assignedAgents, estimatedCompletion, nil
}

// Complete marks a task as completed and frees its agents.
func (s *Scheduler) Complete(taskID string) error {
	return s.transition(taskID, TaskCompleted)
}

// Fail marks a task as failed and frees its agents.
func (s *Scheduler) Fail(taskID string) error {
	return s.transition(taskID, TaskFailed)
}

// Get returns a task by ID.
func (s *Scheduler) Get(taskID string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	return t, ok
}

// Stats returns a snapshot of task counts by state.
func (s *Scheduler) Stats() map[TaskState]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	counts := make(map[TaskState]int)
	for _, t := range s.tasks {
		counts[t.State]++
	}
	return counts
}

// ── internal ─────────────────────────────────────────────────────────

func (s *Scheduler) transition(taskID string, next TaskState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}

	task.State = next
	task.UpdatedAt = time.Now()

	// Release agents back to IDLE
	for _, id := range task.AssignedAgents {
		_ = s.registry.UpdateStatus(id, agentv1.AgentStatus_STATUS_IDLE)
	}
	return nil
}

func (s *Scheduler) countByState(state TaskState) int {
	count := 0
	for _, t := range s.tasks {
		if t.State == state {
			count++
		}
	}
	return count
}

// runTimeoutWatcher marks running tasks as TIMEOUT when their deadline passes.
func (s *Scheduler) runTimeoutWatcher() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for _, t := range s.tasks {
			if t.State == TaskRunning && now.After(t.TimeoutDeadline) {
				t.State = TaskTimeout
				t.UpdatedAt = now
				for _, id := range t.AssignedAgents {
					_ = s.registry.UpdateStatus(id, agentv1.AgentStatus_STATUS_IDLE)
				}
			}
		}
		s.mu.Unlock()
	}
}

// estimateDuration returns a rough ETA based on task priority.
func estimateDuration(p agentv1.TaskPriority) time.Duration {
	switch p {
	case agentv1.TaskPriority_PRIORITY_CRITICAL:
		return 30 * time.Second
	case agentv1.TaskPriority_PRIORITY_HIGH:
		return 2 * time.Minute
	case agentv1.TaskPriority_PRIORITY_NORMAL:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// EstimatedCompletion is exported for use in the server layer.
func EstimatedCompletion(priority agentv1.TaskPriority) time.Time {
	return time.Now().Add(estimateDuration(priority))
}

// NewTaskID generates a simple deterministic task ID if the caller didn't supply one.
func NewTaskID(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

// MetaSuccess and MetaError live here to keep server.go import-lean.
func SuccessResponse(requestID string) *timestamppb.Timestamp {
	return timestamppb.Now()
}
