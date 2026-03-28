// Package analyticsstore persists THREAT and FACES analytics events to SQLite
// and computes the SurroundingsStatus score from recent activity.
package analyticsstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS analytics_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  TEXT    NOT NULL,
    created_at  DATETIME DEFAULT (datetime('now')),

    -- THREAT fields
    subject_id  TEXT,
    object_type TEXT,
    location    TEXT,
    signals     TEXT,        -- JSON array of strings
    threat_level TEXT,

    -- FACES fields
    filename    TEXT,
    face_count  INTEGER,
    sentiments  TEXT,        -- JSON array of sentiment strings
    image_path  TEXT,

    -- pre-computed event score stored for history
    score       REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_analytics_created ON analytics_events(created_at);
`

// Store is the SQLite-backed analytics event store.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New opens (or creates) the SQLite database at dbPath and applies the schema.
func New(dbPath string, log *slog.Logger) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("analyticsstore: open db: %w", err)
	}
	// WAL mode allows concurrent reads alongside a single writer, preventing
	// lock contention when gRPC handlers for threat, faces, and audit queries
	// run concurrently against the same *sql.DB.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		return nil, fmt.Errorf("analyticsstore: enable WAL: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("analyticsstore: apply schema: %w", err)
	}
	return &Store{db: db, log: log}, nil
}

// DB returns the underlying *sql.DB so other stores can share the connection.
func (s *Store) DB() *sql.DB { return s.db }

// ThreatEvent holds the metadata captured when AssessThreat completes.
type ThreatEvent struct {
	SubjectID   string
	ObjectType  string
	Location    string
	Signals     []string
	ThreatLevel string // e.g. "THREAT_LEVEL_HIGH"
}

// FacesEvent holds the metadata captured when AnalyzeFaces completes.
type FacesEvent struct {
	Filename   string
	FaceCount  int
	Sentiments []string // one entry per detected face
	ImagePath  string
}

// InsertThreat records a completed threat assessment.
func (s *Store) InsertThreat(ctx context.Context, e ThreatEvent) error {
	sig, _ := json.Marshal(e.Signals)
	score := threatLevelToScore(e.ThreatLevel)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analytics_events
		    (event_type, subject_id, object_type, location, signals, threat_level, score)
		VALUES ('threat', ?, ?, ?, ?, ?, ?)`,
		e.SubjectID, e.ObjectType, e.Location, string(sig), e.ThreatLevel, score,
	)
	if err != nil {
		return fmt.Errorf("analyticsstore: insert threat: %w", err)
	}
	return nil
}

// InsertFaces records a completed face-analysis run.
func (s *Store) InsertFaces(ctx context.Context, e FacesEvent) error {
	sents, _ := json.Marshal(e.Sentiments)
	score := avgSentimentsScore(e.Sentiments)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analytics_events
		    (event_type, filename, face_count, sentiments, image_path, score)
		VALUES ('faces', ?, ?, ?, ?, ?)`,
		e.Filename, e.FaceCount, string(sents), e.ImagePath, score,
	)
	if err != nil {
		return fmt.Errorf("analyticsstore: insert faces: %w", err)
	}
	return nil
}

// SurroundingsStatus is the computed aggregate assessment.
type SurroundingsStatus struct {
	Score  float64 // 0–100
	Color  string  // "GREEN" | "YELLOW" | "RED"
	Status string  // "NOMINAL" | "COMPROMISED"
}

// ComputeStatus calculates the current SurroundingsStatus.
// It uses events from the last 30 minutes; if none exist it falls back to
// the last 20 events regardless of age.
func (s *Store) ComputeStatus(ctx context.Context) (SurroundingsStatus, error) {
	events, err := s.queryWindow(ctx)
	if err != nil {
		return SurroundingsStatus{}, err
	}
	if len(events) == 0 {
		return SurroundingsStatus{Score: 0, Color: "GREEN", Status: "NOMINAL"}, nil
	}
	return statusFromEvents(events), nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

type eventRow struct {
	eventType string
	score     float64
}

func (s *Store) queryWindow(ctx context.Context) ([]eventRow, error) {
	cutoff := time.Now().UTC().Add(-30 * time.Minute).Format("2006-01-02 15:04:05")

	rows, err := s.db.QueryContext(ctx,
		`SELECT event_type, score FROM analytics_events WHERE created_at >= ? ORDER BY created_at DESC`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("analyticsstore: query window: %w", err)
	}
	defer rows.Close()

	events, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		return events, nil
	}

	// Fall back: last 20 events regardless of age.
	rows2, err := s.db.QueryContext(ctx,
		`SELECT event_type, score FROM analytics_events ORDER BY created_at DESC LIMIT 20`,
	)
	if err != nil {
		return nil, fmt.Errorf("analyticsstore: query fallback: %w", err)
	}
	defer rows2.Close()
	return scanRows(rows2)
}

func scanRows(rows *sql.Rows) ([]eventRow, error) {
	var out []eventRow
	for rows.Next() {
		var r eventRow
		if err := rows.Scan(&r.eventType, &r.score); err != nil {
			return nil, fmt.Errorf("analyticsstore: scan row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func statusFromEvents(events []eventRow) SurroundingsStatus {
	var threatScores, faceScores []float64
	for _, e := range events {
		if e.eventType == "threat" {
			threatScores = append(threatScores, e.score)
		} else {
			faceScores = append(faceScores, e.score)
		}
	}

	hasThreat := len(threatScores) > 0
	hasFaces := len(faceScores) > 0

	var score float64
	switch {
	case hasThreat && hasFaces:
		score = 0.7*avg(threatScores) + 0.3*avg(faceScores)
	case hasThreat:
		score = avg(threatScores)
	case hasFaces:
		score = avg(faceScores)
	}

	score = math.Round(score*10) / 10 // one decimal place
	return scoreToStatus(score)
}

func scoreToStatus(score float64) SurroundingsStatus {
	var color string
	switch {
	case score <= 20:
		color = "GREEN"
	case score <= 70:
		color = "YELLOW"
	default:
		color = "RED"
	}
	statusLabel := "NOMINAL"
	if score >= 40 {
		statusLabel = "COMPROMISED"
	}
	return SurroundingsStatus{Score: score, Color: color, Status: statusLabel}
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// threatLevelToScore maps a ThreatLevel proto string to a 0–100 score.
// Temporary mapping until the THREAT module is fully implemented.
func threatLevelToScore(level string) float64 {
	switch strings.ToUpper(strings.TrimPrefix(level, "THREAT_LEVEL_")) {
	case "CRITICAL", "HIGH":
		return 100
	case "MODERATE":
		return 50
	case "LOW":
		return 10
	default:
		return 0
	}
}

var sentimentScoreMap = map[string]float64{
	"terrified": 90, "hostile": 90, "angry": 90, "furious": 90, "aggressive": 90,
	"alarmed": 70, "fearful": 70, "panicked": 70, "suspicious": 70,
	"distressed": 50, "sad": 50, "worried": 50, "tense": 50,
	"confused": 35, "surprised": 35, "shocked": 35,
	"neutral": 20, "bored": 20, "calm": 20, "blank": 20,
	"happy": 10, "pleased": 10, "amused": 10, "content": 10, "smug": 10,
}

func sentimentToScore(s string) float64 {
	if v, ok := sentimentScoreMap[strings.ToLower(s)]; ok {
		return v
	}
	return 30 // unknown
}

func avgSentimentsScore(sentiments []string) float64 {
	if len(sentiments) == 0 {
		return 0
	}
	var total float64
	for _, s := range sentiments {
		total += sentimentToScore(s)
	}
	return total / float64(len(sentiments))
}
