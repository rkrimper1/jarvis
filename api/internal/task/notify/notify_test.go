package notify_test

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/integrations/email"
	"github.com/rkrimper1/jarvis/api/internal/task/notify"
	taskv1 "github.com/rkrimper1/jarvis/api/pb/task"
)

// ── fake SMTP ──────────────────────────────────────────────────────────────

// smtpResult holds what the fake server captured from one mail transaction.
type smtpResult struct {
	subject string
	body    string
	to      string
}

// fakeSMTP starts a minimal SMTP listener on a random loopback port.
// It accepts exactly one connection, captures the DATA payload, and sends
// the result on the returned channel. The listener is closed on t.Cleanup.
func fakeSMTP(t *testing.T) (host, port string, ch <-chan smtpResult) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeSMTP listen: %v", err)
	}
	out := make(chan smtpResult, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		out <- handleSMTP(conn)
	}()
	t.Cleanup(func() { ln.Close() })
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p, out
}

// failSMTP starts a server that immediately closes every connection,
// simulating a dial/transport error. Used to verify graceful failure.
func failSMTP(t *testing.T) (host, port string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failSMTP listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close() // hang up immediately
		}
	}()
	t.Cleanup(func() { ln.Close() })
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p
}

// handleSMTP drives the minimal SMTP exchange and returns what was received.
func handleSMTP(conn net.Conn) smtpResult {
	r := bufio.NewReader(conn)
	writeln := func(s string) { fmt.Fprint(conn, s+"\r\n") }

	writeln("220 localhost ESMTP")

	var raw strings.Builder
	var toAddr string
	inData := false

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case strings.HasPrefix(strings.ToUpper(line), "EHLO"),
			strings.HasPrefix(strings.ToUpper(line), "HELO"):
			fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n")

		case strings.HasPrefix(strings.ToUpper(line), "AUTH PLAIN"):
			writeln("235 Authentication successful")

		case strings.HasPrefix(strings.ToUpper(line), "MAIL FROM"):
			writeln("250 OK")

		case strings.HasPrefix(strings.ToUpper(line), "RCPT TO"):
			// extract address between < >
			start := strings.Index(line, "<")
			end := strings.Index(line, ">")
			if start >= 0 && end > start {
				toAddr = line[start+1 : end]
			}
			writeln("250 OK")

		case strings.ToUpper(line) == "DATA":
			writeln("354 Start mail input; end with <CRLF>.<CRLF>")
			inData = true

		case inData && line == ".":
			writeln("250 OK")
			inData = false

		case inData:
			raw.WriteString(line + "\n")

		case strings.HasPrefix(strings.ToUpper(line), "QUIT"):
			writeln("221 Bye")
			goto done
		}
	}
done:
	full := raw.String()
	subject := extractHeader(full, "Subject")
	// body starts after the blank line separating headers from body
	body := ""
	if idx := strings.Index(full, "\n\n"); idx >= 0 {
		body = full[idx+2:]
	}
	return smtpResult{subject: subject, body: body, to: toAddr}
}

func extractHeader(raw, name string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, name+":") {
			return strings.TrimSpace(strings.TrimPrefix(line, name+":"))
		}
	}
	return ""
}

// ── helpers ────────────────────────────────────────────────────────────────

func newNotifier(t *testing.T, host, port string, notifySelf bool) *notify.Notifier {
	t.Helper()
	cfg := notify.Config{
		SMTP: email.Config{
			Host:      host,
			Port:      port,
			User:      "jarvis@test.local",
			Pass:      "secret",
			Organizer: "org@test.local",
		},
		NotifySelf: notifySelf,
	}
	return notify.New(cfg, slog.Default())
}

func basicTask(displayID int32, title string) *taskv1.Task {
	return &taskv1.Task{
		TaskId:    "task-001",
		DisplayId: displayID,
		Title:     title,
		Status:    taskv1.TaskStatus_TASK_STATUS_ASSIGNED,
		Priority:  taskv1.TaskPriority_TASK_PRIORITY_HIGH,
		TaskType:  taskv1.TaskType_TASK_TYPE_TASK,
	}
}

