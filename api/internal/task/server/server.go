package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	taskv1   "github.com/rkrimper1/jarvis/api/pb/task"
	userv1   "github.com/rkrimper1/jarvis/api/pb/user"
	"github.com/rkrimper1/jarvis/api/internal/security/token"
	"github.com/rkrimper1/jarvis/api/internal/task/notify"
	"github.com/rkrimper1/jarvis/api/internal/task/store"
	"github.com/rkrimper1/jarvis/api/middleware"
)

// UserLookup retrieves a user record for assignment notification emails.
// *userstore.Store satisfies this interface.
type UserLookup interface {
	GetByID(ctx context.Context, id string) (*userv1.User, error)
}

// TaskServer implements taskv1.TaskServiceServer.
type TaskServer struct {
	taskv1.UnimplementedTaskServiceServer
	store    *store.Store
	mgr      *token.Manager
	log      *slog.Logger
	notifier *notify.Notifier // nil → notifications disabled
	users    UserLookup       // nil → email lookup unavailable
}

// New returns a TaskServer backed by the given store.
func New(s *store.Store, tokenSecret string, log *slog.Logger) *TaskServer {
	mgr := token.New(tokenSecret, time.Hour, "jarvis.security")
	return &TaskServer{store: s, mgr: mgr, log: log}
}

// NewWithNotify is like New but wires in assignment email notifications.
// Pass nil for notifier or users to disable notifications.
func NewWithNotify(s *store.Store, tokenSecret string, log *slog.Logger, n *notify.Notifier, users UserLookup) *TaskServer {
	srv := New(s, tokenSecret, log)
	srv.notifier = n
	srv.users = users
	return srv
}

// notifyAssignment looks up the assignee's email and fires a background notification.
// It is a no-op when the notifier or user store is not configured.
func (s *TaskServer) notifyAssignment(ctx context.Context, task *taskv1.Task, reporterID string) {
	if s.notifier == nil || s.users == nil || task.AssigneeId == "" {
		return
	}
	assignee, err := s.users.GetByID(ctx, task.AssigneeId)
	if err != nil {
		s.log.WarnContext(ctx, "task notify: assignee lookup failed",
			slog.String("task_id", task.TaskId),
			slog.String("assignee_id", task.AssigneeId),
			slog.Any("err", err),
		)
		return
	}
	var reporterEmail string
	if reporter, err := s.users.GetByID(ctx, reporterID); err == nil {
		reporterEmail = reporter.Email
	}
	s.notifier.SendAssignment(ctx, task, assignee.Email, reporterEmail)
}

func (s *TaskServer) storeRequired() error {
	if s.store == nil {
		return status.Error(codes.FailedPrecondition, "task store not configured (set TASKS_DB_PATH)")
	}
	return nil
}

// callerRole extracts the role and userID from the Bearer token in gRPC metadata.
func callerRole(ctx context.Context, mgr *token.Manager) (role, userID string, err error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "missing authorization header")
	}
	raw := vals[0]
	raw = strings.TrimPrefix(raw, "Bearer ")
	raw = strings.TrimPrefix(raw, "bearer ")
	claims, err2 := mgr.Validate(raw)
	if err2 != nil {
		return "", "", status.Errorf(codes.Unauthenticated, "invalid token: %v", err2)
	}
	return claims.Role, claims.Subject, nil
}

