package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	taskv1 "github.com/rkrimper1/jarvis/api/pb/task"
	"github.com/rkrimper1/jarvis/api/internal/task/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := store.New(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func mustCreateTask(t *testing.T, s *store.Store, title, assignee string) *taskv1.Task {
	t.Helper()
	task, err := s.CreateTask(context.Background(), title, "desc", assignee, "reporter-1",
		"medium", "task", "", 3, "2026-12-31", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

func mustCreateSprint(t *testing.T, s *store.Store, name string) *taskv1.Sprint {
	t.Helper()
	sp, err := s.CreateSprint(context.Background(), name, "goal", "2026-04-01", "2026-04-14")
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	return sp
}

// ── Task CRUD ─────────────────────────────────────────────────────────────────

func TestCreateTask_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "Build suit", "Iron Man suit", "tony", "pepper",
		"high", "task", "", 5, "2026-06-01", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.TaskId == "" {
		t.Error("expected non-empty TaskId")
	}
	if task.Title != "Build suit" {
		t.Errorf("Title = %q, want %q", task.Title, "Build suit")
	}
	if task.Priority != taskv1.TaskPriority_TASK_PRIORITY_HIGH {
		t.Errorf("Priority = %v, want HIGH", task.Priority)
	}
	if task.Status != taskv1.TaskStatus_TASK_STATUS_UNASSIGNED {
		t.Errorf("Status = %v, want UNASSIGNED (no sprint)", task.Status)
	}
	if task.DisplayId == 0 {
		t.Error("expected display_id > 0")
	}
	if task.CreatedAt == nil {
		t.Error("expected CreatedAt")
	}
}

func TestCreateTask_WithSprint_StatusAssigned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Sprint 1")

	task, err := s.CreateTask(ctx, "Task in sprint", "", "tony", "tony",
		"medium", "task", "", 0, "", sp.SprintId)
	if err != nil {
		t.Fatalf("CreateTask with sprint: %v", err)
	}
	if task.Status != taskv1.TaskStatus_TASK_STATUS_ASSIGNED {
		t.Errorf("Status = %v, want ASSIGNED when sprint supplied", task.Status)
	}
	if task.SprintId != sp.SprintId {
		t.Errorf("SprintId = %q, want %q", task.SprintId, sp.SprintId)
	}
}

func TestCreateTask_DisplayIDMonotonicallyIncreases(t *testing.T) {
	s := newTestStore(t)
	t1 := mustCreateTask(t, s, "Task A", "alice")
	t2 := mustCreateTask(t, s, "Task B", "bob")
	if t2.DisplayId <= t1.DisplayId {
		t.Errorf("display_id not increasing: %d, %d", t1.DisplayId, t2.DisplayId)
	}
}

func TestCreateTask_Epic_Story_Types(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		taskType string
		want     taskv1.TaskType
	}{
		{"epic", taskv1.TaskType_TASK_TYPE_EPIC},
		{"story", taskv1.TaskType_TASK_TYPE_STORY},
		{"bug", taskv1.TaskType_TASK_TYPE_BUG},
		{"subtask", taskv1.TaskType_TASK_TYPE_SUBTASK},
		{"task", taskv1.TaskType_TASK_TYPE_TASK},
		// "unknown" omitted — SQLite CHECK constraint rejects it at the DB level
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.taskType, func(t *testing.T) {
			task, err := s.CreateTask(ctx, "x", "", "u", "u", "medium", tc.taskType, "", 0, "", "")
			if err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if task.TaskType != tc.want {
				t.Errorf("TaskType = %v, want %v", task.TaskType, tc.want)
			}
		})
	}
}

