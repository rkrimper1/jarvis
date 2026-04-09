package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/rkrimper1/jarvis/api/pb/task"
)

const schema = `
CREATE TABLE IF NOT EXISTS sprints (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    goal       TEXT NOT NULL DEFAULT '',
    start_date TEXT NOT NULL,
    end_date   TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','closed')),
    created_at DATETIME DEFAULT (datetime('now')),
    updated_at DATETIME DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS task_seq (
    last_id INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS tasks (
    id              TEXT PRIMARY KEY,
    display_id      INTEGER NOT NULL DEFAULT 0,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    assignee_id     TEXT NOT NULL,
    reporter_id     TEXT NOT NULL,
    priority        TEXT NOT NULL DEFAULT 'medium' CHECK(priority IN ('critical','high','medium','low')),
    story_points    INTEGER NOT NULL DEFAULT 0,
    due_date        TEXT,
    sprint_id       TEXT REFERENCES sprints(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'unassigned' CHECK(status IN ('unassigned','assigned','in_progress','testing','review','completed')),
    task_type       TEXT NOT NULL DEFAULT 'task' CHECK(task_type IN ('task','epic','story','bug','subtask')),
    parent_id       TEXT REFERENCES tasks(id) ON DELETE SET NULL,
    completed_by_id TEXT,
    completed_at    DATETIME,
    created_at      DATETIME DEFAULT (datetime('now')),
    updated_at      DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_tasks_sprint ON tasks(sprint_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_assignee ON tasks(assignee_id);
`

// migrations are run after schema creation to add columns to existing databases.
const migrations = `
ALTER TABLE tasks ADD COLUMN display_id INTEGER NOT NULL DEFAULT 0;
INSERT OR IGNORE INTO task_seq VALUES (0);
`

type Store struct {
	db  *sql.DB
	log *slog.Logger
}

func New(dbPath string, log *slog.Logger) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("task store: open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("task store: apply schema: %w", err)
	}
	// Apply migrations best-effort — statements may fail if column already exists.
	for _, stmt := range []string{
		`ALTER TABLE tasks ADD COLUMN display_id INTEGER NOT NULL DEFAULT 0`,
		`INSERT OR IGNORE INTO task_seq VALUES (0)`,
		`ALTER TABLE tasks ADD COLUMN task_type TEXT NOT NULL DEFAULT 'task'`,
		`ALTER TABLE tasks ADD COLUMN parent_id TEXT`,
	} {
		_, _ = db.Exec(stmt)
	}
	return &Store{db: db, log: log}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (s *Store) CreateTask(ctx context.Context, title, description, assigneeID, reporterID, priority, taskType, parentID string, storyPoints int32, dueDate, sprintID string) (*taskv1.Task, error) {
	id := uuid.New().String()
	statusStr := "unassigned"
	if sprintID != "" {
		statusStr = "assigned"
	}

	// Allocate display_id atomically using the task_seq counter.
	var displayID int32
	err := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, `UPDATE task_seq SET last_id = last_id + 1`); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT last_id FROM task_seq`).Scan(&displayID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO tasks (id, display_id, title, description, assignee_id, reporter_id, priority, task_type, parent_id, story_points, due_date, sprint_id, status)
			 VALUES (?,?,?,?,?,?,?,?,nullif(?,?),?,nullif(?,?),nullif(?,?),?)`,
			id, displayID, title, description, assigneeID, reporterID, priority, taskType,
			parentID, "", storyPoints, dueDate, "", sprintID, "", statusStr,
		)
		if err != nil {
			return err
		}
		return tx.Commit()
	}()
	if err != nil {
		return nil, fmt.Errorf("task store: insert task: %w", err)
	}
	return s.GetTask(ctx, id)
}