func isAdminOrEditor(role string) bool {
	return role == "ROLE_ADMIN" || role == "ROLE_EDITOR"
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (s *TaskServer) CreateTask(ctx context.Context, req *taskv1.CreateTaskRequest) (*taskv1.CreateTaskResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/CreateTask")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	if req.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if req.AssigneeId == "" {
		return nil, status.Error(codes.InvalidArgument, "assignee_id is required")
	}
	if req.ReporterId == "" {
		return nil, status.Error(codes.InvalidArgument, "reporter_id is required")
	}

	priority := store.PriorityToString(req.Priority)
	taskType := store.TaskTypeToString(req.TaskType)
	t, err := s.store.CreateTask(ctx, req.Title, req.Description, req.AssigneeId, req.ReporterId,
		priority, taskType, req.ParentId, req.StoryPoints, req.DueDate, req.SprintId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create task: %v", err)
	}
	s.log.InfoContext(ctx, "CreateTask", slog.String("task_id", t.TaskId))
	s.notifyAssignment(ctx, t, req.ReporterId)
	return &taskv1.CreateTaskResponse{Meta: metaOK(req.Meta.RequestId), Task: t}, nil
}

func (s *TaskServer) GetTask(ctx context.Context, req *taskv1.GetTaskRequest) (*taskv1.GetTaskResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetTask")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	t, err := s.store.GetTask(ctx, req.TaskId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &taskv1.GetTaskResponse{Meta: metaOK(req.Meta.RequestId), Task: t}, nil
}

func (s *TaskServer) UpdateTask(ctx context.Context, req *taskv1.UpdateTaskRequest) (*taskv1.UpdateTaskResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/UpdateTask")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, callerID, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	existing, err := s.store.GetTask(ctx, req.TaskId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}

	if !isAdminOrEditor(role) && existing.AssigneeId != callerID {
		return nil, status.Error(codes.PermissionDenied, "only admin, editor, or the task assignee may update this task")
	}

	priority := store.PriorityToString(req.Priority)
	taskType := store.TaskTypeToString(req.TaskType)
	t, err := s.store.UpdateTask(ctx, req.TaskId, req.Title, req.Description, req.AssigneeId, req.ReporterId, priority, taskType, req.ParentId, req.StoryPoints, req.DueDate)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update task: %v", err)
	}
	s.log.InfoContext(ctx, "UpdateTask", slog.String("task_id", req.TaskId))
	if req.AssigneeId != "" && req.AssigneeId != existing.AssigneeId {
		s.notifyAssignment(ctx, t, existing.ReporterId)
	}
	return &taskv1.UpdateTaskResponse{Meta: metaOK(req.Meta.RequestId), Task: t}, nil
}

func (s *TaskServer) DeleteTask(ctx context.Context, req *taskv1.DeleteTaskRequest) (*taskv1.DeleteTaskResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/DeleteTask")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, callerID, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}

	existing, err := s.store.GetTask(ctx, req.TaskId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}

	if !isAdminOrEditor(role) && existing.AssigneeId != callerID {
		return nil, status.Error(codes.PermissionDenied, "only admin, editor, or the task assignee may delete this task")
	}

	if err := s.store.DeleteTask(ctx, req.TaskId); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "%v", err)
		}
		return nil, status.Errorf(codes.Internal, "delete task: %v", err)
	}
	s.log.InfoContext(ctx, "DeleteTask", slog.String("task_id", req.TaskId))
	return &taskv1.DeleteTaskResponse{Meta: metaOK(req.Meta.RequestId), TaskId: req.TaskId}, nil
}

func (s *TaskServer) ListBacklog(ctx context.Context, req *taskv1.ListBacklogRequest) (*taskv1.ListBacklogResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/ListBacklog")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	tasks, err := s.store.ListBacklog(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list backlog: %v", err)
	}
	return &taskv1.ListBacklogResponse{Meta: metaOK(req.Meta.RequestId), Tasks: tasks}, nil
}

func (s *TaskServer) ListAllTasks(ctx context.Context, req *taskv1.ListAllTasksRequest) (*taskv1.ListAllTasksResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/ListAllTasks")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	tasks, err := s.store.ListAll(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list all tasks: %v", err)
	}
	return &taskv1.ListAllTasksResponse{Meta: metaOK(req.Meta.RequestId), Tasks: tasks}, nil
}