func TestCreateTask_WithParentId(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	epic := mustCreateTask(t, s, "Epic", "alice")
	child, err := s.CreateTask(ctx, "Child task", "", "bob", "alice",
		"low", "subtask", epic.TaskId, 1, "", "")
	if err != nil {
		t.Fatalf("CreateTask with parent: %v", err)
	}
	if child.ParentId != epic.TaskId {
		t.Errorf("ParentId = %q, want %q", child.ParentId, epic.TaskId)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetTask(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestUpdateTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "Original", "alice")

	updated, err := s.UpdateTask(ctx, task.TaskId, "Updated", "new desc", "bob",
		"", "high", "bug", "", 8, "2027-01-01")
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if updated.Title != "Updated" {
		t.Errorf("Title = %q, want Updated", updated.Title)
	}
	if updated.AssigneeId != "bob" {
		t.Errorf("AssigneeId = %q, want bob", updated.AssigneeId)
	}
	if updated.Priority != taskv1.TaskPriority_TASK_PRIORITY_HIGH {
		t.Errorf("Priority = %v, want HIGH", updated.Priority)
	}
	if updated.TaskType != taskv1.TaskType_TASK_TYPE_BUG {
		t.Errorf("TaskType = %v, want BUG", updated.TaskType)
	}
	if updated.StoryPoints != 8 {
		t.Errorf("StoryPoints = %d, want 8", updated.StoryPoints)
	}
}

func TestDeleteTask_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "To delete", "alice")

	if err := s.DeleteTask(ctx, task.TaskId); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if _, err := s.GetTask(ctx, task.TaskId); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteTask(context.Background(), "no-such-id")
	if err == nil {
		t.Fatal("expected error deleting non-existent task")
	}
}

// ── List operations ───────────────────────────────────────────────────────────

func TestListBacklog_OnlyNoSprintTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Sprint A")

	backlogTask := mustCreateTask(t, s, "Backlog", "alice")
	_, err := s.CreateTask(ctx, "Sprint task", "", "bob", "bob", "medium", "task", "", 0, "", sp.SprintId)
	if err != nil {
		t.Fatalf("create sprint task: %v", err)
	}

	tasks, err := s.ListBacklog(ctx)
	if err != nil {
		t.Fatalf("ListBacklog: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(backlog) = %d, want 1", len(tasks))
	}
	if tasks[0].TaskId != backlogTask.TaskId {
		t.Error("wrong task in backlog")
	}
}

func TestListBacklog_Empty(t *testing.T) {
	s := newTestStore(t)
	tasks, err := s.ListBacklog(context.Background())
	if err != nil {
		t.Fatalf("ListBacklog empty: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestListAll_ReturnsBothBacklogAndSprintTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Sprint B")

	mustCreateTask(t, s, "Backlog task", "alice")
	s.CreateTask(ctx, "Sprint task", "", "bob", "bob", "medium", "task", "", 0, "", sp.SprintId) //nolint

	tasks, err := s.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("len(all) = %d, want 2", len(tasks))
	}
}

func TestListBySprintID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp1 := mustCreateSprint(t, s, "Sprint 1")
	sp2 := mustCreateSprint(t, s, "Sprint 2")

	s.CreateTask(ctx, "In S1", "", "u", "u", "medium", "task", "", 0, "", sp1.SprintId) //nolint
	s.CreateTask(ctx, "In S2", "", "u", "u", "medium", "task", "", 0, "", sp2.SprintId) //nolint

	tasks, err := s.ListBySprintID(ctx, sp1.SprintId)
	if err != nil {
		t.Fatalf("ListBySprintID: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 task in sprint1, got %d", len(tasks))
	}
	if tasks[0].SprintId != sp1.SprintId {
		t.Errorf("SprintId = %q, want %q", tasks[0].SprintId, sp1.SprintId)
	}
}

// ── AssignToSprint ────────────────────────────────────────────────────────────

func TestAssignToSprint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "Unassigned task", "alice")
	sp := mustCreateSprint(t, s, "Sprint X")

	updated, err := s.AssignToSprint(ctx, task.TaskId, sp.SprintId, "system")
	if err != nil {
		t.Fatalf("AssignToSprint: %v", err)
	}
	if updated.SprintId != sp.SprintId {
		t.Errorf("SprintId = %q, want %q", updated.SprintId, sp.SprintId)
	}
	if updated.Status != taskv1.TaskStatus_TASK_STATUS_ASSIGNED {
		t.Errorf("Status = %v, want ASSIGNED", updated.Status)
	}
}

// ── MoveStatus ────────────────────────────────────────────────────────────────

func TestMoveStatus_Progression(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "Progressing", "alice")

	steps := []struct {
		status string
		want   taskv1.TaskStatus
	}{
		{"in_progress", taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS},
		{"testing", taskv1.TaskStatus_TASK_STATUS_TESTING},
		{"review", taskv1.TaskStatus_TASK_STATUS_REVIEW},
		{"completed", taskv1.TaskStatus_TASK_STATUS_COMPLETED},
	}
	for _, step := range steps {
		t.Run(step.status, func(t *testing.T) {
			updated, err := s.MoveStatus(ctx, task.TaskId, step.status, "alice")
			if err != nil {
				t.Fatalf("MoveStatus(%q): %v", step.status, err)
			}
			if updated.Status != step.want {
				t.Errorf("Status = %v, want %v", updated.Status, step.want)
			}
		})
	}
}

