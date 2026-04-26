// Package threatstore persists camera threat events to SQLite.
package threatstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
)

const schema = `
CREATE TABLE IF NOT EXISTS threat_events (
	id                  TEXT PRIMARY KEY,
	camera_label        TEXT NOT NULL DEFAULT '',
	detected_objects    TEXT NOT NULL DEFAULT '[]',
	level               TEXT NOT NULL DEFAULT 'THREAT_LEVEL_UNSPECIFIED',
	confidence          REAL NOT NULL DEFAULT 0,
	threat_summary      TEXT NOT NULL DEFAULT '',
	recommended_actions TEXT NOT NULL DEFAULT '[]',
	image_url           TEXT NOT NULL DEFAULT '',
	created_at          TEXT NOT NULL DEFAULT (datetime('now'))
);`

// Store is a SQLite-backed threat event log.
type Store struct {
	db *sql.DB
}

// New initialises the threat_events table and returns a Store.
func New(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("threatstore: create table: %w", err)
	}
	return &Store{db: db}, nil
}

// Log persists a threat event and returns it with its generated ID and timestamp.
func (s *Store) Log(ctx context.Context, e *securityv1.ThreatEvent) (*securityv1.ThreatEvent, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	objs, _ := json.Marshal(e.DetectedObjects)
	acts, _ := json.Marshal(e.RecommendedActions)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO threat_events
			(id, camera_label, detected_objects, level, confidence, threat_summary, recommended_actions, image_url, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		e.CameraLabel,
		string(objs),
		e.Level.String(),
		e.Confidence,
		e.ThreatSummary,
		string(acts),
		e.ImageUrl,
		now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("threatstore: insert: %w", err)
	}

	out := proto(e)
	out.EventId = id
	out.Timestamp = timestamppb.New(now)
	return out, nil
}

// List returns the most recent events, newest first.
func (s *Store) List(ctx context.Context, pageSize int32) ([]*securityv1.ThreatEvent, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, camera_label, detected_objects, level, confidence,
		        threat_summary, recommended_actions, image_url, created_at
		 FROM threat_events
		 ORDER BY created_at DESC
		 LIMIT ?`,
		pageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("threatstore: list: %w", err)
	}
	defer rows.Close()

	var events []*securityv1.ThreatEvent
	for rows.Next() {
		var (
			id, cameraLabel, objsJSON, levelStr, summary, actsJSON, imageURL, createdAt string
			confidence                                                                    float64
		)
		if err := rows.Scan(&id, &cameraLabel, &objsJSON, &levelStr, &confidence,
			&summary, &actsJSON, &imageURL, &createdAt); err != nil {
			return nil, fmt.Errorf("threatstore: scan: %w", err)
		}

		var objs, acts []string
		_ = json.Unmarshal([]byte(objsJSON), &objs)
		_ = json.Unmarshal([]byte(actsJSON), &acts)

		ts, _ := time.Parse(time.RFC3339, createdAt)

		events = append(events, &securityv1.ThreatEvent{
			EventId:            id,
			Timestamp:          timestamppb.New(ts),
			CameraLabel:        cameraLabel,
			DetectedObjects:    objs,
			Level:              parseThreatLevel(levelStr),
			Confidence:         float32(confidence),
			ThreatSummary:      summary,
			RecommendedActions: acts,
			ImageUrl:           imageURL,
		})
	}
	return events, rows.Err()
}

// proto copies the mutable fields from e into a new proto (without EventId / Timestamp).
func proto(e *securityv1.ThreatEvent) *securityv1.ThreatEvent {
	objs := make([]string, len(e.DetectedObjects))
	copy(objs, e.DetectedObjects)
	acts := make([]string, len(e.RecommendedActions))
	copy(acts, e.RecommendedActions)
	return &securityv1.ThreatEvent{
		CameraLabel:        e.CameraLabel,
		DetectedObjects:    objs,
		Level:              e.Level,
		Confidence:         e.Confidence,
		ThreatSummary:      e.ThreatSummary,
		RecommendedActions: acts,
		ImageUrl:           e.ImageUrl,
	}
}

func parseThreatLevel(s string) securityv1.ThreatLevel {
	if v, ok := securityv1.ThreatLevel_value[s]; ok {
		return securityv1.ThreatLevel(v)
	}
	return securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED
}