func mustReceive(t *testing.T, ch <-chan smtpResult, timeout time.Duration) smtpResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(timeout):
		t.Fatal("timed out waiting for email")
		return smtpResult{}
	}
}

// ── New ────────────────────────────────────────────────────────────────────

func TestNew(t *testing.T) {
	n := notify.New(notify.Config{}, slog.Default())
	if n == nil {
		t.Fatal("New returned nil")
	}
}

// ── SendAssignment — synchronous early-exit paths ──────────────────────────

func TestSendAssignment_EmptyAssignee(t *testing.T) {
	// empty assigneeEmail must be a no-op; no goroutine, no panic
	n := notify.New(notify.Config{}, slog.Default())
	n.SendAssignment(context.Background(), basicTask(1, "Test"), "", "reporter@test.local")
	// If the goroutine fired and tried to dial with zero host it would panic/log;
	// the lack of error/panic here is the assertion.
}

func TestSendAssignment_SelfAssignSkipped(t *testing.T) {
	// NotifySelf=false + same email → skip, no goroutine
	n := notify.New(notify.Config{NotifySelf: false}, slog.Default())
	n.SendAssignment(context.Background(), basicTask(1, "Test"),
		"same@test.local", "same@test.local")
}

func TestSendAssignment_SelfAssignAllowed_WhenNotifySelfTrue(t *testing.T) {
	host, port, ch := fakeSMTP(t)
	n := newNotifier(t, host, port, true)
	n.SendAssignment(context.Background(), basicTask(42, "Self task"),
		"self@test.local", "self@test.local")
	r := mustReceive(t, ch, 5*time.Second)
	if !strings.Contains(r.subject, "JARVIS-0042") {
		t.Errorf("subject %q missing task ID", r.subject)
	}
}

// ── SendAssignment — happy path ────────────────────────────────────────────

func TestSendAssignment_SubjectFormat(t *testing.T) {
	host, port, ch := fakeSMTP(t)
	n := newNotifier(t, host, port, false)
	n.SendAssignment(context.Background(), basicTask(7, "Fix login bug"),
		"dev@test.local", "lead@test.local")
	r := mustReceive(t, ch, 5*time.Second)
	want := "JARVIS-0007: Fix login bug"
	if r.subject != want {
		t.Errorf("subject = %q, want %q", r.subject, want)
	}
}

func TestSendAssignment_RecipientAddress(t *testing.T) {
	host, port, ch := fakeSMTP(t)
	n := newNotifier(t, host, port, false)
	n.SendAssignment(context.Background(), basicTask(1, "Task"),
		"assignee@test.local", "reporter@test.local")
	r := mustReceive(t, ch, 5*time.Second)
	if r.to != "assignee@test.local" {
		t.Errorf("RCPT TO = %q, want assignee@test.local", r.to)
	}
}

func TestSendAssignment_DisplayIdPadding(t *testing.T) {
	tests := []struct {
		id   int32
		want string
	}{
		{0, "JARVIS-0000"},
		{1, "JARVIS-0001"},
		{99, "JARVIS-0099"},
		{1000, "JARVIS-1000"},
		{9999, "JARVIS-9999"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			host, port, ch := fakeSMTP(t)
			n := newNotifier(t, host, port, false)
			n.SendAssignment(context.Background(), basicTask(tc.id, "X"),
				"a@test.local", "b@test.local")
			r := mustReceive(t, ch, 5*time.Second)
			if !strings.HasPrefix(r.subject, tc.want) {
				t.Errorf("subject = %q, want prefix %q", r.subject, tc.want)
			}
		})
	}
}

// ── SendAssignment — body content ──────────────────────────────────────────