func (s *TaskServer) ListSprintTasks(ctx context.Context, req *taskv1.ListSprintTasksRequest) (*taskv1.ListSprintTasksResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/ListSprintTasks")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	if req.SprintId == "" {
		return nil, status.Error(codes.InvalidArgument, "sprint_id is required")
	}
	tasks, err := s.store.ListBySprintID(ctx, req.SprintId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list sprint tasks: %v", err)
	}
	return &taskv1.ListSprintTasksResponse{Meta: metaOK(req.Meta.RequestId), Tasks: tasks}, nil
}

func (s *TaskServer) AssignTaskToSprint(ctx context.Context, req *taskv1.AssignTaskToSprintRequest) (*taskv1.AssignTaskToSprintResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/AssignTaskToSprint")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	_, callerID, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	var t *taskv1.Task
	if req.SprintId == "" {
		t, err = s.store.RemoveFromSprint(ctx, req.TaskId, callerID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "remove from sprint: %v", err)
		}
		s.log.InfoContext(ctx, "RemoveFromSprint", slog.String("task_id", req.TaskId))
	} else {
		t, err = s.store.AssignToSprint(ctx, req.TaskId, req.SprintId, callerID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "assign to sprint: %v", err)
		}
		s.log.InfoContext(ctx, "AssignTaskToSprint", slog.String("task_id", req.TaskId), slog.String("sprint_id", req.SprintId))
	}
	return &taskv1.AssignTaskToSprintResponse{Meta: metaOK(req.Meta.RequestId), Task: t}, nil
}

func (s *TaskServer) MoveTaskStatus(ctx context.Context, req *taskv1.MoveTaskStatusRequest) (*taskv1.MoveTaskStatusResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/MoveTaskStatus")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	_, callerID, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}

	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	if req.NewStatus == taskv1.TaskStatus_TASK_STATUS_UNSPECIFIED || req.NewStatus == taskv1.TaskStatus_TASK_STATUS_UNASSIGNED {
		return nil, status.Error(codes.InvalidArgument, "cannot move to unassigned status via MoveTaskStatus")
	}

	newStatusStr := store.TaskStatusToString(req.NewStatus)
	completedBy := req.UserId
	if completedBy == "" {
		completedBy = callerID
	}

	t, err := s.store.MoveStatus(ctx, req.TaskId, newStatusStr, completedBy)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "move status: %v", err)
	}
	s.log.InfoContext(ctx, "MoveTaskStatus", slog.String("task_id", req.TaskId), slog.String("status", newStatusStr))
	return &taskv1.MoveTaskStatusResponse{Meta: metaOK(req.Meta.RequestId), Task: t}, nil
}

// ── Sprints ───────────────────────────────────────────────────────────────────

func (s *TaskServer) CreateSprint(ctx context.Context, req *taskv1.CreateSprintRequest) (*taskv1.CreateSprintResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/CreateSprint")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, _, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}
	if !isAdminOrEditor(role) {
		return nil, status.Error(codes.PermissionDenied, "only admin or editor may create sprints")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	sp, err := s.store.CreateSprint(ctx, req.Name, req.Goal, req.StartDate, req.EndDate)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create sprint: %v", err)
	}
	s.log.InfoContext(ctx, "CreateSprint", slog.String("sprint_id", sp.SprintId))
	return &taskv1.CreateSprintResponse{Meta: metaOK(req.Meta.RequestId), Sprint: sp}, nil
}

func (s *TaskServer) UpdateSprint(ctx context.Context, req *taskv1.UpdateSprintRequest) (*taskv1.UpdateSprintResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/UpdateSprint")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, _, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}
	if !isAdminOrEditor(role) {
		return nil, status.Error(codes.PermissionDenied, "only admin or editor may update sprints")
	}

	if req.SprintId == "" {
		return nil, status.Error(codes.InvalidArgument, "sprint_id is required")
	}
	sp, err := s.store.UpdateSprint(ctx, req.SprintId, req.Name, req.Goal, req.StartDate, req.EndDate)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update sprint: %v", err)
	}
	s.log.InfoContext(ctx, "UpdateSprint", slog.String("sprint_id", req.SprintId))
	return &taskv1.UpdateSprintResponse{Meta: metaOK(req.Meta.RequestId), Sprint: sp}, nil
}

