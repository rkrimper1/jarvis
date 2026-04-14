package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

const defaultPageSize = 20
const maxPageSize = 100

const schema = `
CREATE TABLE IF NOT EXISTS raw_signals (
    id          TEXT PRIMARY KEY,
    source_type TEXT NOT NULL DEFAULT 'manual'
                CHECK(source_type IN ('manual','rss','file_upload')),
    raw_content TEXT NOT NULL,
    source_uri  TEXT NOT NULL DEFAULT '',
    ingested_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_raw_signals_ingested ON raw_signals(ingested_at);

CREATE TABLE IF NOT EXISTS intel_cards (
    id               TEXT PRIMARY KEY,
    title            TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',
    opportunity_type TEXT NOT NULL DEFAULT 'tactical'
                     CHECK(opportunity_type IN ('tactical','strategic','resource','threat_mitigation')),
    confidence_score REAL NOT NULL DEFAULT 0.0,
    suggested_action TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending_review'
                     CHECK(status IN ('pending_review','confirmed','dismissed')),
    raw_signal_ids   TEXT NOT NULL DEFAULT '[]', -- JSON array of signal UUIDs
    created_at       DATETIME DEFAULT (datetime('now')),
    updated_at       DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_intel_cards_status   ON intel_cards(status);
CREATE INDEX IF NOT EXISTS idx_intel_cards_created  ON intel_cards(created_at);
`

// Store is the SQLite-backed store for RawSignals and IntelCards.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New applies the intel schema to the shared db.
// The caller owns db and is responsible for closing it.
func New(db *sql.DB, log *slog.Logger) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("intel store: apply schema: %w", err)
	}
	return &Store{db: db, log: log}, nil
}

// Close is a no-op: the caller owns the shared *sql.DB.
func (s *Store) Close() error { return nil }

// ── RawSignal ─────────────────────────────────────────────────────────────────

// SaveSignal persists a new RawSignal and returns it with its generated id and ingested_at set.
func (s *Store) SaveSignal(ctx context.Context, sourceType intelligv1.SourceType, rawContent, sourceURI string) (*intelligv1.RawSignal, error) {
	id := uuid.New().String()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO raw_signals (id, source_type, raw_content, source_uri) VALUES (?,?,?,?)`,
		id, sourceTypeToString(sourceType), rawContent, sourceURI,
	)
	if err != nil {
		return nil, fmt.Errorf("intel store: insert signal: %w", err)
	}
	return s.GetSignal(ctx, id)
}

// SignalExistsBySourceURI reports whether a signal with the given source URI
// has already been ingested. Used by the RSS poller for deduplication.
func (s *Store) SignalExistsBySourceURI(ctx context.Context, uri string) (bool, error) {
	if uri == "" {
		return false, nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_signals WHERE source_uri=?`, uri).Scan(&n); err != nil {
		return false, fmt.Errorf("intel store: check source uri: %w", err)
	}
	return n > 0, nil
}

// GetSignal fetches a single RawSignal by id.
func (s *Store) GetSignal(ctx context.Context, id string) (*intelligv1.RawSignal, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, source_type, raw_content, source_uri, ingested_at FROM raw_signals WHERE id=?`, id)
	return s.scanSignal(row)
}

// ── IntelCard ─────────────────────────────────────────────────────────────────

// SaveCard persists a new IntelCard and returns it with id and timestamps set.
func (s *Store) SaveCard(ctx context.Context, title, summary string, oppType intelligv1.OpportunityType, confidence float32, suggestedAction string, signalIDs []string) (*intelligv1.IntelCard, error) {
	id := uuid.New().String()
	sigJSON, err := json.Marshal(signalIDs)
	if err != nil {
		return nil, fmt.Errorf("intel store: marshal signal ids: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO intel_cards (id, title, summary, opportunity_type, confidence_score, suggested_action, status, raw_signal_ids)
		 VALUES (?,?,?,?,?,?,'pending_review',?)`,
		id, title, summary, opportunityTypeToString(oppType), confidence, suggestedAction, string(sigJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("intel store: insert card: %w", err)
	}
	return s.GetCard(ctx, id)
}

