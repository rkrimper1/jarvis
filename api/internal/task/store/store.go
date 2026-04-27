package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskv1 "github.com/rkrimper1/jarvis/api/pb/task"
)

// ErrNotFound is returned by store methods when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// ErrNoActiveSprint is returned when a report requires an active sprint but none exists.
var ErrNoActiveSprint = errors.New("no active sprint")

const schema = `
CREATE TABLE IF NOT EXISTS task_status_log (
    id            TEXT PRIMARY KEY,
    task_id       TEXT NOT NULL,
    from_status   TEXT NOT NULL DEFAULT '',
    to_status     TEXT NOT NULL,
    changed_by_id TEXT NOT NULL DEFAULT '',
    changed_at    DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_status_log_task       ON task_status_log(task_id);
CREATE INDEX IF NOT EXISTS idx_status_log_changed_at ON task_status_log(changed_at);

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

type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New applies the schema and best-effort migrations to the shared db.
// The caller owns db and is responsible for closing it.
func New(db *sql.DB, log *slog.Logger) (*Store, error) {
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

// Close is a no-op: the caller owns the shared *sql.DB and is responsible for closing it.
func (s *Store) Close() error { return nil }

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
	return s.scanTask(row)
}

func (s *Store) UpdateTask(ctx context.Context, id, title, description, assigneeID, reporterID, priority, taskType, parentID string, storyPoints int32, dueDate string) (*taskv1.Task, error) {
	q := `UPDATE tasks SET title=?,description=?,assignee_id=?,priority=?,task_type=?,
		       parent_id=nullif(?,?),story_points=?,due_date=nullif(?,?),updated_at=datetime('now')`
	args := []any{title, description, assigneeID, priority, taskType, parentID, "", storyPoints, dueDate, ""}
	if reporterID != "" {
		q += `,reporter_id=?`
		args = append(args, reporterID)
	}
	q += ` WHERE id=?`
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
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
		return fmt.Errorf("task %s: %w", id, ErrNotFound)
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
	return s.scanTasks(rows)
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
	return s.scanTasks(rows)
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
	return s.scanTasks(rows)
}

func (s *Store) AssignToSprint(ctx context.Context, taskID, sprintID, changedByID string) (*taskv1.Task, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var fromStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, taskID).Scan(&fromStatus); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET sprint_id=?,status='assigned',updated_at=datetime('now') WHERE id=?`,
			sprintID, taskID,
		); err != nil {
			return err
		}
		return s.insertLogTx(ctx, tx, taskID, fromStatus, "assigned", changedByID)
	})
	if err != nil {
		return nil, fmt.Errorf("task store: assign to sprint: %w", err)
	}
	return s.GetTask(ctx, taskID)
}

