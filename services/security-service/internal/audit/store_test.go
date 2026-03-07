package audit_test

import (
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/services/security-service/internal/audit"
)

func TestStore_AppendAndQuery(t *testing.T) {
	s := audit.New()

	s.Append("tony-stark", "authenticate:PASSCODE", "security/auth", true)
	s.Append("tony-stark", "threat_assessment:HIGH", "security/threat", true)
	s.Append("pepper-potts", "authenticate:PASSCODE", "security/auth", true)

	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}

	// Query all entries for tony
	entries, nextToken, err := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "")
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries for tony-stark, want 2", len(entries))
	}
	if nextToken != "" {
		t.Error("expected empty next token (all results fit on one page)")
	}
}

func TestStore_Pagination(t *testing.T) {
	s := audit.New()

	for i := 0; i < 10; i++ {
		s.Append("tony-stark", "action", "resource", true)
	}

	// Page 1: 4 entries
	page1, token, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, "")
	if err != nil {
		t.Fatalf("page 1 error: %v", err)
	}
	if len(page1) != 4 {
		t.Errorf("page 1 len = %d, want 4", len(page1))
	}
	if token == "" {
		t.Error("expected next page token after page 1")
	}

	// Page 2: next 4
	page2, token2, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, token)
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if len(page2) != 4 {
		t.Errorf("page 2 len = %d, want 4", len(page2))
	}
	if token2 == "" {
		t.Error("expected next page token after page 2")
	}

	// Page 3: last 2
	page3, token3, err := s.Query("tony-stark", time.Time{}, time.Time{}, 4, token2)
	if err != nil {
		t.Fatalf("page 3 error: %v", err)
	}
	if len(page3) != 2 {
		t.Errorf("page 3 len = %d, want 2", len(page3))
	}
	if token3 != "" {
		t.Error("expected empty token on last page")
	}
}

func TestStore_QueryAllSubjects(t *testing.T) {
	s := audit.New()
	s.Append("tony-stark", "a", "r", true)
	s.Append("pepper-potts", "b", "r", false)
	s.Append("james-rhodes", "c", "r", true)

	all, _, err := s.Query("", time.Time{}, time.Time{}, 100, "")
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d entries for all subjects, want 3", len(all))
	}
}

func TestStore_InvalidPageToken(t *testing.T) {
	s := audit.New()
	s.Append("tony-stark", "a", "r", true)

	_, _, err := s.Query("tony-stark", time.Time{}, time.Time{}, 10, "!!!not-base64!!!")
	if err == nil {
		t.Error("expected error for invalid page token, got nil")
	}
}

func TestStore_EventIDsAreUnique(t *testing.T) {
	s := audit.New()
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
