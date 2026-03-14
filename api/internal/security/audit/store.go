// Package audit provides an append-only audit log for security events.
// In production this would write to a Cloud Spanner or BigQuery table.
package audit

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Store is a thread-safe append-only audit log.
type Store struct {
	mu      sync.RWMutex
	entries []*securityv1.AuditEntry
	counter int64
}

// New creates a new audit Store.
func New() *Store {
	return &Store{
		entries: make([]*securityv1.AuditEntry, 0, 256),
	}
}

// Append adds a new entry to the audit log.
func (s *Store) Append(subjectID, action, resource string, success bool) {
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

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Decode cursor (offset into filtered results)
	offset := 0
	if pageToken != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(pageToken)
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token")
		}
		offset, err = strconv.Atoi(string(decoded))
		if err != nil {
			return nil, "", fmt.Errorf("invalid page token value")
		}
	}

	// Collect matching entries
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

	// Apply pagination
	if offset >= len(filtered) {
		return []*securityv1.AuditEntry{}, "", nil
	}

	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	page := filtered[offset:end]

	// Build next page token
	if end < len(filtered) {
		nextOffset := strconv.Itoa(end)
		nextToken = base64.RawURLEncoding.EncodeToString([]byte(nextOffset))
	}

	return page, nextToken, nil
}

// Len returns the total number of audit entries.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
