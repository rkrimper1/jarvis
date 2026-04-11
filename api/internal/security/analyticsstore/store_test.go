package analyticsstore_test

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rkrimper1/jarvis/api/internal/security/analyticsstore"
)

// newTestStore opens an in-memory SQLite-backed Store for each test.
func newTestStore(t *testing.T) *analyticsstore.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := analyticsstore.New(db, slog.Default())
	if err != nil {
		t.Fatalf("analyticsstore.New: %v", err)
	}
	return s
}

// ── ComputeStatus — empty store ───────────────────────────────────────

func TestComputeStatus_EmptyDB(t *testing.T) {
	s := newTestStore(t)
	got, err := s.ComputeStatus(context.Background())
	if err != nil {
		t.Fatalf("ComputeStatus: %v", err)
	}
	if got.Score != 0 || got.Color != "GREEN" || got.Status != "NOMINAL" {
		t.Errorf("empty store: got {%.1f %s %s}, want {0 GREEN NOMINAL}", got.Score, got.Color, got.Status)
	}
}

// ── Threat level → score → color/status ──────────────────────────────

func TestComputeStatus_ThreatHigh(t *testing.T) {
	s := newTestStore(t)
	mustInsertThreat(t, s, "THREAT_LEVEL_HIGH")

	got := mustComputeStatus(t, s)
	if got.Score != 100 {
		t.Errorf("HIGH threat: score = %.1f, want 100", got.Score)
	}
	if got.Color != "RED" {
		t.Errorf("HIGH threat: color = %s, want RED", got.Color)
	}
	if got.Status != "COMPROMISED" {
		t.Errorf("HIGH threat: status = %s, want COMPROMISED", got.Status)
	}
}

func TestComputeStatus_ThreatCritical(t *testing.T) {
	s := newTestStore(t)
	mustInsertThreat(t, s, "THREAT_LEVEL_CRITICAL")

	got := mustComputeStatus(t, s)
	if got.Score != 100 {
		t.Errorf("CRITICAL threat: score = %.1f, want 100", got.Score)
	}
}

func TestComputeStatus_ThreatModerate(t *testing.T) {
	s := newTestStore(t)
	mustInsertThreat(t, s, "THREAT_LEVEL_MODERATE")

	got := mustComputeStatus(t, s)
	if got.Score != 50 {
		t.Errorf("MODERATE threat: score = %.1f, want 50", got.Score)
	}
	if got.Color != "YELLOW" {
		t.Errorf("MODERATE threat: color = %s, want YELLOW", got.Color)
	}
	if got.Status != "COMPROMISED" {
		t.Errorf("MODERATE threat: status = %s, want COMPROMISED", got.Status)
	}
}

func TestComputeStatus_ThreatLow(t *testing.T) {
	s := newTestStore(t)
	mustInsertThreat(t, s, "THREAT_LEVEL_LOW")

	got := mustComputeStatus(t, s)
	if got.Score != 10 {
		t.Errorf("LOW threat: score = %.1f, want 10", got.Score)
	}
	if got.Color != "GREEN" {
		t.Errorf("LOW threat: color = %s, want GREEN", got.Color)
	}
	if got.Status != "NOMINAL" {
		t.Errorf("LOW threat: status = %s, want NOMINAL", got.Status)
	}
}

func TestComputeStatus_ThreatUnknown(t *testing.T) {
	s := newTestStore(t)
	mustInsertThreat(t, s, "THREAT_LEVEL_UNSPECIFIED")

	got := mustComputeStatus(t, s)
	if got.Score != 0 {
		t.Errorf("UNSPECIFIED threat: score = %.1f, want 0", got.Score)
	}
}

// ── Face sentiment → score ────────────────────────────────────────────

func TestComputeStatus_FacesHappy(t *testing.T) {
	s := newTestStore(t)
	mustInsertFaces(t, s, []string{"happy"})

	got := mustComputeStatus(t, s)
	if got.Score != 10 {
		t.Errorf("happy face: score = %.1f, want 10", got.Score)
	}
	if got.Color != "GREEN" {
		t.Errorf("happy face: color = %s, want GREEN", got.Color)
	}
	if got.Status != "NOMINAL" {
		t.Errorf("happy face: status = %s, want NOMINAL", got.Status)
	}
}

func TestComputeStatus_FacesAngry(t *testing.T) {
	s := newTestStore(t)
	mustInsertFaces(t, s, []string{"angry"})

	got := mustComputeStatus(t, s)
	if got.Score != 90 {
		t.Errorf("angry face: score = %.1f, want 90", got.Score)
	}
	if got.Color != "RED" {
		t.Errorf("angry face: color = %s, want RED", got.Color)
	}
}

func TestComputeStatus_FacesUnknownSentiment(t *testing.T) {
	s := newTestStore(t)
	mustInsertFaces(t, s, []string{"perplexed"}) // not in the map → 30

	got := mustComputeStatus(t, s)
	if got.Score != 30 {
		t.Errorf("unknown sentiment: score = %.1f, want 30", got.Score)
	}
}

