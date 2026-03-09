package tasks_test

import (
	"testing"
	"time"

	businessv1 "github.com/rkrimper1/jarvis/gen/business"
	"github.com/rkrimper1/jarvis/services/business-ops-service/internal/tasks"
)

func TestCreate_And_Get(t *testing.T) {
	s := tasks.New()
	id := s.Create("Test Task", "A test", "tony-stark", time.Now().Add(24*time.Hour), 3)
	if id == "" {
		t.Error("expected non-empty task ID")
	}
	task, ok := s.Get(id)
	if !ok {
		t.Fatal("expected task to be retrievable")
	}
	if task.Title != "Test Task" {
		t.Errorf("title = %q, want Test Task", task.Title)
	}
	if task.Status != businessv1.TaskStatus_TASK_STATUS_PENDING {
		t.Errorf("status = %v, want TASK_PENDING", task.Status)
	}
}

func TestCreate_SeededTasks(t *testing.T) {
	s := tasks.New()
	task, ok := s.Get("task-0001")
	if !ok {
		t.Fatal("expected seeded task-0001")
	}
	if task.AssigneeID != "tony-stark" {
		t.Errorf("assignee = %q, want tony-stark", task.AssigneeID)
	}
}

func TestUpdateStatus(t *testing.T) {
	s := tasks.New()
	id := s.Create("Status Test", "", "pepper-potts", time.Now().Add(time.Hour), 2)
	if err := s.UpdateStatus(id, businessv1.TaskStatus_TASK_STATUS_DONE); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	task, _ := s.Get(id)
	if task.Status != businessv1.TaskStatus_TASK_STATUS_DONE {
		t.Errorf("status = %v, want TASK_DONE", task.Status)
	}
}

func TestUpdateStatus_UnknownTask(t *testing.T) {
	s := tasks.New()
	if err := s.UpdateStatus("ghost-task", businessv1.TaskStatus_TASK_STATUS_DONE); err == nil {
		t.Error("expected error for unknown task")
	}
}

func TestGet_UnknownTask(t *testing.T) {
	s := tasks.New()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for unknown task")
	}
}