func TestMoveStatus_Completed_SetsCompletedFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "Done task", "alice")

	updated, err := s.MoveStatus(ctx, task.TaskId, "completed", "alice")
	if err != nil {
		t.Fatalf("MoveStatus completed: %v", err)
	}
	if updated.CompletedById != "alice" {
		t.Errorf("CompletedById = %q, want alice", updated.CompletedById)
	}
	if updated.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestMoveStatus_NonCompleted_ClearsCompletedFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := mustCreateTask(t, s, "Reopen task", "alice")

	s.MoveStatus(ctx, task.TaskId, "completed", "alice") //nolint
	updated, err := s.MoveStatus(ctx, task.TaskId, "in_progress", "alice")
	if err != nil {
		t.Fatalf("MoveStatus in_progress: %v", err)
	}
	if updated.CompletedById != "" {
		t.Errorf("CompletedById = %q, want empty after reopen", updated.CompletedById)
	}
	if updated.CompletedAt != nil {
		t.Error("expected CompletedAt to be nil after reopen")
	}
}

// ── Sprint CRUD ───────────────────────────────────────────────────────────────

func TestCreateSprint_HappyPath(t *testing.T) {
	s := newTestStore(t)
	sp, err := s.CreateSprint(context.Background(), "Sprint Alpha", "ship the suit", "2026-04-01", "2026-04-14")
	if err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	if sp.SprintId == "" {
		t.Error("expected non-empty SprintId")
	}
	if sp.Name != "Sprint Alpha" {
		t.Errorf("Name = %q, want Sprint Alpha", sp.Name)
	}
	if sp.Status != taskv1.SprintStatus_SPRINT_STATUS_ACTIVE {
		t.Errorf("Status = %v, want ACTIVE", sp.Status)
	}
	if sp.CreatedAt == nil {
		t.Error("expected CreatedAt")
	}
}

func TestGetSprint_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetSprint(context.Background(), "no-such-sprint")
	if err == nil {
		t.Fatal("expected error for missing sprint")
	}
}

func TestUpdateSprint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Old Name")

	updated, err := s.UpdateSprint(ctx, sp.SprintId, "New Name", "new goal", "2026-05-01", "2026-05-15")
	if err != nil {
		t.Fatalf("UpdateSprint: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("Name = %q, want New Name", updated.Name)
	}
	if updated.Goal != "new goal" {
		t.Errorf("Goal = %q, want new goal", updated.Goal)
	}
}

func TestDeleteSprint_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "To delete")

	if err := s.DeleteSprint(ctx, sp.SprintId); err != nil {
		t.Fatalf("DeleteSprint: %v", err)
	}
	if _, err := s.GetSprint(ctx, sp.SprintId); err == nil {
		t.Fatal("expected error after sprint delete")
	}
}

func TestDeleteSprint_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.DeleteSprint(context.Background(), "no-such-sprint")
	if err == nil {
		t.Fatal("expected error deleting non-existent sprint")
	}
}

func TestCloseSprint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Sprint to close")

	closed, err := s.CloseSprint(ctx, sp.SprintId)
	if err != nil {
		t.Fatalf("CloseSprint: %v", err)
	}
	if closed.Status != taskv1.SprintStatus_SPRINT_STATUS_CLOSED {
		t.Errorf("Status = %v, want CLOSED", closed.Status)
	}
}

func TestListSprints_ActiveBeforeClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp1 := mustCreateSprint(t, s, "Sprint 1")
	sp2 := mustCreateSprint(t, s, "Sprint 2")

	s.CloseSprint(ctx, sp1.SprintId) //nolint

	sprints, err := s.ListSprints(ctx)
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if len(sprints) != 2 {
		t.Fatalf("len(sprints) = %d, want 2", len(sprints))
	}
	// Active sprint (sp2) should appear first
	if sprints[0].SprintId != sp2.SprintId {
		t.Errorf("first sprint = %q, want active sprint %q", sprints[0].SprintId, sp2.SprintId)
	}
}

func TestListSprints_Empty(t *testing.T) {
	s := newTestStore(t)
	sprints, err := s.ListSprints(context.Background())
	if err != nil {
		t.Fatalf("ListSprints empty: %v", err)
	}
	if len(sprints) != 0 {
		t.Errorf("expected 0 sprints, got %d", len(sprints))
	}
}