func TestComputeStatus_FacesAveragedAcrossFaces(t *testing.T) {
	s := newTestStore(t)
	// One face event with happy (10) + angry (90) → avg = 50
	mustInsertFaces(t, s, []string{"happy", "angry"})

	got := mustComputeStatus(t, s)
	if got.Score != 50 {
		t.Errorf("mixed faces avg: score = %.1f, want 50", got.Score)
	}
}

// ── Weighted mixing (70% threat, 30% faces) ───────────────────────────

func TestComputeStatus_WeightedMix(t *testing.T) {
	s := newTestStore(t)
	// HIGH threat (100) + happy face (10) → 0.7*100 + 0.3*10 = 73.0
	mustInsertThreat(t, s, "THREAT_LEVEL_HIGH")
	mustInsertFaces(t, s, []string{"happy"})

	got := mustComputeStatus(t, s)
	if got.Score != 73.0 {
		t.Errorf("mixed: score = %.1f, want 73.0", got.Score)
	}
	if got.Color != "RED" {
		t.Errorf("mixed: color = %s, want RED", got.Color)
	}
	if got.Status != "COMPROMISED" {
		t.Errorf("mixed: status = %s, want COMPROMISED", got.Status)
	}
}

func TestComputeStatus_WeightedMix_ThreatOnly_NoFaces(t *testing.T) {
	s := newTestStore(t)
	// With no face events, threat score is used at full weight (not 70%)
	mustInsertThreat(t, s, "THREAT_LEVEL_MODERATE")

	got := mustComputeStatus(t, s)
	if got.Score != 50 {
		t.Errorf("threat-only weight: score = %.1f, want 50 (not 35)", got.Score)
	}
}

// ── Score color/status band boundaries ───────────────────────────────

func TestComputeStatus_ScoreBands(t *testing.T) {
	// score 20 → GREEN NOMINAL
	// score 21 → YELLOW NOMINAL
	// score 40 → YELLOW COMPROMISED
	// score 70 → YELLOW COMPROMISED
	// score 71 → RED COMPROMISED
	// We drive this via LOW (10) + happy faces (10) = 0.7*10+0.3*10 = 10 (GREEN)
	// and MODERATE (50) alone = YELLOW COMPROMISED
	// and HIGH (100) alone = RED COMPROMISED
	cases := []struct {
		level         string
		wantColor     string
		wantStatus    string
	}{
		{"THREAT_LEVEL_LOW", "GREEN", "NOMINAL"},
		{"THREAT_LEVEL_MODERATE", "YELLOW", "COMPROMISED"},
		{"THREAT_LEVEL_HIGH", "RED", "COMPROMISED"},
		{"THREAT_LEVEL_CRITICAL", "RED", "COMPROMISED"},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			s := newTestStore(t)
			mustInsertThreat(t, s, tc.level)
			got := mustComputeStatus(t, s)
			if got.Color != tc.wantColor {
				t.Errorf("color = %s, want %s", got.Color, tc.wantColor)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %s, want %s", got.Status, tc.wantStatus)
			}
		})
	}
}

// ── InsertFaces / InsertThreat error path ─────────────────────────────

func TestInsertThreat_Roundtrip(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertThreat(context.Background(), analyticsstore.ThreatEvent{
		SubjectID:   "ivan-vanko",
		ObjectType:  "person",
		Location:    "monaco-circuit",
		Signals:     []string{"energy_signature", "weapons_detected"},
		ThreatLevel: "THREAT_LEVEL_HIGH",
	})
	if err != nil {
		t.Fatalf("InsertThreat: %v", err)
	}
	got := mustComputeStatus(t, s)
	if got.Score == 0 {
		t.Error("expected non-zero score after threat insert")
	}
}

func TestInsertFaces_Roundtrip(t *testing.T) {
	s := newTestStore(t)
	err := s.InsertFaces(context.Background(), analyticsstore.FacesEvent{
		Filename:   "faces_20260328.png",
		FaceCount:  2,
		Sentiments: []string{"neutral", "suspicious"},
		ImagePath:  "/home/vagrant/.jarvis/faces/faces_20260328.png",
	})
	if err != nil {
		t.Fatalf("InsertFaces: %v", err)
	}
	got := mustComputeStatus(t, s)
	if got.Score == 0 {
		t.Error("expected non-zero score after faces insert")
	}
}

// ── helpers ───────────────────────────────────────────────────────────

func mustInsertThreat(t *testing.T, s *analyticsstore.Store, level string) {
	t.Helper()
	if err := s.InsertThreat(context.Background(), analyticsstore.ThreatEvent{
		ThreatLevel: level,
	}); err != nil {
		t.Fatalf("InsertThreat(%s): %v", level, err)
	}
}

func mustInsertFaces(t *testing.T, s *analyticsstore.Store, sentiments []string) {
	t.Helper()
	if err := s.InsertFaces(context.Background(), analyticsstore.FacesEvent{
		Sentiments: sentiments,
	}); err != nil {
		t.Fatalf("InsertFaces: %v", err)
	}
}

func mustComputeStatus(t *testing.T, s *analyticsstore.Store) analyticsstore.SurroundingsStatus {
	t.Helper()
	got, err := s.ComputeStatus(context.Background())
	if err != nil {
		t.Fatalf("ComputeStatus: %v", err)
	}
	return got
}
