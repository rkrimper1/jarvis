package audit_test

import (
	"database/sql"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rkrimper1/jarvis/api/internal/security/audit"
)

// newSQLiteStore opens an in-memory SQLite DB and returns a SQLite-backed Store.
func newSQLiteStore(t *testing.T) *audit.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := audit.NewSQLite(db, slog.Default())
	if err != nil {
		t.Fatalf("audit.NewSQLite: %v", err)
	}
	return s
}

// ── Basic append + query ──────────────────────────────────────────────

func TestSQLiteStore_AppendAndQuery(t *testing.T) {
	s := newSQLiteStore(t)

	s.Append("tony-stark", "authenticate:PASSCODE", "security/auth", true)
	s.Append("tony-stark", "threat_assessment:HIGH", "security/threat", true)
	s.Append("pepper-potts", "authenticate:PASSCODE", "security/auth", false)

	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}

	entries, nextToken, err := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("tony-stark entries = %d, want 2", len(entries))
	}
	if nextToken != "" {
		t.Error("expected empty next token")
	}
}

func TestSQLiteStore_QueryAllSubjects(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("tony-stark", "a", "r", true)
	s.Append("pepper-potts", "b", "r", false)
	s.Append("james-rhodes", "c", "r", true)

	all, _, err := s.Query("", time.Time{}, time.Time{}, 100, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all subjects = %d entries, want 3", len(all))
	}
}

func TestSQLiteStore_SubjectFilterCaseInsensitive(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("Tony-Stark", "action", "resource", true)

	entries, _, err := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("case-insensitive filter = %d entries, want 1", len(entries))
	}
}

// ── Pagination ────────────────────────────────────────────────────────

func TestSQLiteStore_Pagination(t *testing.T) {
	s := newSQLiteStore(t)
	for i := 0; i < 10; i++ {
		s.Append("tony-stark", "action", "resource", true)
	}

	page1, tok1, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 4 {
		t.Errorf("page 1 len = %d, want 4", len(page1))
	}
	if tok1 == "" {
		t.Error("expected next token after page 1")
	}

	page2, tok2, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, tok1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 4 {
		t.Errorf("page 2 len = %d, want 4", len(page2))
	}
	if tok2 == "" {
		t.Error("expected next token after page 2")
	}

	page3, tok3, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, tok2)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 2 {
		t.Errorf("page 3 len = %d, want 2", len(page3))
	}
	if tok3 != "" {
		t.Error("expected empty token on last page")
	}
}

func TestSQLiteStore_InvalidPageToken(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("tony-stark", "a", "r", true)

	_, _, err := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid page token, got nil")
	}
}

// ── Nil / zero timestamp must not filter results ──────────────────────
//
// Regression: (*timestamppb.Timestamp)(nil).AsTime() returns time.Unix(0,0),
// not time.Time{}, so IsZero() returns false. The fix checks Unix() > 0.

func TestSQLiteStore_ZeroTimestamp_DoesNotFilter(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("tony-stark", "action", "resource", true)

	// time.Unix(0, 0) mimics what grpc-gateway sends when the proto timestamp
	// field is nil — it must not be used as a filter bound.
	epoch := time.Unix(0, 0)
	entries, _, err := s.Query("", epoch, epoch, 100, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("epoch timestamps should not filter: got %d entries, want 1", len(entries))
	}
}

func TestSQLiteStore_ValidTimeRange_FiltersCorrectly(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("tony-stark", "action", "resource", true)

	// A 'from' in the far future should exclude all current entries.
	future := time.Now().Add(24 * time.Hour)
	entries, _, err := s.Query("", future, time.Time{}, 100, "")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("future 'from' should exclude all entries: got %d", len(entries))
	}
}

// ── Entry fields ──────────────────────────────────────────────────────

func TestSQLiteStore_EventIDsAreUnique(t *testing.T) {
	s := newSQLiteStore(t)
	for i := 0; i < 5; i++ {
		s.Append("tony-stark", "action", "resource", true)
	}

	entries, _, _ := s.Query("tony-stark", time.Time{}, time.Time{}, 100, "")
	seen := make(map[string]bool)
	for _, e := range entries {
		if seen[e.EventId] {
			t.Errorf("duplicate event ID: %s", e.EventId)
		}
		seen[e.EventId] = true
	}
}

func TestSQLiteStore_SuccessField(t *testing.T) {
	s := newSQLiteStore(t)
	s.Append("tony-stark", "action", "resource", false)

	entries, _, _ := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "")
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if entries[0].Success {
		t.Error("Success should be false")
	}
}

func TestSQLiteStore_Len(t *testing.T) {
	s := newSQLiteStore(t)
	if s.Len() != 0 {
		t.Errorf("Len() before appends = %d, want 0", s.Len())
	}
	s.Append("a", "b", "c", true)
	s.Append("d", "e", "f", true)
	if s.Len() != 2 {
		t.Errorf("Len() after 2 appends = %d, want 2", s.Len())
	}
}