func (s *TaskServer) DeleteSprint(ctx context.Context, req *taskv1.DeleteSprintRequest) (*taskv1.DeleteSprintResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/DeleteSprint")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, _, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}
	if !isAdminOrEditor(role) {
		return nil, status.Error(codes.PermissionDenied, "only admin or editor may delete sprints")
	}

	if req.SprintId == "" {
		return nil, status.Error(codes.InvalidArgument, "sprint_id is required")
	}
	if err := s.store.DeleteSprint(ctx, req.SprintId); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "%v", err)
		}
		return nil, status.Errorf(codes.Internal, "delete sprint: %v", err)
	}
	s.log.InfoContext(ctx, "DeleteSprint", slog.String("sprint_id", req.SprintId))
	return &taskv1.DeleteSprintResponse{Meta: metaOK(req.Meta.RequestId), SprintId: req.SprintId}, nil
}

func (s *TaskServer) CloseSprint(ctx context.Context, req *taskv1.CloseSprintRequest) (*taskv1.CloseSprintResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/CloseSprint")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	role, _, err := callerRole(ctx, s.mgr)
	if err != nil {
		return nil, err
	}
	if !isAdminOrEditor(role) {
		return nil, status.Error(codes.PermissionDenied, "only admin or editor may close sprints")
	}

	if req.SprintId == "" {
		return nil, status.Error(codes.InvalidArgument, "sprint_id is required")
	}
	sp, err := s.store.CloseSprint(ctx, req.SprintId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "close sprint: %v", err)
	}
	s.log.InfoContext(ctx, "CloseSprint", slog.String("sprint_id", req.SprintId))
	return &taskv1.CloseSprintResponse{Meta: metaOK(req.Meta.RequestId), Sprint: sp}, nil
}

func (s *TaskServer) ListSprints(ctx context.Context, req *taskv1.ListSprintsRequest) (*taskv1.ListSprintsResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/ListSprints")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	sprints, err := s.store.ListSprints(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list sprints: %v", err)
	}
	return &taskv1.ListSprintsResponse{Meta: metaOK(req.Meta.RequestId), Sprints: sprints}, nil
}

func (s *TaskServer) GetSprintVelocity(ctx context.Context, req *taskv1.GetSprintVelocityRequest) (*taskv1.GetSprintVelocityResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetSprintVelocity")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}

	if req.SprintId == "" {
		return nil, status.Error(codes.InvalidArgument, "sprint_id is required")
	}
	vels, err := s.store.GetSprintVelocity(ctx, req.SprintId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get sprint velocity: %v", err)
	}
	return &taskv1.GetSprintVelocityResponse{Meta: metaOK(req.Meta.RequestId), Velocities: vels}, nil
}

// ── Reports ───────────────────────────────────────────────────────────────────

func (s *TaskServer) GetTaskStatusLog(ctx context.Context, req *taskv1.GetTaskStatusLogRequest) (*taskv1.GetTaskStatusLogResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetTaskStatusLog")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	if req.TaskId == "" {
		return nil, status.Error(codes.InvalidArgument, "task_id is required")
	}
	entries, err := s.store.GetTaskStatusLog(ctx, req.TaskId, req.PageSize)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "status log: %v", err)
	}
	var out []*taskv1.StatusLogEntry
	for _, e := range entries {
		out = append(out, &taskv1.StatusLogEntry{
			TaskId:      e.TaskID,
			FromStatus:  e.FromStatus,
			ToStatus:    e.ToStatus,
			ChangedById: displayOrID(e.ChangedByName, e.ChangedByID),
			ChangedAt:   e.ChangedAt,
		})
	}
	return &taskv1.GetTaskStatusLogResponse{Meta: metaOK(req.Meta.RequestId), Entries: out}, nil
}