// RemoveFromSprint clears the sprint assignment and resets the task status to unassigned.
func (s *Store) RemoveFromSprint(ctx context.Context, taskID, changedByID string) (*taskv1.Task, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var fromStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, taskID).Scan(&fromStatus); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET sprint_id=NULL,status='unassigned',updated_at=datetime('now') WHERE id=?`,
			taskID,
		); err != nil {
			return err
		}
		return s.insertLogTx(ctx, tx, taskID, fromStatus, "unassigned", changedByID)
	})
	if err != nil {
		return nil, fmt.Errorf("task store: remove from sprint: %w", err)
	}
	return s.GetTask(ctx, taskID)
}

func (s *Store) MoveStatus(ctx context.Context, taskID, newStatus, completedBy string) (*taskv1.Task, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var fromStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM tasks WHERE id=?`, taskID).Scan(&fromStatus); err != nil {
			return err
		}
		if newStatus == "completed" {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET status=?,completed_by_id=?,completed_at=datetime('now'),updated_at=datetime('now') WHERE id=?`,
				newStatus, completedBy, taskID,
			); err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx,
				`UPDATE tasks SET status=?,completed_by_id=NULL,completed_at=NULL,updated_at=datetime('now') WHERE id=?`,
				newStatus, taskID,
			); err != nil {
				return err
			}
		}
		return s.insertLogTx(ctx, tx, taskID, fromStatus, newStatus, completedBy)
	})
	if err != nil {
		return nil, fmt.Errorf("task store: move status: %w", err)
	}
	return s.GetTask(ctx, taskID)
}

// ── Status log ────────────────────────────────────────────────────────────────

// StatusLogEntry is a single row from task_status_log.
type StatusLogEntry struct {
	ID              string
	TaskID          string
	FromStatus      string
	ToStatus        string
	ChangedByID     string
	ChangedByName   string // display_name from users; empty if user not found
	ChangedAt       string
}

// TransitionCount is an aggregated from→to transition count.
type TransitionCount struct {
	FromStatus string
	ToStatus   string
	Count      int32
}

// DailyThroughput is the number of tasks completed and story points delivered on a single day.
type DailyThroughput struct {
	Date        string
	Count       int32
	StoryPoints int32
}

func (s *Store) GetTaskStatusLog(ctx context.Context, taskID string, pageSize int32) ([]*StatusLogEntry, error) {
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT l.id, l.task_id, l.from_status, l.to_status, l.changed_by_id,
		        COALESCE(u.display_name,''), strftime('%Y-%m-%dT%H:%M:%SZ',l.changed_at)
		 FROM task_status_log l
		 LEFT JOIN users u ON u.id = l.changed_by_id
		 WHERE l.task_id=?
		 ORDER BY l.changed_at DESC LIMIT ?`, taskID, pageSize)
	if err != nil {
		return nil, fmt.Errorf("task store: status log: %w", err)
	}
	defer rows.Close()
	var out []*StatusLogEntry
	for rows.Next() {
		e := &StatusLogEntry{}
		if err := rows.Scan(&e.ID, &e.TaskID, &e.FromStatus, &e.ToStatus, &e.ChangedByID, &e.ChangedByName, &e.ChangedAt); err != nil {
			return nil, fmt.Errorf("task store: scan status log: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetTransitionReport(ctx context.Context) ([]*TransitionCount, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_status,to_status,COUNT(*) FROM task_status_log
		 GROUP BY from_status,to_status ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil, fmt.Errorf("task store: transition report: %w", err)
	}
	defer rows.Close()
	var out []*TransitionCount
	for rows.Next() {
		tc := &TransitionCount{}
		if err := rows.Scan(&tc.FromStatus, &tc.ToStatus, &tc.Count); err != nil {
			return nil, fmt.Errorf("task store: scan transition: %w", err)
		}
		out = append(out, tc)
	}
	return out, rows.Err()
}

func (s *Store) GetThroughputReport(ctx context.Context, days int32) ([]*DailyThroughput, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT date(changed_at) as day, COUNT(*), COALESCE(SUM(t.story_points),0)
		 FROM task_status_log l
		 JOIN tasks t ON t.id = l.task_id
		 WHERE l.to_status='completed'
		   AND l.changed_at >= datetime('now',?||' days')
		 GROUP BY day ORDER BY day ASC`,
		fmt.Sprintf("-%d", days))
	if err != nil {
		return nil, fmt.Errorf("task store: throughput report: %w", err)
	}
	defer rows.Close()
	var out []*DailyThroughput
	for rows.Next() {
		dt := &DailyThroughput{}
		if err := rows.Scan(&dt.Date, &dt.Count, &dt.StoryPoints); err != nil {
			return nil, fmt.Errorf("task store: scan throughput: %w", err)
		}
		out = append(out, dt)
	}
	return out, rows.Err()
}

// ── Extended reports ──────────────────────────────────────────────────────────

// AssigneeSprintPts is points delivered by one assignee in one sprint.
type AssigneeSprintPts struct {
	SprintID   string
	SprintName string
	Points     int32
}

// AssigneeVelocity aggregates sprint points for one assignee.
type AssigneeVelocity struct {
	UserID      string
	DisplayName string // display_name from users; empty if user not found
	Avg         float64
	StdDev      float64
	Sprints     []AssigneeSprintPts
}

func (s *Store) GetAssigneeVelocityReport(ctx context.Context) ([]*AssigneeVelocity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.assignee_id, COALESCE(u.display_name,''), sp.id, sp.name, COALESCE(SUM(t.story_points),0)
		FROM tasks t
		JOIN sprints sp ON sp.id = t.sprint_id
		LEFT JOIN users u ON u.id = t.assignee_id
		WHERE t.status='completed' AND t.assignee_id != ''
		GROUP BY t.assignee_id, sp.id
		ORDER BY t.assignee_id, sp.created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("task store: assignee velocity: %w", err)
	}
	defer rows.Close()

	byUser := map[string]*AssigneeVelocity{}
	var order []string
	for rows.Next() {
		var userID, displayName, sprintID, sprintName string
		var pts int32
		if err := rows.Scan(&userID, &displayName, &sprintID, &sprintName, &pts); err != nil {
			return nil, err
		}
		if _, ok := byUser[userID]; !ok {
			byUser[userID] = &AssigneeVelocity{UserID: userID, DisplayName: displayName}
			order = append(order, userID)
		}
		byUser[userID].Sprints = append(byUser[userID].Sprints, AssigneeSprintPts{SprintID: sprintID, SprintName: sprintName, Points: pts})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*AssigneeVelocity, 0, len(order))
	for _, uid := range order {
		v := byUser[uid]
		var sum float64
		for _, sp := range v.Sprints {
			sum += float64(sp.Points)
		}
		n := float64(len(v.Sprints))
		if n > 0 {
			v.Avg = sum / n
			var variance float64
			for _, sp := range v.Sprints {
				d := float64(sp.Points) - v.Avg
				variance += d * d
			}
			if n > 1 {
				v.StdDev = math.Sqrt(variance / n)
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// StatusBucket is task count + story points for one status.
type StatusBucket struct {
	Status      string
	TaskCount   int32
	StoryPoints int32
}

// SprintStatusReport holds the sprint status report.
type SprintStatusReport struct {
	SprintID    string
	SprintName  string
	Buckets     []StatusBucket
	TotalTasks  int32
	TotalPoints int32
}

// GetSprintStatusReport returns task counts per status for a sprint.
// If sprintID is empty, uses the currently active sprint.
func (s *Store) GetSprintStatusReport(ctx context.Context, sprintID string) (*SprintStatusReport, error) {
	var sprintName string
	if sprintID == "" {
		err := s.db.QueryRowContext(ctx,
			`SELECT id, name FROM sprints WHERE status='active' AND start_date <= date('now') AND end_date >= date('now') ORDER BY created_at ASC LIMIT 1`,
		).Scan(&sprintID, &sprintName)
		if err == sql.ErrNoRows {
			return nil, ErrNoActiveSprint
		}
		if err != nil {
			return nil, fmt.Errorf("task store: find active sprint: %w", err)
		}
	} else {
		if err := s.db.QueryRowContext(ctx, `SELECT name FROM sprints WHERE id=?`, sprintID).Scan(&sprintName); err != nil {
			return nil, fmt.Errorf("task store: sprint not found: %w", err)
		}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(*), COALESCE(SUM(story_points),0) FROM tasks WHERE sprint_id=? GROUP BY status`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("task store: sprint status report: %w", err)
	}
	defer rows.Close()
	report := &SprintStatusReport{SprintID: sprintID, SprintName: sprintName}
	for rows.Next() {
		var b StatusBucket
		if err := rows.Scan(&b.Status, &b.TaskCount, &b.StoryPoints); err != nil {
			return nil, err
		}
		report.Buckets = append(report.Buckets, b)
		report.TotalTasks += b.TaskCount
		report.TotalPoints += b.StoryPoints
	}
	return report, rows.Err()
}

