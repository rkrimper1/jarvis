// Package notify sends task-assignment email notifications.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/rkrimper1/jarvis/api/internal/integrations/email"
	taskv1 "github.com/rkrimper1/jarvis/api/pb/task"
)

// Config controls task assignment email behaviour.
type Config struct {
	SMTP       email.Config
	NotifySelf bool // if false, skip email when assignee == reporter
}

// Notifier sends task-assignment emails in a background goroutine so it
// never blocks the RPC response path.
type Notifier struct {
	cfg Config
	log *slog.Logger
}

// New returns a Notifier ready to send notifications.
func New(cfg Config, log *slog.Logger) *Notifier {
	return &Notifier{cfg: cfg, log: log}
}

// SendAssignment fires an assignment email in the background.
// assigneeEmail is the recipient; reporterEmail is used only for the
// self-assign check (TASK_NOTIFY_SELF_ASSIGN). Either may be empty —
// an empty assigneeEmail is a no-op.
func (n *Notifier) SendAssignment(ctx context.Context, task *taskv1.Task, assigneeEmail, reporterEmail string) {
	if assigneeEmail == "" {
		return
	}
	if !n.cfg.NotifySelf && assigneeEmail == reporterEmail {
		n.log.InfoContext(ctx, "task notify: skipping self-assign",
			slog.String("task_id", task.TaskId),
		)
		return
	}
	// Capture values used in the goroutine before they can change.
	cfg := n.cfg
	log := n.log
	detachedCtx := context.WithoutCancel(ctx)
	go func() {
		displayID := fmt.Sprintf("JARVIS-%04d", task.DisplayId)
		subject := fmt.Sprintf("%s: %s", displayID, task.Title)
		body := buildBody(task, displayID, reporterEmail)
		if err := email.SendPlain(detachedCtx, cfg.SMTP, subject, body, assigneeEmail); err != nil {
			log.Warn("task notify: email not sent",
				slog.String("task_id", task.TaskId),
				slog.String("assignee", assigneeEmail),
				slog.Any("err", err),
			)
		} else {
			log.Info("task notify: email sent",
				slog.String("task_id", task.TaskId),
				slog.String("assignee", assigneeEmail),
			)
		}
	}()
}

func buildBody(t *taskv1.Task, displayID, reporterEmail string) string {
	priority := strings.TrimPrefix(t.Priority.String(), "TASK_PRIORITY_")
	taskType := strings.TrimPrefix(t.TaskType.String(), "TASK_TYPE_")
	taskStatus := strings.ReplaceAll(strings.TrimPrefix(t.Status.String(), "TASK_STATUS_"), "_", " ")

	var sb strings.Builder
	sb.WriteString("You have been assigned a task in JARVIS.\n\n")
	fmt.Fprintf(&sb, "TASK:         %s\n", displayID)
	fmt.Fprintf(&sb, "TITLE:        %s\n", t.Title)
	fmt.Fprintf(&sb, "STATUS:       %s\n", taskStatus)
	fmt.Fprintf(&sb, "PRIORITY:     %s\n", priority)
	fmt.Fprintf(&sb, "TYPE:         %s\n", taskType)
	if t.StoryPoints > 0 {
		fmt.Fprintf(&sb, "STORY POINTS: %d\n", t.StoryPoints)
	}
	if t.Description != "" {
		fmt.Fprintf(&sb, "\nDESCRIPTION:\n%s\n", t.Description)
	}
	if t.DueDate != "" {
		fmt.Fprintf(&sb, "\nDUE DATE: %s\n", t.DueDate)
	}
	if reporterEmail != "" {
		fmt.Fprintf(&sb, "\nAssigned by: %s\n", reporterEmail)
	}
	sb.WriteString("\n---\nThis is an automated notification from JARVIS.\n")
	return sb.String()
}
