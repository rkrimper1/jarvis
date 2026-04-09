package server_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	taskv1   "github.com/rkrimper1/jarvis/api/pb/task"
	"github.com/rkrimper1/jarvis/api/internal/task/server"
	"github.com/rkrimper1/jarvis/api/internal/task/store"
	"github.com/rkrimper1/jarvis/api/internal/security/token"
)

const testSecret = "test-task-secret"

// ── test helpers ──────────────────────────────────────────────────────────────

func newTestServer(t *testing.T) *server.TaskServer {
	t.Helper()
	f, err := os.CreateTemp("", "tasks-server-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	s, err := store.New(f.Name(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return server.New(s, testSecret, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func newNilStoreServer(t *testing.T) *server.TaskServer {
	t.Helper()
	return server.New(nil, testSecret, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// ctxWithToken injects a Bearer token for the given user+role into gRPC metadata.
func ctxWithToken(t *testing.T, userID, role string) context.Context {
	t.Helper()
	mgr := token.New(testSecret, time.Hour, "jarvis.security")
	tok, _, err := mgr.Issue(userID, []string{role}, role)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	md := metadata.Pairs("authorization", "Bearer "+tok)
	return metadata.NewIncomingContext(context.Background(), md)
}

func meta(id string) *commonv1.RequestMeta {
	return &commonv1.RequestMeta{RequestId: id}
}

func mustCreateTask(t *testing.T, srv *server.TaskServer, ctx context.Context, title, assignee string) *taskv1.Task {
	t.Helper()
	resp, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta:       meta("ct-" + title),
		Title:      title,
		AssigneeId: assignee,
		ReporterId: "reporter",
		Priority:   taskv1.TaskPriority_TASK_PRIORITY_MEDIUM,
		TaskType:   taskv1.TaskType_TASK_TYPE_TASK,
	})
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", title, err)
	}
	return resp.Task
}

func mustCreateSprint(t *testing.T, srv *server.TaskServer, ctx context.Context, name string) *taskv1.Sprint {
	t.Helper()
	resp, err := srv.CreateSprint(ctx, &taskv1.CreateSprintRequest{
		Meta:      meta("cs-" + name),
		Name:      name,
		Goal:      "test goal",
		StartDate: "2026-04-01",
		EndDate:   "2026-04-14",
	})
	if err != nil {
		t.Fatalf("CreateSprint(%q): %v", name, err)
	}
	return resp.Sprint
}

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a gRPC status: %v", err)
	}
	if st.Code() != want {
		t.Errorf("code = %v, want %v: %v", st.Code(), want, err)
	}
}

// ── store-not-configured ──────────────────────────────────────────────────────

func TestCreateTask_NoStore_FailedPrecondition(t *testing.T) {
	srv := newNilStoreServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: meta("r1"), Title: "x", AssigneeId: "a", ReporterId: "r",
	})
	assertCode(t, err, codes.FailedPrecondition)
}

// ── meta validation ───────────────────────────────────────────────────────────

func TestCreateTask_NilMeta_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateTask_EmptyRequestID_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: &commonv1.RequestMeta{RequestId: ""},
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── authentication ────────────────────────────────────────────────────────────

func TestCreateTask_NoToken_Unauthenticated(t *testing.T) {
	srv := newTestServer(t)
	// context without metadata
	_, err := srv.CreateTask(context.Background(), &taskv1.CreateTaskRequest{
		Meta: meta("r1"), Title: "x", AssigneeId: "a", ReporterId: "r",
	})
	assertCode(t, err, codes.Unauthenticated)
}

func TestCreateTask_InvalidToken_Unauthenticated(t *testing.T) {
	srv := newTestServer(t)
	md := metadata.Pairs("authorization", "Bearer bad-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: meta("r1"), Title: "x", AssigneeId: "a", ReporterId: "r",
	})
	assertCode(t, err, codes.Unauthenticated)
}

// ── CreateTask ────────────────────────────────────────────────────────────────

func TestCreateTask_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	resp, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta:        meta("create-1"),
		Title:       "Build suit",
		Description: "Iron Man suit v3",
		AssigneeId:  "tony",
		ReporterId:  "pepper",
		Priority:    taskv1.TaskPriority_TASK_PRIORITY_HIGH,
		TaskType:    taskv1.TaskType_TASK_TYPE_TASK,
		StoryPoints: 5,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if resp.Task.TaskId == "" {
		t.Error("expected non-empty TaskId")
	}
	if resp.Task.Title != "Build suit" {
		t.Errorf("Title = %q, want Build suit", resp.Task.Title)
	}
	if resp.Meta.RequestId != "create-1" {
		t.Errorf("ResponseMeta.RequestId = %q, want create-1", resp.Meta.RequestId)
	}
	if !resp.Meta.Success {
		t.Error("expected Success=true")
	}
}