// StatusChangeEntry is a single status transition from the log.
type StatusChangeEntry struct {
	TaskID        string
	DisplayID     int32
	Title         string
	FromStatus    string
	ToStatus      string
	ChangedByID   string
	ChangedByName string // display_name from users; empty if user not found
	ChangedAt     time.Time
}

// UserEODSummary is the per-user end-of-day summary.
type UserEODSummary struct {
	UserID         string
	DisplayName    string // display_name from users; empty if user not found
	CompletedToday int32
	PointsToday    int32
	StatusChanges  int32
}

// EODReport holds end-of-day report data.
type EODReport struct {
	SprintID             string
	SprintName           string
	CompletedToday       int32
	CompletedPointsToday int32
	TotalSprintTasks     int32
	TotalSprintPoints    int32
	TotalCompleted       int32
	TotalCompletedPoints int32
	CloseProbability     float32
	StatusChangesToday   []StatusChangeEntry
	UserSummaries        []UserEODSummary
}

// GetEndOfDayReport returns the end-of-day status snapshot for a sprint.
// If sprintID is empty, uses the currently active sprint.
func (s *Store) GetEndOfDayReport(ctx context.Context, sprintID string) (*EODReport, error) {
	var sprintName string
	if sprintID == "" {
		err := s.db.QueryRowContext(ctx,
			`SELECT id, name FROM sprints WHERE status='active' AND start_date <= date('now') AND end_date >= date('now') ORDER BY created_at ASC LIMIT 1`,
		).Scan(&sprintID, &sprintName)
		if err == sql.ErrNoRows {
			return nil, ErrNoActiveSprint
		}
		if err != nil {
			return nil, fmt.Errorf("task store: find active sprint: %w", err)
		}
	} else {
		if err := s.db.QueryRowContext(ctx, `SELECT name FROM sprints WHERE id=?`, sprintID).Scan(&sprintName); err != nil {
			return nil, fmt.Errorf("task store: sprint not found: %w", err)
		}
	}
	report := &EODReport{SprintID: sprintID, SprintName: sprintName}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(story_points),0) FROM tasks WHERE sprint_id=?`, sprintID,
	).Scan(&report.TotalSprintTasks, &report.TotalSprintPoints); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("task store: eod sprint totals: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(story_points),0) FROM tasks WHERE sprint_id=? AND status='completed'`, sprintID,
	).Scan(&report.TotalCompleted, &report.TotalCompletedPoints); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("task store: eod completed totals: %w", err)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(story_points),0) FROM tasks WHERE sprint_id=? AND status='completed' AND date(completed_at)=date('now')`, sprintID,
	).Scan(&report.CompletedToday, &report.CompletedPointsToday); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("task store: eod completed today: %w", err)
	}
	if report.TotalSprintPoints > 0 {
		report.CloseProbability = float32(report.TotalCompletedPoints) / float32(report.TotalSprintPoints)
	}

	changeRows, err := s.db.QueryContext(ctx, `
		SELECT l.task_id, COALESCE(t.display_id,0), COALESCE(t.title,''),
		       l.from_status, l.to_status, l.changed_by_id, COALESCE(u.display_name,''), l.changed_at
		FROM task_status_log l
		LEFT JOIN tasks t ON t.id = l.task_id
		LEFT JOIN users u ON u.id = l.changed_by_id
		WHERE date(l.changed_at) = date('now')
		  AND t.sprint_id = ?
		ORDER BY l.changed_at DESC`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("task store: eod status changes: %w", err)
	}
	defer changeRows.Close()
	userChanges := map[string]int32{}
	userNames := map[string]string{}
	for changeRows.Next() {
		var e StatusChangeEntry
		var changedAtStr string
		if err := changeRows.Scan(&e.TaskID, &e.DisplayID, &e.Title, &e.FromStatus, &e.ToStatus, &e.ChangedByID, &e.ChangedByName, &changedAtStr); err != nil {
			return nil, err
		}
		for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
			if t, err2 := time.Parse(layout, changedAtStr); err2 == nil {
				e.ChangedAt = t
				break
			}
		}
		report.StatusChangesToday = append(report.StatusChangesToday, e)
		userChanges[e.ChangedByID]++
		if e.ChangedByName != "" {
			userNames[e.ChangedByID] = e.ChangedByName
		}
	}
	if err := changeRows.Err(); err != nil {
		return nil, err
	}

	userRows, err := s.db.QueryContext(ctx,
		`SELECT t.assignee_id, COALESCE(u.display_name,''), COUNT(*), COALESCE(SUM(t.story_points),0)
		 FROM tasks t
		 LEFT JOIN users u ON u.id = t.assignee_id
		 WHERE t.sprint_id=? AND t.status='completed' AND date(t.completed_at)=date('now') AND t.assignee_id != ''
		 GROUP BY t.assignee_id`, sprintID)
	if err != nil {
		return nil, fmt.Errorf("task store: eod user summary: %w", err)
	}
	defer userRows.Close()
	byUser := map[string]*UserEODSummary{}
	for userRows.Next() {
		var u UserEODSummary
		if err := userRows.Scan(&u.UserID, &u.DisplayName, &u.CompletedToday, &u.PointsToday); err != nil {
			return nil, err
		}
		byUser[u.UserID] = &u
	}
	for uid, changes := range userChanges {
		if uid == "" {
			continue
		}
		if _, ok := byUser[uid]; !ok {
			byUser[uid] = &UserEODSummary{UserID: uid, DisplayName: userNames[uid]}
		}
		byUser[uid].StatusChanges = changes
	}
	for _, u := range byUser {
		report.UserSummaries = append(report.UserSummaries, *u)
	}
	return report, nil
}

// ReporterStatusCount is a single status bucket for the reporter usage report.
type ReporterStatusCount struct {
	Status string
	Count  int32
}

// ReporterUsageRow is per-reporter summary data.
type ReporterUsageRow struct {
	ReporterID           string
	DisplayName          string // display_name from users; empty if user not found
	StatusCounts         []ReporterStatusCount
	TotalTasks           int32
	CompletedOnTime      int32
	CompletedLate        int32
	OnTimePct            float32
	TotalPointsCompleted int32
}

func (s *Store) GetReporterUsageReport(ctx context.Context) ([]*ReporterUsageRow, error) {
	statusRows, err := s.db.QueryContext(ctx,
		`SELECT t.reporter_id, COALESCE(u.display_name,''), t.status, COUNT(*)
		 FROM tasks t
		 LEFT JOIN users u ON u.id = t.reporter_id
		 WHERE t.reporter_id != ''
		 GROUP BY t.reporter_id, t.status
		 ORDER BY t.reporter_id`)
	if err != nil {
		return nil, fmt.Errorf("task store: reporter usage status: %w", err)
	}
	defer statusRows.Close()
	byReporter := map[string]*ReporterUsageRow{}
	var order []string
	for statusRows.Next() {
		var rid, displayName, st string
		var count int32
		if err := statusRows.Scan(&rid, &displayName, &st, &count); err != nil {
			return nil, err
		}
		if _, ok := byReporter[rid]; !ok {
			byReporter[rid] = &ReporterUsageRow{ReporterID: rid, DisplayName: displayName}
			order = append(order, rid)
		}
		byReporter[rid].StatusCounts = append(byReporter[rid].StatusCounts, ReporterStatusCount{Status: st, Count: count})
		byReporter[rid].TotalTasks += count
	}
	if err := statusRows.Err(); err != nil {
		return nil, err
	}

	onTimeRows, err := s.db.QueryContext(ctx, `
		SELECT reporter_id,
		    SUM(CASE WHEN date(completed_at) <= due_date THEN 1 ELSE 0 END),
		    SUM(CASE WHEN date(completed_at) >  due_date THEN 1 ELSE 0 END),
		    COALESCE(SUM(story_points),0)
		FROM tasks
		WHERE status='completed' AND completed_at IS NOT NULL AND due_date IS NOT NULL AND due_date != ''
		GROUP BY reporter_id`)
	if err != nil {
		return nil, fmt.Errorf("task store: reporter usage on-time: %w", err)
	}
	defer onTimeRows.Close()
	for onTimeRows.Next() {
		var rid string
		var onTime, late, pts int32
		if err := onTimeRows.Scan(&rid, &onTime, &late, &pts); err != nil {
			return nil, err
		}
		if _, ok := byReporter[rid]; !ok {
			byReporter[rid] = &ReporterUsageRow{ReporterID: rid}
			order = append(order, rid)
		}
		r := byReporter[rid]
		r.CompletedOnTime = onTime
		r.CompletedLate = late
		r.TotalPointsCompleted = pts
		if total := onTime + late; total > 0 {
			r.OnTimePct = float32(onTime) / float32(total) * 100
		}
	}
	if err := onTimeRows.Err(); err != nil {
		return nil, err
	}
	out := make([]*ReporterUsageRow, 0, len(order))
	for _, rid := range order {
		out = append(out, byReporter[rid])
	}
	return out, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) insertLogTx(ctx context.Context, tx *sql.Tx, taskID, fromStatus, toStatus, changedByID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO task_status_log (id,task_id,from_status,to_status,changed_by_id)
		 VALUES (?,?,?,?,?)`,
		uuid.New().String(), taskID, fromStatus, toStatus, changedByID,
	)
	return err
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
	return s.scanSprint(row)
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
		return fmt.Errorf("sprint %s: %w", id, ErrNotFound)
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
		sp, err := s.scanSprintRow(rows)
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