// ── Sprint velocity ───────────────────────────────────────────────────────────

func TestGetSprintVelocity(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Velocity Sprint")

	// Create and complete two tasks with different story points and completers
	t1, _ := s.CreateTask(ctx, "T1", "", "alice", "alice", "medium", "task", "", 5, "", sp.SprintId)
	t2, _ := s.CreateTask(ctx, "T2", "", "alice", "alice", "medium", "task", "", 3, "", sp.SprintId)
	t3, _ := s.CreateTask(ctx, "T3", "", "bob", "alice", "medium", "task", "", 8, "", sp.SprintId)

	s.MoveStatus(ctx, t1.TaskId, "completed", "alice")
	s.MoveStatus(ctx, t2.TaskId, "completed", "alice")
	s.MoveStatus(ctx, t3.TaskId, "completed", "bob")

	vels, err := s.GetSprintVelocity(ctx, sp.SprintId)
	if err != nil {
		t.Fatalf("GetSprintVelocity: %v", err)
	}
	if len(vels) != 2 {
		t.Fatalf("len(velocities) = %d, want 2", len(vels))
	}

	totals := map[string]int32{}
	for _, v := range vels {
		totals[v.UserId] = v.StoryPoints
	}
	if totals["alice"] != 8 {
		t.Errorf("alice velocity = %d, want 8", totals["alice"])
	}
	if totals["bob"] != 8 {
		t.Errorf("bob velocity = %d, want 8", totals["bob"])
	}
}

func TestGetSprintVelocity_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	sp := mustCreateSprint(t, s, "Empty Sprint")

	vels, err := s.GetSprintVelocity(ctx, sp.SprintId)
	if err != nil {
		t.Fatalf("GetSprintVelocity empty: %v", err)
	}
	if len(vels) != 0 {
		t.Errorf("expected 0 velocities, got %d", len(vels))
	}
}

// ── Enum converters (via exported vars) ───────────────────────────────────────

func TestPriorityToString(t *testing.T) {
	tests := []struct {
		p    taskv1.TaskPriority
		want string
	}{
		{taskv1.TaskPriority_TASK_PRIORITY_CRITICAL, "critical"},
		{taskv1.TaskPriority_TASK_PRIORITY_HIGH, "high"},
		{taskv1.TaskPriority_TASK_PRIORITY_MEDIUM, "medium"},
		{taskv1.TaskPriority_TASK_PRIORITY_LOW, "low"},
		{taskv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED, "medium"},
	}
	for _, tc := range tests {
		if got := store.PriorityToString(tc.p); got != tc.want {
			t.Errorf("PriorityToString(%v) = %q, want %q", tc.p, got, tc.want)
		}
	}
}

func TestTaskStatusToString(t *testing.T) {
	tests := []struct {
		s    taskv1.TaskStatus
		want string
	}{
		{taskv1.TaskStatus_TASK_STATUS_UNASSIGNED, "unassigned"},
		{taskv1.TaskStatus_TASK_STATUS_ASSIGNED, "assigned"},
		{taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS, "in_progress"},
		{taskv1.TaskStatus_TASK_STATUS_TESTING, "testing"},
		{taskv1.TaskStatus_TASK_STATUS_REVIEW, "review"},
		{taskv1.TaskStatus_TASK_STATUS_COMPLETED, "completed"},
		{taskv1.TaskStatus_TASK_STATUS_UNSPECIFIED, "unassigned"},
	}
	for _, tc := range tests {
		if got := store.TaskStatusToString(tc.s); got != tc.want {
			t.Errorf("TaskStatusToString(%v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestTaskTypeToString(t *testing.T) {
	tests := []struct {
		tt   taskv1.TaskType
		want string
	}{
		{taskv1.TaskType_TASK_TYPE_EPIC, "epic"},
		{taskv1.TaskType_TASK_TYPE_STORY, "story"},
		{taskv1.TaskType_TASK_TYPE_BUG, "bug"},
		{taskv1.TaskType_TASK_TYPE_SUBTASK, "subtask"},
		{taskv1.TaskType_TASK_TYPE_TASK, "task"},
		{taskv1.TaskType_TASK_TYPE_UNSPECIFIED, "task"},
	}
	for _, tc := range tests {
		if got := store.TaskTypeToString(tc.tt); got != tc.want {
			t.Errorf("TaskTypeToString(%v) = %q, want %q", tc.tt, got, tc.want)
		}
	}
}
