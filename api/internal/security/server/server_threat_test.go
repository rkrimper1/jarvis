package server_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	"github.com/rkrimper1/jarvis/api/internal/security/config"
	"github.com/rkrimper1/jarvis/api/internal/security/server"
	"google.golang.org/grpc/codes"
)

// newTestServerWithDB constructs a SecurityServer backed by an in-memory SQLite DB
// so that threatstore, analyticsstore, and audit store are fully wired.
func newTestServerWithDB(t *testing.T, face server.FaceConfig) *server.SecurityServer {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	cfg := &config.Config{
		Token: config.TokenConfig{
			Secret:         "test-secret",
			AccessTokenTTL: time.Hour,
			Issuer:         "jarvis.test",
		},
		Audit: config.AuditConfig{MaxPageSize: 100},
		Threat: config.ThreatConfig{
			AlertBroadcastInterval: 24 * time.Hour,
			ConfidenceThresh:       0.65,
		},
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.NewWithUserStoreFace(cfg, log, nil, db, face)
}

func sampleLogRequest(overrides ...func(*securityv1.LogThreatEventRequest)) *securityv1.LogThreatEventRequest {
	r := &securityv1.LogThreatEventRequest{
		Meta:               &commonv1.RequestMeta{RequestId: "log-001"},
		CameraLabel:        "Front Door",
		DetectedObjects:    []string{"person(0.92)"},
		Level:              securityv1.ThreatLevel_THREAT_LEVEL_HIGH,
		Confidence:         0.87,
		ThreatSummary:      "Armed individual detected.",
		RecommendedActions: []string{"Lock down"},
	}
	for _, fn := range overrides {
		fn(r)
	}
	return r
}

// ── AnalyzeThreatScene ────────────────────────────────────────────────────────

func TestAnalyzeThreatScene_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AnalyzeThreatScene(context.Background(), &securityv1.AnalyzeThreatSceneRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAnalyzeThreatScene_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AnalyzeThreatScene(context.Background(), &securityv1.AnalyzeThreatSceneRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAnalyzeThreatScene_DisabledReturnsUnspecified(t *testing.T) {
	// newTestServer has THREAT_VISION_ENABLED=false → sceneAnalyzer == nil
	s := newTestServer(t)
	resp, err := s.AnalyzeThreatScene(context.Background(), &securityv1.AnalyzeThreatSceneRequest{
		Meta:      metaFor("scene-001"),
		ImageData: []byte{0xFF, 0xD8, 0xFF},
	})
	if err != nil {
		t.Fatalf("expected success response when disabled, got error: %v", err)
	}
	if resp.Level != securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED {
		t.Errorf("Level = %v, want UNSPECIFIED when disabled", resp.Level)
	}
	if resp.ThreatSummary == "" {
		t.Error("expected non-empty ThreatSummary in disabled response")
	}
}

func TestAnalyzeThreatScene_DisabledIncludesLogMode(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{
		ThreatVisionEnabled: false,
		ThreatLogMode:       "auto",
	})
	resp, err := s.AnalyzeThreatScene(context.Background(), &securityv1.AnalyzeThreatSceneRequest{
		Meta: metaFor("scene-mode"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.LogMode != "auto" {
		t.Errorf("LogMode = %q, want 'auto'", resp.LogMode)
	}
}

func TestAnalyzeThreatScene_EnabledEmptyImageData(t *testing.T) {
	// ThreatVisionEnabled=true but no API key → sceneAnalyzer == nil (key missing)
	// so the handler returns the disabled response, not an error for empty image.
	// To hit the image_data validation, we need a server with sceneAnalyzer wired,
	// which requires a real API key — skip that path; test the guard instead.
	s := newTestServerWithDB(t, server.FaceConfig{
		ThreatVisionEnabled: true,
		// AnthropicKey intentionally empty — analyzer won't be wired
	})
	resp, err := s.AnalyzeThreatScene(context.Background(), &securityv1.AnalyzeThreatSceneRequest{
		Meta: metaFor("scene-noimage"),
	})
	// No API key → disabled path → success with UNSPECIFIED
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Level != securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED {
		t.Errorf("Level = %v, want UNSPECIFIED", resp.Level)
	}
}

// ── LogThreatEvent ────────────────────────────────────────────────────────────

func TestLogThreatEvent_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.LogThreatEvent(context.Background(), &securityv1.LogThreatEventRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestLogThreatEvent_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.LogThreatEvent(context.Background(), &securityv1.LogThreatEventRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestLogThreatEvent_NoStore_ReturnsNotLogged(t *testing.T) {
	// newTestServer has no DB → threatStore == nil → Logged=false
	s := newTestServer(t)
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = true },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Logged {
		t.Error("expected Logged=false when no threat store is configured")
	}
}

func TestLogThreatEvent_ManualMode_ForceTrue_Logs(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "manual"})
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = true },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Logged {
		t.Error("expected Logged=true with force=true in manual mode")
	}
	if resp.Event == nil {
		t.Fatal("expected non-nil Event")
	}
	if resp.Event.EventId == "" {
		t.Error("expected non-empty EventId")
	}
}

func TestLogThreatEvent_ManualMode_ForceFalse_SkipsLog(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "manual"})
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = false },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Logged {
		t.Error("expected Logged=false with force=false in manual mode")
	}
}

