// Package tasks manages to-do items and assignments.
package tasks

import (
	"fmt"
	"sync"
	"time"

	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
)

// Task is the internal task record.
type Task struct {
	ID          string
	Title       string
	Description string
	AssigneeID  string
	Due         time.Time
	Priority    int32
	Status      businessv1.TaskStatus
	CreatedAt   time.Time
}

// Store is a thread-safe task registry.
type Store struct {
	mu    sync.RWMutex
	tasks map[string]*Task
	seq   int64
}

// New creates a Store seeded with sample tasks.
func New() *Store {
	s := &Store{tasks: make(map[string]*Task)}
	s.seed()
	return s
}

// Create adds a new task and returns its ID.
func (s *Store) Create(title, description, assigneeID string, due time.Time, priority int32) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	id := fmt.Sprintf("task-%04d", s.seq)
	s.tasks[id] = &Task{
		ID:          id,
		Title:       title,
		Description: description,
		AssigneeID:  assigneeID,
		Due:         due,
		Priority:    priority,
		Status:      businessv1.TaskStatus_TASK_STATUS_PENDING,
		CreatedAt:   time.Now(),
	}
	return id
}

// Get retrieves a task by ID.
func (s *Store) Get(id string) (*Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	return t, ok
}

// UpdateStatus changes task status.
func (s *Store) UpdateStatus(id string, status businessv1.TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	t.Status = status
	return nil
}

func (s *Store) seed() {
	s.seq = 5
	s.tasks["task-0001"] = &Task{
		ID: "task-0001", Title: "Review Palladium Toxicity Research",
		Description: "Cross-reference latest research papers with JARVIS biochem database.",
		AssigneeID:  "tony-stark", Priority: 5, Due: time.Now().Add(24 * time.Hour),
		Status: businessv1.TaskStatus_TASK_STATUS_IN_PROGRESS, CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	s.tasks["task-0002"] = &Task{
		ID: "task-0002", Title: "Approve Stark Expo Security Plan",
		AssigneeID: "pepper-potts", Priority: 4, Due: time.Now().Add(48 * time.Hour),
		Status: businessv1.TaskStatus_TASK_STATUS_PENDING, CreatedAt: time.Now().Add(-1 * time.Hour),
	}
}