func (s *TaskServer) GetTransitionReport(ctx context.Context, req *taskv1.GetTransitionReportRequest) (*taskv1.GetTransitionReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetTransitionReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	transitions, err := s.store.GetTransitionReport(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "transition report: %v", err)
	}
	var out []*taskv1.TransitionCount
	for _, tc := range transitions {
		out = append(out, &taskv1.TransitionCount{
			FromStatus: tc.FromStatus,
			ToStatus:   tc.ToStatus,
			Count:      tc.Count,
		})
	}
	return &taskv1.GetTransitionReportResponse{Meta: metaOK(req.Meta.RequestId), Transitions: out}, nil
}

func (s *TaskServer) GetThroughputReport(ctx context.Context, req *taskv1.GetThroughputReportRequest) (*taskv1.GetThroughputReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetThroughputReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	buckets, err := s.store.GetThroughputReport(ctx, req.Days)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "throughput report: %v", err)
	}
	var out []*taskv1.DailyThroughput
	for _, dt := range buckets {
		out = append(out, &taskv1.DailyThroughput{
			Date:        dt.Date,
			Count:       dt.Count,
			StoryPoints: dt.StoryPoints,
		})
	}
	return &taskv1.GetThroughputReportResponse{Meta: metaOK(req.Meta.RequestId), Buckets: out}, nil
}

func (s *TaskServer) GetAssigneeVelocityReport(ctx context.Context, req *taskv1.GetAssigneeVelocityReportRequest) (*taskv1.GetAssigneeVelocityReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetAssigneeVelocityReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	rows, err := s.store.GetAssigneeVelocityReport(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "assignee velocity report: %v", err)
	}
	var out []*taskv1.AssigneeVelocityItem
	for _, r := range rows {
		item := &taskv1.AssigneeVelocityItem{
			UserId:    displayOrID(r.DisplayName, r.UserID),
			AvgPoints: float32(r.Avg),
			StdDev:    float32(r.StdDev),
		}
		for _, sp := range r.Sprints {
			item.Sprints = append(item.Sprints, &taskv1.AssigneeSprintPoints{
				SprintId:   sp.SprintID,
				SprintName: sp.SprintName,
				Points:     sp.Points,
			})
		}
		out = append(out, item)
	}
	return &taskv1.GetAssigneeVelocityReportResponse{Meta: metaOK(req.Meta.RequestId), Velocities: out}, nil
}

func (s *TaskServer) GetSprintStatusReport(ctx context.Context, req *taskv1.GetSprintStatusReportRequest) (*taskv1.GetSprintStatusReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetSprintStatusReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	r, err := s.store.GetSprintStatusReport(ctx, req.SprintId)
	if err != nil {
		if errors.Is(err, store.ErrNoActiveSprint) {
			return nil, status.Errorf(codes.NotFound, "no active sprint")
		}
		return nil, status.Errorf(codes.Internal, "sprint status report: %v", err)
	}
	var buckets []*taskv1.StatusBucket
	for _, b := range r.Buckets {
		buckets = append(buckets, &taskv1.StatusBucket{
			Status:      b.Status,
			TaskCount:   b.TaskCount,
			StoryPoints: b.StoryPoints,
		})
	}
	return &taskv1.GetSprintStatusReportResponse{
		Meta:        metaOK(req.Meta.RequestId),
		SprintId:    r.SprintID,
		SprintName:  r.SprintName,
		Buckets:     buckets,
		TotalTasks:  r.TotalTasks,
		TotalPoints: r.TotalPoints,
	}, nil
}