func (s *Store) scanTask(row *sql.Row) (*taskv1.Task, error) {
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
		return nil, fmt.Errorf("task %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("task store: scan task: %w", err)
	}
	t.Priority = priorityToProto(priorityStr)
	t.Status = taskStatusToProto(statusStr)
	t.TaskType = taskTypeToProto(taskTypeStr)
	if completedAt.Valid {
		t.CompletedAt = s.parseTS(completedAt.String)
	}
	t.CreatedAt = s.parseTS(createdAt)
	t.UpdatedAt = s.parseTS(updatedAt)
	return &t, nil
}

func (s *Store) scanTasks(rows *sql.Rows) ([]*taskv1.Task, error) {
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
			t.CompletedAt = s.parseTS(completedAt.String)
		}
		t.CreatedAt = s.parseTS(createdAt)
		t.UpdatedAt = s.parseTS(updatedAt)
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (s *Store) scanSprint(row *sql.Row) (*taskv1.Sprint, error) {
	var sp taskv1.Sprint
	var statusStr, createdAt, updatedAt string
	err := row.Scan(&sp.SprintId, &sp.Name, &sp.Goal, &sp.StartDate, &sp.EndDate, &statusStr, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("sprint %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("task store: scan sprint: %w", err)
	}
	sp.Status = sprintStatusToProto(statusStr)
	sp.CreatedAt = s.parseTS(createdAt)
	sp.UpdatedAt = s.parseTS(updatedAt)
	return &sp, nil
}

func (s *Store) scanSprintRow(rows *sql.Rows) (*taskv1.Sprint, error) {
	var sp taskv1.Sprint
	var statusStr, createdAt, updatedAt string
	err := rows.Scan(&sp.SprintId, &sp.Name, &sp.Goal, &sp.StartDate, &sp.EndDate, &statusStr, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("task store: scan sprint row: %w", err)
	}
	sp.Status = sprintStatusToProto(statusStr)
	sp.CreatedAt = s.parseTS(createdAt)
	sp.UpdatedAt = s.parseTS(updatedAt)
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

func (s *Store) parseTS(raw string) *timestamppb.Timestamp {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return timestamppb.New(t)
		}
	}
	s.log.Warn("task store: unrecognised timestamp format", "value", raw)
	return timestamppb.New(time.Time{})
}

// exported for server use
var PriorityToString = priorityToString
var TaskStatusToString = taskStatusToString
var TaskTypeToString = taskTypeToString
