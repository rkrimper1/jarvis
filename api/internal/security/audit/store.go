// Package audit provides an append-only audit log for security events,
// backed by either SQLite (production) or in-memory (tests / no DB configured).
package audit

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const auditSchema = `
CREATE TABLE IF NOT EXISTS audits (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_id TEXT    NOT NULL DEFAULT '',
    action     TEXT    NOT NULL,
    resource   TEXT    NOT NULL DEFAULT '',
    success    INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_audits_created ON audits(created_at);
CREATE INDEX IF NOT EXISTS idx_audits_subject ON audits(subject_id);
`

// Store is a thread-safe append-only audit log.
// When db is non-nil it persists to SQLite; otherwise it is in-memory only.
type Store struct {
	// in-memory backend
	mu      sync.RWMutex
	entries []*securityv1.AuditEntry
	counter int64

	// SQLite backend (nil = use in-memory)
	db *sql.DB
}

// New creates an in-memory Store (used in tests and when no DB is configured).
func New() *Store {
	return &Store{entries: make([]*securityv1.AuditEntry, 0, 256)}
}

// NewSQLite creates a Store backed by the provided *sql.DB.
// It applies the audits table schema on the shared connection.
func NewSQLite(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(auditSchema); err != nil {
		return nil, fmt.Errorf("audit: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Append adds a new entry to the audit log.
func (s *Store) Append(subjectID, action, resource string, success bool) {
	if s.db != nil {
		successInt := 0
		if success {
			successInt = 1
		}
		if _, err := s.db.Exec(
			`INSERT INTO audits (subject_id, action, resource, success) VALUES (?, ?, ?, ?)`,
			subjectID, action, resource, successInt,
		); err != nil {
			// best-effort: log to stderr rather than crashing the RPC
			fmt.Printf("audit: insert failed: %v\n", err)
		}
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	s.entries = append(s.entries, &securityv1.AuditEntry{
		EventId:   fmt.Sprintf("evt-%06d", s.counter),
		SubjectId: subjectID,
		Action:    action,
		Resource:  resource,
		Success:   success,
		Timestamp: timestamppb.Now(),
	})
}

// Query returns a filtered, paginated slice of audit entries.
func (s *Store) Query(
	subjectID string,
	from, to time.Time,
	pageSize int,
	pageToken string,
) (entries []*securityv1.AuditEntry, nextToken string, err error) {

	if s.db != nil {
		return s.querySQLite(subjectID, from, to, pageSize, pageToken)
	}
	return s.queryMemory(subjectID, from, to, pageSize, pageToken)
}

// Len returns the total number of audit entries.
func (s *Store) Len() int {
	if s.db != nil {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM audits`).Scan(&n) //nolint:errcheck
		return n
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// ── SQLite backend ────────────────────────────────────────────────────────────

func (s *Store) querySQLite(
	subjectID string,
	from, to time.Time,
	pageSize int,
	pageToken string,
) ([]*securityv1.AuditEntry, string, error) {

	offset, err := decodeToken(pageToken)
	if err != nil {
		return nil, "", err
	}

	// Build WHERE clauses
	var conds []string
	var args []any
	if subjectID != "" {
		conds = append(conds, "LOWER(subject_id) = LOWER(?)")
		args = append(args, subjectID)
	}
	if !from.IsZero() && from.Unix() > 0 {
		conds = append(conds, "created_at >= ?")
		args = append(args, from.UTC().Format("2006-01-02 15:04:05"))
	}
	if !to.IsZero() && to.Unix() > 0 {
		conds = append(conds, "created_at <= ?")
		args = append(args, to.UTC().Format("2006-01-02 15:04:05"))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	// Fetch one extra row to detect if there's a next page
	query := fmt.Sprintf(
		`SELECT id, subject_id, action, resource, success, created_at FROM audits %s ORDER BY id DESC LIMIT ? OFFSET ?`,
		where,
	)
	args = append(args, pageSize+1, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	var result []*securityv1.AuditEntry
	for rows.Next() {
		var (
			id        int64
			subj, act, res string
			succ      int
			createdAt string
		)
		if err := rows.Scan(&id, &subj, &act, &res, &succ, &createdAt); err != nil {
			return nil, "", fmt.Errorf("audit: scan: %w", err)
		}
		ts, _ := time.Parse("2006-01-02 15:04:05", createdAt)
		result = append(result, &securityv1.AuditEntry{
			EventId:   fmt.Sprintf("evt-%06d", id),
			SubjectId: subj,
			Action:    act,
			Resource:  res,
			Success:   succ == 1,
			Timestamp: timestamppb.New(ts),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("audit: rows: %w", err)
	}

	var next string
	if len(result) > pageSize {
		result = result[:pageSize]
		next = encodeToken(offset + pageSize)
	}
	return result, next, nil
}

// ── in-memory backend ─────────────────────────────────────────────────────────

func (s *Store) queryMemory(
	subjectID string,
	from, to time.Time,
	pageSize int,
	pageToken string,
) ([]*securityv1.AuditEntry, string, error) {

	s.mu.RLock()
	defer s.mu.RUnlock()

	offset, err := decodeToken(pageToken)
	if err != nil {
		return nil, "", err
	}

	var filtered []*securityv1.AuditEntry
	for _, e := range s.entries {
		if subjectID != "" && !strings.EqualFold(e.SubjectId, subjectID) {
			continue
		}
		ts := e.Timestamp.AsTime()
		if !from.IsZero() && ts.Before(from) {
			continue
		}
		if !to.IsZero() && ts.After(to) {
			continue
		}
		filtered = append(filtered, e)
	}

	if offset >= len(filtered) {
		return []*securityv1.AuditEntry{}, "", nil
	}

	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	var next string
	if end < len(filtered) {
		next = encodeToken(end)
	}
	return filtered[offset:end], next, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func encodeToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid page token")
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return 0, fmt.Errorf("invalid page token value")
	}
	return n, nil
}