// GetCard fetches a single IntelCard by id.
func (s *Store) GetCard(ctx context.Context, id string) (*intelligv1.IntelCard, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, summary, opportunity_type, confidence_score, suggested_action,
		        status, raw_signal_ids, created_at, updated_at
		 FROM intel_cards WHERE id=?`, id)
	return s.scanCard(row)
}

// UpdateCardStatus flips a card to CONFIRMED or DISMISSED.
func (s *Store) UpdateCardStatus(ctx context.Context, id string, newStatus intelligv1.IntelCardStatus) (*intelligv1.IntelCard, error) {
	statusStr := cardStatusToString(newStatus)
	res, err := s.db.ExecContext(ctx,
		`UPDATE intel_cards SET status=?, updated_at=datetime('now') WHERE id=?`,
		statusStr, id,
	)
	if err != nil {
		return nil, fmt.Errorf("intel store: update card status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("intel card %s: %w", id, ErrNotFound)
	}
	return s.GetCard(ctx, id)
}

// ListCards returns a page of IntelCards and the total matching count.
// statusFilter == UNSPECIFIED returns all statuses.
// pageToken encodes the OFFSET as a decimal string; empty string means offset 0.
func (s *Store) ListCards(ctx context.Context, statusFilter intelligv1.IntelCardStatus, pageSize int32, pageToken string) ([]*intelligv1.IntelCard, int32, string, error) {
	// Resolve page size.
	size := int(pageSize)
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}

	// Decode offset from token.
	offset := 0
	if pageToken != "" {
		if v, err := strconv.Atoi(pageToken); err == nil && v > 0 {
			offset = v
		}
	}

	// Build WHERE clause.
	var where string
	var args []any
	if statusFilter != intelligv1.IntelCardStatus_INTEL_CARD_STATUS_UNSPECIFIED {
		where = "WHERE status=?"
		args = append(args, cardStatusToString(statusFilter))
	}

	// Total count.
	var total int32
	countQ := "SELECT COUNT(*) FROM intel_cards " + where
	if err := s.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, "", fmt.Errorf("intel store: count cards: %w", err)
	}

	// Fetch page.
	listQ := fmt.Sprintf(
		`SELECT id, title, summary, opportunity_type, confidence_score, suggested_action,
		        status, raw_signal_ids, created_at, updated_at
		 FROM intel_cards %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		where,
	)
	rows, err := s.db.QueryContext(ctx, listQ, append(args, size, offset)...)
	if err != nil {
		return nil, 0, "", fmt.Errorf("intel store: list cards: %w", err)
	}
	defer rows.Close()

	var cards []*intelligv1.IntelCard
	for rows.Next() {
		card, err := s.scanCardRow(rows)
		if err != nil {
			return nil, 0, "", err
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("intel store: list cards rows: %w", err)
	}

	// Build next page token.
	nextToken := ""
	nextOffset := offset + size
	if int32(nextOffset) < total {
		nextToken = strconv.Itoa(nextOffset)
	}

	return cards, total, nextToken, nil
}

// ── scan helpers ──────────────────────────────────────────────────────────────