func TestCreateTask_MissingTitle_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: meta("r1"), AssigneeId: "tony", ReporterId: "pepper",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateTask_MissingAssignee_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: meta("r1"), Title: "T", ReporterId: "pepper",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateTask_MissingReporter_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.CreateTask(ctx, &taskv1.CreateTaskRequest{
		Meta: meta("r1"), Title: "T", AssigneeId: "tony",
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── GetTask ───────────────────────────────────────────────────────────────────

func TestGetTask_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	task := mustCreateTask(t, srv, ctx, "My Task", "tony")

	resp, err := srv.GetTask(ctx, &taskv1.GetTaskRequest{
		Meta: meta("get-1"), TaskId: task.TaskId,
	})
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if resp.Task.TaskId != task.TaskId {
		t.Errorf("TaskId = %q, want %q", resp.Task.TaskId, task.TaskId)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.GetTask(ctx, &taskv1.GetTaskRequest{
		Meta: meta("get-1"), TaskId: "no-such-id",
	})
	assertCode(t, err, codes.NotFound)
}

func TestGetTask_MissingTaskID_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.GetTask(ctx, &taskv1.GetTaskRequest{Meta: meta("r1")})
	assertCode(t, err, codes.InvalidArgument)
}

// ── UpdateTask ────────────────────────────────────────────────────────────────

func TestUpdateTask_AssigneeCanUpdate(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	task := mustCreateTask(t, srv, ctx, "Original", "tony")

	resp, err := srv.UpdateTask(ctx, &taskv1.UpdateTaskRequest{
		Meta: meta("upd-1"), TaskId: task.TaskId,
		Title: "Updated", AssigneeId: "tony", Priority: taskv1.TaskPriority_TASK_PRIORITY_LOW,
		TaskType: taskv1.TaskType_TASK_TYPE_BUG, StoryPoints: 2,
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if resp.Task.Title != "Updated" {
		t.Errorf("Title = %q, want Updated", resp.Task.Title)
	}
}

func TestUpdateTask_AdminCanUpdateAnyTask(t *testing.T) {
	srv := newTestServer(t)
	ownerCtx := ctxWithToken(t, "alice", "ROLE_VIEWER")
	adminCtx := ctxWithToken(t, "admin", "ROLE_ADMIN")

	task := mustCreateTask(t, srv, ownerCtx, "Alice task", "alice")

	_, err := srv.UpdateTask(adminCtx, &taskv1.UpdateTaskRequest{
		Meta: meta("upd-2"), TaskId: task.TaskId,
		Title: "Admin updated", AssigneeId: "alice",
	})
	if err != nil {
		t.Fatalf("admin UpdateTask: %v", err)
	}
}

func TestUpdateTask_NonAssigneeNonAdmin_PermissionDenied(t *testing.T) {
	srv := newTestServer(t)
	ownerCtx := ctxWithToken(t, "alice", "ROLE_VIEWER")
	bobCtx := ctxWithToken(t, "bob", "ROLE_VIEWER")

	task := mustCreateTask(t, srv, ownerCtx, "Alice task", "alice")

	_, err := srv.UpdateTask(bobCtx, &taskv1.UpdateTaskRequest{
		Meta: meta("upd-3"), TaskId: task.TaskId, Title: "Bob steals it",
	})
	assertCode(t, err, codes.PermissionDenied)
}

// ── DeleteTask ────────────────────────────────────────────────────────────────

func TestDeleteTask_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	task := mustCreateTask(t, srv, ctx, "Delete me", "tony")

	resp, err := srv.DeleteTask(ctx, &taskv1.DeleteTaskRequest{
		Meta: meta("del-1"), TaskId: task.TaskId,
	})
	if err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if resp.TaskId != task.TaskId {
		t.Errorf("TaskId = %q, want %q", resp.TaskId, task.TaskId)
	}
}

func TestDeleteTask_NonOwnerNonAdmin_PermissionDenied(t *testing.T) {
	srv := newTestServer(t)
	ownerCtx := ctxWithToken(t, "alice", "ROLE_VIEWER")
	bobCtx := ctxWithToken(t, "bob", "ROLE_VIEWER")

	task := mustCreateTask(t, srv, ownerCtx, "Alice task", "alice")
	_, err := srv.DeleteTask(bobCtx, &taskv1.DeleteTaskRequest{
		Meta: meta("del-2"), TaskId: task.TaskId,
	})
	assertCode(t, err, codes.PermissionDenied)
}

// ── ListBacklog ───────────────────────────────────────────────────────────────

func TestListBacklog_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	mustCreateTask(t, srv, ctx, "Backlog task", "tony")

	resp, err := srv.ListBacklog(ctx, &taskv1.ListBacklogRequest{Meta: meta("lb-1")})
	if err != nil {
		t.Fatalf("ListBacklog: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Errorf("len(Tasks) = %d, want 1", len(resp.Tasks))
	}
}

// ── ListAllTasks ──────────────────────────────────────────────────────────────

func TestListAllTasks_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	mustCreateTask(t, srv, ctx, "T1", "tony")
	mustCreateTask(t, srv, ctx, "T2", "tony")

	resp, err := srv.ListAllTasks(ctx, &taskv1.ListAllTasksRequest{Meta: meta("la-1")})
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Errorf("len(Tasks) = %d, want 2", len(resp.Tasks))
	}
}

