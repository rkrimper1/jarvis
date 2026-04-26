package threatstore_test

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	"github.com/rkrimper1/jarvis/api/internal/security/threatstore"
)

func newTestStore(t *testing.T) *threatstore.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := threatstore.New(db)
	if err != nil {
		t.Fatalf("threatstore.New: %v", err)
	}
	return s
}

func sampleEvent(overrides ...func(*securityv1.ThreatEvent)) *securityv1.ThreatEvent {
	e := &securityv1.ThreatEvent{
		CameraLabel:        "Front Door",
		DetectedObjects:    []string{"person(0.92)", "knife(0.81)"},
		Level:              securityv1.ThreatLevel_THREAT_LEVEL_HIGH,
		Confidence:         0.87,
		ThreatSummary:      "Armed individual at entrance.",
		RecommendedActions: []string{"Lock down", "Alert security"},
		ImageUrl:           "/threat-events/img-001.jpg",
	}
	for _, fn := range overrides {
		fn(e)
	}
	return e
}

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_CreatesTableOnFreshDB(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	s, err := threatstore.New(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestNew_Idempotent(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := threatstore.New(db); err != nil {
		t.Fatalf("first New: %v", err)
	}
	// calling New again on the same DB must not error (CREATE TABLE IF NOT EXISTS)
	if _, err := threatstore.New(db); err != nil {
		t.Errorf("second New: %v", err)
	}
}

// ── Log ───────────────────────────────────────────────────────────────────────

func TestLog_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.Log(ctx, sampleEvent())
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if saved.EventId == "" {
		t.Error("expected non-empty EventId")
	}
	if saved.Timestamp == nil {
		t.Error("expected non-nil Timestamp")
	}
	if saved.CameraLabel != "Front Door" {
		t.Errorf("camera_label = %q, want %q", saved.CameraLabel, "Front Door")
	}
	if saved.Level != securityv1.ThreatLevel_THREAT_LEVEL_HIGH {
		t.Errorf("level = %v, want HIGH", saved.Level)
	}
	if len(saved.DetectedObjects) != 2 {
		t.Errorf("detected_objects len = %d, want 2", len(saved.DetectedObjects))
	}
	if len(saved.RecommendedActions) != 2 {
		t.Errorf("recommended_actions len = %d, want 2", len(saved.RecommendedActions))
	}
}

func TestLog_GeneratesUniqueIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a, err := s.Log(ctx, sampleEvent())
	if err != nil {
		t.Fatalf("first Log: %v", err)
	}
	b, err := s.Log(ctx, sampleEvent())
	if err != nil {
		t.Fatalf("second Log: %v", err)
	}
	if a.EventId == b.EventId {
		t.Error("expected distinct EventIds for separate log calls")
	}
}

func TestLog_EmptySlices(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.Log(ctx, sampleEvent(func(e *securityv1.ThreatEvent) {
		e.DetectedObjects = nil
		e.RecommendedActions = nil
	}))
	if err != nil {
		t.Fatalf("Log with nil slices: %v", err)
	}
	if saved.EventId == "" {
		t.Error("expected non-empty EventId")
	}
}

func TestLog_NoImageURL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.Log(ctx, sampleEvent(func(e *securityv1.ThreatEvent) {
		e.ImageUrl = ""
	}))
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if saved.ImageUrl != "" {
		t.Errorf("image_url = %q, want empty", saved.ImageUrl)
	}
}

func TestLog_AllThreatLevels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	levels := []securityv1.ThreatLevel{
		securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED,
		securityv1.ThreatLevel_THREAT_LEVEL_LOW,
		securityv1.ThreatLevel_THREAT_LEVEL_MODERATE,
		securityv1.ThreatLevel_THREAT_LEVEL_HIGH,
		securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL,
	}
	for _, lvl := range levels {
		saved, err := s.Log(ctx, sampleEvent(func(e *securityv1.ThreatEvent) { e.Level = lvl }))
		if err != nil {
			t.Errorf("Log level %v: %v", lvl, err)
			continue
		}
		if saved.Level != lvl {
			t.Errorf("level roundtrip: got %v, want %v", saved.Level, lvl)
		}
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList_EmptyStore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	events, err := s.List(ctx, 10)
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestList_ReturnsLoggedEvent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Log(ctx, sampleEvent()); err != nil {
		t.Fatalf("Log: %v", err)
	}

	events, err := s.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].CameraLabel != "Front Door" {
		t.Errorf("camera_label = %q", events[0].CameraLabel)
	}
	if events[0].Level != securityv1.ThreatLevel_THREAT_LEVEL_HIGH {
		t.Errorf("level = %v", events[0].Level)
	}
}

func TestList_RespectsPageSize(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Log(ctx, sampleEvent()); err != nil {
			t.Fatalf("Log %d: %v", i, err)
		}
	}

	events, err := s.List(ctx, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events with page_size=3, got %d", len(events))
	}
}

func TestList_ZeroPageSizeDefaultsTo20(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Log(ctx, sampleEvent()); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// page_size=0 should clamp to 20; all 5 events returned
	events, err := s.List(ctx, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("expected 5 events with page_size=0 (clamp to 20), got %d", len(events))
	}
}

func TestList_OverLimitPageSizeDefaultsTo20(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := s.Log(ctx, sampleEvent()); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	// page_size=999 > 100 should clamp to 20
	events, err := s.List(ctx, 999)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("expected all 5 events (clamped page_size), got %d", len(events))
	}
}

func TestList_RoundtripsFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	orig := sampleEvent()
	if _, err := s.Log(ctx, orig); err != nil {
		t.Fatalf("Log: %v", err)
	}

	events, err := s.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event")
	}
	e := events[0]
	if e.ThreatSummary != orig.ThreatSummary {
		t.Errorf("threat_summary = %q, want %q", e.ThreatSummary, orig.ThreatSummary)
	}
	if e.ImageUrl != orig.ImageUrl {
		t.Errorf("image_url = %q, want %q", e.ImageUrl, orig.ImageUrl)
	}
	if len(e.RecommendedActions) != len(orig.RecommendedActions) {
		t.Errorf("recommended_actions len = %d, want %d", len(e.RecommendedActions), len(orig.RecommendedActions))
	}
}