func (s *Store) GetTask(ctx context.Context, id string) (*taskv1.Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,display_id,title,description,assignee_id,reporter_id,priority,story_points,
		        coalesce(due_date,''),coalesce(sprint_id,''),status,task_type,coalesce(parent_id,''),
		        coalesce(completed_by_id,''),completed_at,created_at,updated_at
		 FROM tasks WHERE id=?`, id)
	return scanTask(row)
}

func (s *Store) UpdateTask(ctx context.Context, id, title, description, assigneeID, priority, taskType, parentID string, storyPoints int32, dueDate string) (*taskv1.Task, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET title=?,description=?,assignee_id=?,priority=?,task_type=?,
		       parent_id=nullif(?,?),story_points=?,due_date=nullif(?,?),updated_at=datetime('now') WHERE id=?`,
		title, description, assigneeID, priority, taskType, parentID, "", storyPoints, dueDate, "", id,
	)
	if err != nil {
		return nil, fmt.Errorf("task store: update task: %w", err)
	}
	return s.GetTask(ctx, id)
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("task store: delete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not found")
	}
	return nil
}

func (s *Store) ListBacklog(ctx context.Context) ([]*taskv1.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,display_id,title,description,assignee_id,reporter_id,priority,story_points,
		        coalesce(due_date,''),coalesce(sprint_id,''),status,task_type,coalesce(parent_id,''),
		        coalesce(completed_by_id,''),completed_at,created_at,updated_at
		 FROM tasks WHERE sprint_id IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("task store: list backlog: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) ListBySprintID(ctx context.Context, sprintID string) ([]*taskv1.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,display_id,title,description,assignee_id,reporter_id,priority,story_points,
		        coalesce(due_date,''),coalesce(sprint_id,''),status,task_type,coalesce(parent_id,''),
		        coalesce(completed_by_id,''),completed_at,created_at,updated_at
		 FROM tasks WHERE sprint_id=? ORDER BY created_at DESC`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("task store: list by sprint: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) ListAll(ctx context.Context) ([]*taskv1.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,display_id,title,description,assignee_id,reporter_id,priority,story_points,
		        coalesce(due_date,''),coalesce(sprint_id,''),status,task_type,coalesce(parent_id,''),
		        coalesce(completed_by_id,''),completed_at,created_at,updated_at
		 FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("task store: list all: %w", err)
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) AssignToSprint(ctx context.Context, taskID, sprintID string) (*taskv1.Task, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET sprint_id=?,status='assigned',updated_at=datetime('now') WHERE id=?`,
		sprintID, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("task store: assign to sprint: %w", err)
	}
	return s.GetTask(ctx, taskID)
}

func (s *Store) MoveStatus(ctx context.Context, taskID, newStatus, completedBy string) (*taskv1.Task, error) {
	var err error
	if newStatus == "completed" {
		_, err = s.db.ExecContext(ctx,
			`UPDATE tasks SET status=?,completed_by_id=?,completed_at=datetime('now'),updated_at=datetime('now') WHERE id=?`,
			newStatus, completedBy, taskID,
		)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE tasks SET status=?,completed_by_id=NULL,completed_at=NULL,updated_at=datetime('now') WHERE id=?`,
			newStatus, taskID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("task store: move status: %w", err)
	}
	return s.GetTask(ctx, taskID)
}

// ── Sprints ───────────────────────────────────────────────────────────────────

func (s *Store) CreateSprint(ctx context.Context, name, goal, startDate, endDate string) (*taskv1.Sprint, error) {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sprints (id, name, goal, start_date, end_date) VALUES (?,?,?,?,?)`,
		id, name, goal, startDate, endDate,
	)
	if err != nil {
		return nil, fmt.Errorf("task store: insert sprint: %w", err)
	}
	return s.GetSprint(ctx, id)
}

func (s *Store) GetSprint(ctx context.Context, id string) (*taskv1.Sprint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,name,goal,start_date,end_date,status,created_at,updated_at FROM sprints WHERE id=?`, id)
	return scanSprint(row)
}

func (s *Store) UpdateSprint(ctx context.Context, id, name, goal, startDate, endDate string) (*taskv1.Sprint, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sprints SET name=?,goal=?,start_date=?,end_date=?,updated_at=datetime('now') WHERE id=?`,
		name, goal, startDate, endDate, id,
	)
	if err != nil {
		return nil, fmt.Errorf("task store: update sprint: %w", err)
	}
	return s.GetSprint(ctx, id)
}

func (s *Store) DeleteSprint(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sprints WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("task store: delete sprint: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sprint not found")
	}
	return nil
}