// ── MoveTaskStatus ────────────────────────────────────────────────────────────

func TestMoveTaskStatus_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	task := mustCreateTask(t, srv, ctx, "In progress", "tony")

	resp, err := srv.MoveTaskStatus(ctx, &taskv1.MoveTaskStatusRequest{
		Meta:      meta("mv-1"),
		TaskId:    task.TaskId,
		NewStatus: taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS,
		UserId:    "tony",
	})
	if err != nil {
		t.Fatalf("MoveTaskStatus: %v", err)
	}
	if resp.Task.Status != taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS {
		t.Errorf("Status = %v, want IN_PROGRESS", resp.Task.Status)
	}
}

func TestMoveTaskStatus_Unassigned_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	task := mustCreateTask(t, srv, ctx, "Task", "tony")

	_, err := srv.MoveTaskStatus(ctx, &taskv1.MoveTaskStatusRequest{
		Meta:      meta("mv-2"),
		TaskId:    task.TaskId,
		NewStatus: taskv1.TaskStatus_TASK_STATUS_UNASSIGNED,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestMoveTaskStatus_MissingTaskID_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.MoveTaskStatus(ctx, &taskv1.MoveTaskStatusRequest{
		Meta:      meta("mv-3"),
		NewStatus: taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS,
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── AssignTaskToSprint ────────────────────────────────────────────────────────

func TestAssignTaskToSprint_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	task := mustCreateTask(t, srv, viewerCtx, "Sprint task", "tony")
	sprint := mustCreateSprint(t, srv, adminCtx, "Sprint 1")

	resp, err := srv.AssignTaskToSprint(viewerCtx, &taskv1.AssignTaskToSprintRequest{
		Meta:     meta("assign-1"),
		TaskId:   task.TaskId,
		SprintId: sprint.SprintId,
	})
	if err != nil {
		t.Fatalf("AssignTaskToSprint: %v", err)
	}
	if resp.Task.SprintId != sprint.SprintId {
		t.Errorf("SprintId = %q, want %q", resp.Task.SprintId, sprint.SprintId)
	}
}

func TestAssignTaskToSprint_MissingIDs_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.AssignTaskToSprint(ctx, &taskv1.AssignTaskToSprintRequest{
		Meta: meta("assign-2"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── Sprint CRUD ───────────────────────────────────────────────────────────────

func TestCreateSprint_AdminOnly(t *testing.T) {
	srv := newTestServer(t)
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")

	_, err := srv.CreateSprint(viewerCtx, &taskv1.CreateSprintRequest{
		Meta: meta("sp-1"), Name: "Sprint",
	})
	assertCode(t, err, codes.PermissionDenied)

	resp, err := srv.CreateSprint(adminCtx, &taskv1.CreateSprintRequest{
		Meta: meta("sp-2"), Name: "Sprint", StartDate: "2026-04-01", EndDate: "2026-04-14",
	})
	if err != nil {
		t.Fatalf("admin CreateSprint: %v", err)
	}
	if resp.Sprint.SprintId == "" {
		t.Error("expected non-empty SprintId")
	}
}

func TestCreateSprint_EditorAllowed(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_EDITOR")
	_, err := srv.CreateSprint(ctx, &taskv1.CreateSprintRequest{
		Meta: meta("sp-3"), Name: "Editor Sprint",
	})
	if err != nil {
		t.Fatalf("editor CreateSprint: %v", err)
	}
}

func TestCreateSprint_EmptyName_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	_, err := srv.CreateSprint(ctx, &taskv1.CreateSprintRequest{Meta: meta("sp-4")})
	assertCode(t, err, codes.InvalidArgument)
}

func TestListSprints_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	mustCreateSprint(t, srv, adminCtx, "Sprint A")
	mustCreateSprint(t, srv, adminCtx, "Sprint B")

	resp, err := srv.ListSprints(viewerCtx, &taskv1.ListSprintsRequest{Meta: meta("ls-1")})
	if err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if len(resp.Sprints) != 2 {
		t.Errorf("len(Sprints) = %d, want 2", len(resp.Sprints))
	}
}

func TestListSprintTasks_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	sprint := mustCreateSprint(t, srv, adminCtx, "Sprint C")
	task := mustCreateTask(t, srv, viewerCtx, "Sprint task", "tony")
	srv.AssignTaskToSprint(viewerCtx, &taskv1.AssignTaskToSprintRequest{ //nolint
		Meta: meta("x"), TaskId: task.TaskId, SprintId: sprint.SprintId,
	})

	resp, err := srv.ListSprintTasks(viewerCtx, &taskv1.ListSprintTasksRequest{
		Meta:     meta("lst-1"),
		SprintId: sprint.SprintId,
	})
	if err != nil {
		t.Fatalf("ListSprintTasks: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Errorf("len(Tasks) = %d, want 1", len(resp.Tasks))
	}
}

func TestListSprintTasks_MissingSprintID_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.ListSprintTasks(ctx, &taskv1.ListSprintTasksRequest{Meta: meta("r1")})
	assertCode(t, err, codes.InvalidArgument)
}

func TestUpdateSprint_NonAdmin_PermissionDenied(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	sprint := mustCreateSprint(t, srv, adminCtx, "Sprint D")
	_, err := srv.UpdateSprint(viewerCtx, &taskv1.UpdateSprintRequest{
		Meta:     meta("upd-sp-1"),
		SprintId: sprint.SprintId,
		Name:     "New Name",
	})
	assertCode(t, err, codes.PermissionDenied)
}

func TestDeleteSprint_AdminOnly(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	sprint := mustCreateSprint(t, srv, adminCtx, "Sprint E")
	_, err := srv.DeleteSprint(viewerCtx, &taskv1.DeleteSprintRequest{
		Meta:     meta("del-sp-1"),
		SprintId: sprint.SprintId,
	})
	assertCode(t, err, codes.PermissionDenied)

	_, err = srv.DeleteSprint(adminCtx, &taskv1.DeleteSprintRequest{
		Meta:     meta("del-sp-2"),
		SprintId: sprint.SprintId,
	})
	if err != nil {
		t.Fatalf("admin DeleteSprint: %v", err)
	}
}

func TestCloseSprint_AdminOnly(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	sprint := mustCreateSprint(t, srv, adminCtx, "Sprint F")
	_, err := srv.CloseSprint(viewerCtx, &taskv1.CloseSprintRequest{
		Meta:     meta("close-sp-1"),
		SprintId: sprint.SprintId,
	})
	assertCode(t, err, codes.PermissionDenied)

	resp, err := srv.CloseSprint(adminCtx, &taskv1.CloseSprintRequest{
		Meta:     meta("close-sp-2"),
		SprintId: sprint.SprintId,
	})
	if err != nil {
		t.Fatalf("admin CloseSprint: %v", err)
	}
	if resp.Sprint.Status != taskv1.SprintStatus_SPRINT_STATUS_CLOSED {
		t.Errorf("Status = %v, want CLOSED", resp.Sprint.Status)
	}
}

func TestGetSprintVelocity_HappyPath(t *testing.T) {
	srv := newTestServer(t)
	adminCtx := ctxWithToken(t, "tony", "ROLE_ADMIN")
	viewerCtx := ctxWithToken(t, "tony", "ROLE_VIEWER")

	sprint := mustCreateSprint(t, srv, adminCtx, "Velocity Sprint")
	task := mustCreateTask(t, srv, viewerCtx, "Velocity task", "tony")
	srv.AssignTaskToSprint(viewerCtx, &taskv1.AssignTaskToSprintRequest{ //nolint
		Meta: meta("x"), TaskId: task.TaskId, SprintId: sprint.SprintId,
	})
	srv.MoveTaskStatus(viewerCtx, &taskv1.MoveTaskStatusRequest{ //nolint
		Meta:      meta("y"),
		TaskId:    task.TaskId,
		NewStatus: taskv1.TaskStatus_TASK_STATUS_COMPLETED,
		UserId:    "tony",
	})

	resp, err := srv.GetSprintVelocity(viewerCtx, &taskv1.GetSprintVelocityRequest{
		Meta:     meta("vel-1"),
		SprintId: sprint.SprintId,
	})
	if err != nil {
		t.Fatalf("GetSprintVelocity: %v", err)
	}
	// Velocity data present (story points default to 0 unless set)
	_ = resp.Velocities
}

func TestGetSprintVelocity_MissingSprintID_InvalidArgument(t *testing.T) {
	srv := newTestServer(t)
	ctx := ctxWithToken(t, "tony", "ROLE_VIEWER")
	_, err := srv.GetSprintVelocity(ctx, &taskv1.GetSprintVelocityRequest{Meta: meta("r1")})
	assertCode(t, err, codes.InvalidArgument)
}