func (s *TaskServer) GetEndOfDayReport(ctx context.Context, req *taskv1.GetEndOfDayReportRequest) (*taskv1.GetEndOfDayReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetEndOfDayReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	r, err := s.store.GetEndOfDayReport(ctx, req.SprintId)
	if err != nil {
		if errors.Is(err, store.ErrNoActiveSprint) {
			return nil, status.Errorf(codes.NotFound, "no active sprint")
		}
		return nil, status.Errorf(codes.Internal, "end of day report: %v", err)
	}
	var changes []*taskv1.StatusChangeEntry
	for _, c := range r.StatusChangesToday {
		changes = append(changes, &taskv1.StatusChangeEntry{
			TaskId:      c.TaskID,
			DisplayId:   c.DisplayID,
			Title:       c.Title,
			FromStatus:  c.FromStatus,
			ToStatus:    c.ToStatus,
			ChangedById: displayOrID(c.ChangedByName, c.ChangedByID),
			ChangedAt:   timestamppb.New(c.ChangedAt),
		})
	}
	var summaries []*taskv1.UserEodSummary
	for _, u := range r.UserSummaries {
		summaries = append(summaries, &taskv1.UserEodSummary{
			UserId:         displayOrID(u.DisplayName, u.UserID),
			CompletedToday: u.CompletedToday,
			PointsToday:    u.PointsToday,
			StatusChanges:  u.StatusChanges,
		})
	}
	return &taskv1.GetEndOfDayReportResponse{
		Meta:                  metaOK(req.Meta.RequestId),
		SprintId:              r.SprintID,
		SprintName:            r.SprintName,
		CompletedToday:        r.CompletedToday,
		CompletedPointsToday:  r.CompletedPointsToday,
		TotalSprintTasks:      r.TotalSprintTasks,
		TotalSprintPoints:     r.TotalSprintPoints,
		TotalCompleted:        r.TotalCompleted,
		TotalCompletedPoints:  r.TotalCompletedPoints,
		CloseProbability:      r.CloseProbability,
		StatusChangesToday:    changes,
		UserSummaries:         summaries,
	}, nil
}

func (s *TaskServer) GetReporterUsageReport(ctx context.Context, req *taskv1.GetReporterUsageReportRequest) (*taskv1.GetReporterUsageReportResponse, error) {
	if err := s.storeRequired(); err != nil {
		return nil, err
	}
	if err := validateMeta(req.GetMeta()); err != nil {
		return nil, err
	}
	ctx, span := trace.StartSpan(ctx, "task/GetReporterUsageReport")
	defer span.End()
	middleware.AddRequestAttributes(ctx, req.Meta.GetRequestId(), req.Meta.GetUserId())

	if _, _, err := callerRole(ctx, s.mgr); err != nil {
		return nil, err
	}
	rows, err := s.store.GetReporterUsageReport(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "reporter usage report: %v", err)
	}
	var out []*taskv1.ReporterUsageItem
	for _, r := range rows {
		item := &taskv1.ReporterUsageItem{
			ReporterId:           displayOrID(r.DisplayName, r.ReporterID),
			TotalTasks:           r.TotalTasks,
			CompletedOnTime:      r.CompletedOnTime,
			CompletedLate:        r.CompletedLate,
			OnTimePct:            r.OnTimePct,
			TotalPointsCompleted: r.TotalPointsCompleted,
		}
		for _, sc := range r.StatusCounts {
			item.StatusCounts = append(item.StatusCounts, &taskv1.ReporterStatusCount{
				Status: sc.Status,
				Count:  sc.Count,
			})
		}
		out = append(out, item)
	}
	return &taskv1.GetReporterUsageReportResponse{Meta: metaOK(req.Meta.RequestId), Items: out}, nil
}

// displayOrID returns displayName when non-empty, otherwise falls back to id.
func displayOrID(displayName, id string) string {
	if displayName != "" {
		return displayName
	}
	return id
}

// ── helpers ───────────────────────────────────────────────────────────────────

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
	return &commonv1.ResponseMeta{RequestId: requestID, Success: true, Timestamp: timestamppb.Now()}
}