func TestSendAssignment_BodyContainsTaskFields(t *testing.T) {
	host, port, ch := fakeSMTP(t)
	n := newNotifier(t, host, port, false)
	task := &taskv1.Task{
		TaskId:      "t-42",
		DisplayId:   42,
		Title:       "Refactor auth",
		Status:      taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS,
		Priority:    taskv1.TaskPriority_TASK_PRIORITY_CRITICAL,
		TaskType:    taskv1.TaskType_TASK_TYPE_BUG,
		StoryPoints: 5,
		Description: "Needs full rewrite.",
		DueDate:     "2026-05-01",
	}
	n.SendAssignment(context.Background(), task, "dev@test.local", "lead@test.local")
	r := mustReceive(t, ch, 5*time.Second)

	checks := []struct {
		label string
		want  string
	}{
		{"header line", "You have been assigned a task in JARVIS."},
		{"task ID", "JARVIS-0042"},
		{"title", "Refactor auth"},
		{"status formatted", "IN PROGRESS"},    // TASK_STATUS_IN_PROGRESS → IN PROGRESS
		{"priority trimmed", "CRITICAL"},        // TASK_PRIORITY_CRITICAL → CRITICAL
		{"type trimmed", "BUG"},                 // TASK_TYPE_BUG → BUG
		{"story points", "STORY POINTS: 5"},
		{"description label", "DESCRIPTION:"},
		{"description body", "Needs full rewrite."},
		{"due date", "DUE DATE: 2026-05-01"},
		{"assigned by", "lead@test.local"},
		{"footer", "automated notification from JARVIS"},
	}
	for _, c := range checks {
		if !strings.Contains(r.body, c.want) {
			t.Errorf("body missing %s: want %q in:\n%s", c.label, c.want, r.body)
		}
	}
}

func TestSendAssignment_BodyOmitsOptionalFields_WhenEmpty(t *testing.T) {
	host, port, ch := fakeSMTP(t)
	n := newNotifier(t, host, port, false)
	task := &taskv1.Task{
		TaskId:    "t-1",
		DisplayId: 1,
		Title:     "Minimal task",
		Status:    taskv1.TaskStatus_TASK_STATUS_UNASSIGNED,
		Priority:  taskv1.TaskPriority_TASK_PRIORITY_LOW,
		TaskType:  taskv1.TaskType_TASK_TYPE_TASK,
		// StoryPoints=0, Description="", DueDate=""
	}
	// reporterEmail="" → no "Assigned by" line
	n.SendAssignment(context.Background(), task, "a@test.local", "")
	r := mustReceive(t, ch, 5*time.Second)

	if strings.Contains(r.body, "STORY POINTS") {
		t.Errorf("body should not contain STORY POINTS when 0")
	}
	if strings.Contains(r.body, "DESCRIPTION") {
		t.Errorf("body should not contain DESCRIPTION when empty")
	}
	if strings.Contains(r.body, "DUE DATE") {
		t.Errorf("body should not contain DUE DATE when empty")
	}
	if strings.Contains(r.body, "Assigned by") {
		t.Errorf("body should not contain 'Assigned by' when reporterEmail is empty")
	}
}

func TestSendAssignment_StatusFormatting(t *testing.T) {
	tests := []struct {
		status taskv1.TaskStatus
		want   string
	}{
		{taskv1.TaskStatus_TASK_STATUS_UNASSIGNED, "UNASSIGNED"},
		{taskv1.TaskStatus_TASK_STATUS_IN_PROGRESS, "IN PROGRESS"},
		{taskv1.TaskStatus_TASK_STATUS_COMPLETED, "COMPLETED"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			host, port, ch := fakeSMTP(t)
			n := newNotifier(t, host, port, false)
			task := basicTask(1, "T")
			task.Status = tc.status
			n.SendAssignment(context.Background(), task, "a@test.local", "b@test.local")
			r := mustReceive(t, ch, 5*time.Second)
			if !strings.Contains(r.body, tc.want) {
				t.Errorf("body missing status %q", tc.want)
			}
		})
	}
}

// ── SendAssignment — failure path ──────────────────────────────────────────

func TestSendAssignment_SMTPFailure_NoPanic(t *testing.T) {
	// Server closes connection immediately — SendPlain returns error.
	// The goroutine must log a warning and exit cleanly; no panic.
	host, port := failSMTP(t)
	n := newNotifier(t, host, port, false)
	n.SendAssignment(context.Background(), basicTask(1, "Task"),
		"dev@test.local", "lead@test.local")
	// Give the goroutine time to attempt and fail.
	time.Sleep(200 * time.Millisecond)
	// If we reach here without panic the test passes.
}
