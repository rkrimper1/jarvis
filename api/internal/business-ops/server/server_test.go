package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/business-ops/server"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) *server.BusinessOpsServer {
	t.Helper()
	return server.New(discardLogger())
}

func metaFor(id string) *commonv1.RequestMeta { return &commonv1.RequestMeta{RequestId: id} }

// ── validateMeta shared behaviour ────────────────────────────────────────────

func TestScheduleEvent_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestScheduleEvent_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

// ── ScheduleEvent ─────────────────────────────────────────────────────────────

func TestScheduleEvent_MissingTitle(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{
		Meta: metaFor("sched-001"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestScheduleEvent_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{
		Meta:      metaFor("sched-002"),
		Title:     "Quarterly Review",
		Attendees: []string{"alice@example.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EventId == "" {
		t.Error("expected non-empty EventId")
	}
	if !resp.Meta.GetSuccess() {
		t.Error("expected Meta.Success = true")
	}
}

func TestScheduleEvent_HighPriority(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{
		Meta:         metaFor("sched-003"),
		Title:        "Urgent Sync",
		HighPriority: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EventId == "" {
		t.Error("expected non-empty EventId")
	}
}

// ── GetSchedule ───────────────────────────────────────────────────────────────

func TestGetSchedule_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GetSchedule(context.Background(), &businessv1.GetScheduleRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGetSchedule_HappyPath(t *testing.T) {
	s := newTestServer(t)
	// Schedule an event first so there is data to list.
	_, err := s.ScheduleEvent(context.Background(), &businessv1.ScheduleEventRequest{
		Meta:      metaFor("gs-setup"),
		Title:     "Standup",
		Attendees: []string{"bob@example.com"},
	})
	if err != nil {
		t.Fatalf("setup schedule: %v", err)
	}

	resp, err := s.GetSchedule(context.Background(), &businessv1.GetScheduleRequest{
		Meta:      metaFor("gs-001"),
		SubjectId: "bob@example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta")
	}
}

// ── CreateTask ────────────────────────────────────────────────────────────────

func TestCreateTask_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.CreateTask(context.Background(), &businessv1.CreateTaskRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateTask_MissingTitle(t *testing.T) {
	s := newTestServer(t)
	_, err := s.CreateTask(context.Background(), &businessv1.CreateTaskRequest{
		Meta: metaFor("ct-bad"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestCreateTask_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.CreateTask(context.Background(), &businessv1.CreateTaskRequest{
		Meta:       metaFor("ct-001"),
		Title:      "Prepare briefing",
		AssigneeId: "alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.TaskId == "" {
		t.Error("expected non-empty TaskId")
	}
	if resp.Status != businessv1.TaskStatus_TASK_STATUS_PENDING {
		t.Errorf("status = %v, want PENDING", resp.Status)
	}
}

// ── SendMessage ───────────────────────────────────────────────────────────────

func TestSendMessage_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SendMessage(context.Background(), &businessv1.SendMessageRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSendMessage_NoRecipients(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SendMessage(context.Background(), &businessv1.SendMessageRequest{
		Meta: metaFor("sm-bad"),
		Body: "hello",
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSendMessage_EmptyBody(t *testing.T) {
	s := newTestServer(t)
	_, err := s.SendMessage(context.Background(), &businessv1.SendMessageRequest{
		Meta:       metaFor("sm-bad2"),
		Recipients: []string{"alice"},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestSendMessage_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.SendMessage(context.Background(), &businessv1.SendMessageRequest{
		Meta:       metaFor("sm-001"),
		Recipients: []string{"alice", "bob"},
		Channel:    businessv1.MessageChannel_MESSAGE_CHANNEL_EMAIL,
		Subject:    "Status update",
		Body:       "Everything is on track.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.MessageId == "" {
		t.Error("expected non-empty MessageId")
	}
}

// ── GenerateReport ────────────────────────────────────────────────────────────

func TestGenerateReport_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GenerateReport(context.Background(), &businessv1.GenerateReportRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGenerateReport_MissingType(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GenerateReport(context.Background(), &businessv1.GenerateReportRequest{
		Meta: metaFor("gr-bad"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGenerateReport_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.GenerateReport(context.Background(), &businessv1.GenerateReportRequest{
		Meta:       metaFor("gr-001"),
		ReportType: "status_summary",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ReportId == "" {
		t.Error("expected non-empty ReportId")
	}
	if resp.Summary == "" {
		t.Error("expected non-empty Summary")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != want {
		t.Errorf("code = %v, want %v (message: %q)", st.Code(), want, st.Message())
	}
}