func TestLogThreatEvent_AutoMode_AlwaysLogs(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "auto"})
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = false },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Logged {
		t.Error("expected Logged=true in auto mode even with force=false")
	}
}

func TestLogThreatEvent_AllMode_AlwaysLogs(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "all"})
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = false },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Logged {
		t.Error("expected Logged=true in all mode")
	}
}

func TestLogThreatEvent_DefaultMode_IsManual(t *testing.T) {
	// Empty ThreatLogMode → server defaults to "manual"
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: ""})
	resp, err := s.LogThreatEvent(context.Background(), sampleLogRequest(
		func(r *securityv1.LogThreatEventRequest) { r.Force = false },
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Logged {
		t.Error("expected Logged=false when ThreatLogMode is empty (defaults to manual) and force=false")
	}
}

func TestLogThreatEvent_RoundtripsEventFields(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "auto"})
	req := sampleLogRequest()
	resp, err := s.LogThreatEvent(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := resp.Event
	if e.CameraLabel != req.CameraLabel {
		t.Errorf("CameraLabel = %q, want %q", e.CameraLabel, req.CameraLabel)
	}
	if e.Level != req.Level {
		t.Errorf("Level = %v, want %v", e.Level, req.Level)
	}
	if e.ThreatSummary != req.ThreatSummary {
		t.Errorf("ThreatSummary = %q, want %q", e.ThreatSummary, req.ThreatSummary)
	}
	if e.Timestamp == nil {
		t.Error("expected non-nil Timestamp on returned event")
	}
}

// ── ListThreatEvents ──────────────────────────────────────────────────────────

func TestListThreatEvents_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ListThreatEvents(context.Background(), &securityv1.ListThreatEventsRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestListThreatEvents_NoStore_ReturnsEmpty(t *testing.T) {
	// newTestServer has no DB → threatStore == nil → empty list, no error
	s := newTestServer(t)
	resp, err := s.ListThreatEvents(context.Background(), &securityv1.ListThreatEventsRequest{
		Meta: metaFor("list-nostore"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Errorf("expected 0 events with no store, got %d", len(resp.Events))
	}
}

func TestListThreatEvents_ReturnsLoggedEvents(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "auto"})
	ctx := context.Background()

	// Log two events
	for i := 0; i < 2; i++ {
		if _, err := s.LogThreatEvent(ctx, sampleLogRequest()); err != nil {
			t.Fatalf("LogThreatEvent %d: %v", i, err)
		}
	}

	resp, err := s.ListThreatEvents(ctx, &securityv1.ListThreatEventsRequest{
		Meta:     metaFor("list-001"),
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListThreatEvents: %v", err)
	}
	if len(resp.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(resp.Events))
	}
}

func TestListThreatEvents_RespectsPageSize(t *testing.T) {
	s := newTestServerWithDB(t, server.FaceConfig{ThreatLogMode: "auto"})
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.LogThreatEvent(ctx, sampleLogRequest(func(r *securityv1.LogThreatEventRequest) {
			r.Meta = &commonv1.RequestMeta{RequestId: "log-setup"}
		})); err != nil {
			t.Fatalf("LogThreatEvent %d: %v", i, err)
		}
	}

	resp, err := s.ListThreatEvents(ctx, &securityv1.ListThreatEventsRequest{
		Meta:     metaFor("list-page"),
		PageSize: 3,
	})
	if err != nil {
		t.Fatalf("ListThreatEvents: %v", err)
	}
	if len(resp.Events) != 3 {
		t.Errorf("expected 3 events with page_size=3, got %d", len(resp.Events))
	}
}