func (s *Store) scanSignal(row *sql.Row) (*intelligv1.RawSignal, error) {
	var sig intelligv1.RawSignal
	var sourceTypeStr, ingestedAt string
	err := row.Scan(&sig.Id, &sourceTypeStr, &sig.RawContent, &sig.SourceUri, &ingestedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("signal %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("intel store: scan signal: %w", err)
	}
	sig.SourceType = sourceTypeFromString(sourceTypeStr)
	sig.IngestedAt = s.parseTS(ingestedAt)
	return &sig, nil
}

func (s *Store) scanCard(row *sql.Row) (*intelligv1.IntelCard, error) {
	var c intelligv1.IntelCard
	var oppTypeStr, statusStr, sigIDsJSON, createdAt, updatedAt string
	err := row.Scan(
		&c.Id, &c.Title, &c.Summary, &oppTypeStr, &c.ConfidenceScore,
		&c.SuggestedAction, &statusStr, &sigIDsJSON, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("intel card %w", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("intel store: scan card: %w", err)
	}
	c.OpportunityType = opportunityTypeFromString(oppTypeStr)
	c.Status = cardStatusFromString(statusStr)
	c.RawSignalIds = unmarshalStringSlice(sigIDsJSON)
	c.CreatedAt = s.parseTS(createdAt)
	c.UpdatedAt = s.parseTS(updatedAt)
	return &c, nil
}

func (s *Store) scanCardRow(rows *sql.Rows) (*intelligv1.IntelCard, error) {
	var c intelligv1.IntelCard
	var oppTypeStr, statusStr, sigIDsJSON, createdAt, updatedAt string
	err := rows.Scan(
		&c.Id, &c.Title, &c.Summary, &oppTypeStr, &c.ConfidenceScore,
		&c.SuggestedAction, &statusStr, &sigIDsJSON, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("intel store: scan card row: %w", err)
	}
	c.OpportunityType = opportunityTypeFromString(oppTypeStr)
	c.Status = cardStatusFromString(statusStr)
	c.RawSignalIds = unmarshalStringSlice(sigIDsJSON)
	c.CreatedAt = s.parseTS(createdAt)
	c.UpdatedAt = s.parseTS(updatedAt)
	return &c, nil
}

// ── enum converters ───────────────────────────────────────────────────────────

func sourceTypeToString(t intelligv1.SourceType) string {
	switch t {
	case intelligv1.SourceType_SOURCE_TYPE_RSS:
		return "rss"
	case intelligv1.SourceType_SOURCE_TYPE_FILE_UPLOAD:
		return "file_upload"
	default:
		return "manual"
	}
}

func sourceTypeFromString(s string) intelligv1.SourceType {
	switch s {
	case "rss":
		return intelligv1.SourceType_SOURCE_TYPE_RSS
	case "file_upload":
		return intelligv1.SourceType_SOURCE_TYPE_FILE_UPLOAD
	default:
		return intelligv1.SourceType_SOURCE_TYPE_MANUAL
	}
}

func opportunityTypeToString(t intelligv1.OpportunityType) string {
	switch t {
	case intelligv1.OpportunityType_OPPORTUNITY_TYPE_STRATEGIC:
		return "strategic"
	case intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE:
		return "resource"
	case intelligv1.OpportunityType_OPPORTUNITY_TYPE_THREAT_MITIGATION:
		return "threat_mitigation"
	default:
		return "tactical"
	}
}

func opportunityTypeFromString(s string) intelligv1.OpportunityType {
	switch s {
	case "strategic":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_STRATEGIC
	case "resource":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE
	case "threat_mitigation":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_THREAT_MITIGATION
	default:
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL
	}
}

func cardStatusToString(s intelligv1.IntelCardStatus) string {
	switch s {
	case intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED:
		return "confirmed"
	case intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED:
		return "dismissed"
	default:
		return "pending_review"
	}
}

func cardStatusFromString(s string) intelligv1.IntelCardStatus {
	switch s {
	case "confirmed":
		return intelligv1.IntelCardStatus_INTEL_CARD_STATUS_CONFIRMED
	case "dismissed":
		return intelligv1.IntelCardStatus_INTEL_CARD_STATUS_DISMISSED
	default:
		return intelligv1.IntelCardStatus_INTEL_CARD_STATUS_PENDING_REVIEW
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func unmarshalStringSlice(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" || raw == "null" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func (s *Store) parseTS(raw string) *timestamppb.Timestamp {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return timestamppb.New(t)
		}
	}
	s.log.Warn("intel store: unrecognised timestamp format", "value", raw)
	return timestamppb.New(time.Time{})
}