func (s *Store) CloseSprint(ctx context.Context, id string) (*taskv1.Sprint, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sprints SET status='closed',updated_at=datetime('now') WHERE id=?`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("task store: close sprint: %w", err)
	}
	return s.GetSprint(ctx, id)
}

func (s *Store) ListSprints(ctx context.Context) ([]*taskv1.Sprint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,goal,start_date,end_date,status,created_at,updated_at
		 FROM sprints ORDER BY CASE status WHEN 'active' THEN 0 ELSE 1 END, created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("task store: list sprints: %w", err)
	}
	defer rows.Close()
	var out []*taskv1.Sprint
	for rows.Next() {
		sp, err := scanSprintRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *Store) GetSprintVelocity(ctx context.Context, sprintID string) ([]*taskv1.UserVelocity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT completed_by_id, SUM(story_points) FROM tasks
		 WHERE sprint_id=? AND status='completed' AND completed_by_id != ''
		 GROUP BY completed_by_id`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("task store: sprint velocity: %w", err)
	}
	defer rows.Close()
	var out []*taskv1.UserVelocity
	for rows.Next() {
		var v taskv1.UserVelocity
		if err := rows.Scan(&v.UserId, &v.StoryPoints); err != nil {
			return nil, fmt.Errorf("task store: scan velocity: %w", err)
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

// ── scan helpers ──────────────────────────────────────────────────────────────

func scanTask(row *sql.Row) (*taskv1.Task, error) {
	var t taskv1.Task
	var priorityStr, statusStr, taskTypeStr string
	var completedAt sql.NullString
	var createdAt, updatedAt string
	err := row.Scan(
		&t.TaskId, &t.DisplayId, &t.Title, &t.Description, &t.AssigneeId, &t.ReporterId,
		&priorityStr, &t.StoryPoints, &t.DueDate, &t.SprintId, &statusStr,
		&taskTypeStr, &t.ParentId, &t.CompletedById, &completedAt, &createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task not found")
	}
	if err != nil {
		return nil, fmt.Errorf("task store: scan task: %w", err)
	}
	t.Priority = priorityToProto(priorityStr)
	t.Status = taskStatusToProto(statusStr)
	t.TaskType = taskTypeToProto(taskTypeStr)
	if completedAt.Valid {
		t.CompletedAt = parseTS(completedAt.String)
	}
	t.CreatedAt = parseTS(createdAt)
	t.UpdatedAt = parseTS(updatedAt)
	return &t, nil
}

func scanTasks(rows *sql.Rows) ([]*taskv1.Task, error) {
	var out []*taskv1.Task
	for rows.Next() {
		var t taskv1.Task
		var priorityStr, statusStr, taskTypeStr string
		var completedAt sql.NullString
		var createdAt, updatedAt string
		err := rows.Scan(
			&t.TaskId, &t.DisplayId, &t.Title, &t.Description, &t.AssigneeId, &t.ReporterId,
			&priorityStr, &t.StoryPoints, &t.DueDate, &t.SprintId, &statusStr,
			&taskTypeStr, &t.ParentId, &t.CompletedById, &completedAt, &createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("task store: scan tasks row: %w", err)
		}
		t.Priority = priorityToProto(priorityStr)
		t.Status = taskStatusToProto(statusStr)
		t.TaskType = taskTypeToProto(taskTypeStr)
		if completedAt.Valid {
			t.CompletedAt = parseTS(completedAt.String)
		}
		t.CreatedAt = parseTS(createdAt)
		t.UpdatedAt = parseTS(updatedAt)
		out = append(out, &t)
	}
	return out, rows.Err()
}

func scanSprint(row *sql.Row) (*taskv1.Sprint, error) {
	var sp taskv1.Sprint
	var statusStr, createdAt, updatedAt string
	err := row.Scan(&sp.SprintId, &sp.Name, &sp.Goal, &sp.StartDate, &sp.EndDate, &statusStr, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sprint not found")
	}
	if err != nil {
		return nil, fmt.Errorf("task store: scan sprint: %w", err)
	}
	sp.Status = sprintStatusToProto(statusStr)
	sp.CreatedAt = parseTS(createdAt)
	sp.UpdatedAt = parseTS(updatedAt)
	return &sp, nil
}

func scanSprintRow(rows *sql.Rows) (*taskv1.Sprint, error) {
	var sp taskv1.Sprint
	var statusStr, createdAt, updatedAt string
	err := rows.Scan(&sp.SprintId, &sp.Name, &sp.Goal, &sp.StartDate, &sp.EndDate, &statusStr, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("task store: scan sprint row: %w", err)
	}
	sp.Status = sprintStatusToProto(statusStr)
	sp.CreatedAt = parseTS(createdAt)
	sp.UpdatedAt = parseTS(updatedAt)
	return &sp, nil
}

// ── enum converters ───────────────────────────────────────────────────────────

func priorityToProto(s string) taskv1.TaskPriority {
	switch s {
	case "critical":
		return taskv1.TaskPriority_TASK_PRIORITY_CRITICAL
	case "high":
		return taskv1.TaskPriority_TASK_PRIORITY_HIGH
	case "medium":
		return taskv1.TaskPriority_TASK_PRIORITY_MEDIUM
	case "low":
		return taskv1.TaskPriority_TASK_PRIORITY_LOW
	default:
		return taskv1.TaskPriority_TASK_PRIORITY_UNSPECIFIED
	}
}

func priorityToString(p taskv1.TaskPriority) string {
	switch p {
	case taskv1.TaskPriority_TASK_PRIORITY_CRITICAL:
		return "critical"
	case taskv1.TaskPriority_TASK_PRIORITY_HIGH:
		return "high"
	case taskv1.TaskPriority_TASK_PRIORITY_LOW:
		return "low"
	default:
		return "medium"
	}
}

func taskStatusToProto(s string) taskv1.TaskStatus {
	switch s {
	case "unassigned":
		return taskv1.TaskStatus_TASK_STATUS_UNASSIGNED
	case "assigned":
		return taskv1.TaskStatus_TASK_STATUS_ASSIGNED
	case "in_progress":
		return taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	case "testing":
		return taskv1.TaskStatus_TASK_STATUS_TESTING
	case "review":
		return taskv1.TaskStatus_TASK_STATUS_REVIEW
	case "completed":
		return taskv1.TaskStatus_TASK_STATUS_COMPLETED
	default:
		return taskv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}

func taskStatusToString(s taskv1.TaskStatus) string {
	switch s {
	case taskv1.TaskStatus_TASK_STATUS_UNASSIGNED:
		return "unassigned"
	case taskv1.TaskStatus_TASK_STATUS_ASSIGNED:
		return "assigned"
	case taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "in_progress"
	case taskv1.TaskStatus_TASK_STATUS_TESTING:
		return "testing"
	case taskv1.TaskStatus_TASK_STATUS_REVIEW:
		return "review"
	case taskv1.TaskStatus_TASK_STATUS_COMPLETED:
		return "completed"
	default:
		return "unassigned"
	}
}

func taskTypeToProto(s string) taskv1.TaskType {
	switch s {
	case "epic":
		return taskv1.TaskType_TASK_TYPE_EPIC
	case "story":
		return taskv1.TaskType_TASK_TYPE_STORY
	case "bug":
		return taskv1.TaskType_TASK_TYPE_BUG
	case "subtask":
		return taskv1.TaskType_TASK_TYPE_SUBTASK
	default:
		return taskv1.TaskType_TASK_TYPE_TASK
	}
}

func taskTypeToString(t taskv1.TaskType) string {
	switch t {
	case taskv1.TaskType_TASK_TYPE_EPIC:
		return "epic"
	case taskv1.TaskType_TASK_TYPE_STORY:
		return "story"
	case taskv1.TaskType_TASK_TYPE_BUG:
		return "bug"
	case taskv1.TaskType_TASK_TYPE_SUBTASK:
		return "subtask"
	default:
		return "task"
	}
}

func sprintStatusToProto(s string) taskv1.SprintStatus {
	switch s {
	case "active":
		return taskv1.SprintStatus_SPRINT_STATUS_ACTIVE
	case "closed":
		return taskv1.SprintStatus_SPRINT_STATUS_CLOSED
	default:
		return taskv1.SprintStatus_SPRINT_STATUS_UNSPECIFIED
	}
}

func parseTS(s string) *timestamppb.Timestamp {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return timestamppb.New(t)
		}
	}
	return timestamppb.Now()
}

// exported for server use
var PriorityToString = priorityToString
var TaskStatusToString = taskStatusToString
var TaskTypeToString = taskTypeToString
