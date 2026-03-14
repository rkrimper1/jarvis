// Package calendar manages Tony Stark's schedule and detects conflicts.
package calendar

import (
	"fmt"
	"sync"
	"time"

	businessv1 "github.com/rkrimper1/jarvis/api/pb/business"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Event is the internal calendar event record.
type Event struct {
	ID        string
	Title     string
	Location  string
	Attendees []string
	Start     time.Time
	End       time.Time
	Priority  bool
	Status    businessv1.TaskStatus
}

// Store is a thread-safe calendar backed by an in-memory slice.
type Store struct {
	mu     sync.RWMutex
	events map[string]*Event
	seq    int64
}

// New creates a seeded Store.
func New() *Store {
	s := &Store{events: make(map[string]*Event)}
	s.seed()
	return s
}

// Schedule adds a new event, returning the event ID and any conflicting event IDs.
func (s *Store) Schedule(
	title, description, location string,
	attendees []string,
	start, end time.Time,
	highPriority bool,
) (id string, conflicts []string, autoResolved bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seq++
	id = fmt.Sprintf("evt-%04d", s.seq)

	// Detect conflicts for the same attendees
	for _, ev := range s.events {
		if ev.Status == businessv1.TaskStatus_TASK_STATUS_CANCELLED {
			continue
		}
		if rangesOverlap(ev.Start, ev.End, start, end) && hasCommonAttendee(ev.Attendees, attendees) {
			conflicts = append(conflicts, ev.ID)
		}
	}

	// Auto-resolve low-priority conflicts if the new event is high priority
	if highPriority && len(conflicts) > 0 {
		for _, cid := range conflicts {
			if ev, ok := s.events[cid]; ok && !ev.Priority {
				ev.Status = businessv1.TaskStatus_TASK_STATUS_CANCELLED
			}
		}
		autoResolved = true
		conflicts = nil
	}

	s.events[id] = &Event{
		ID:        id,
		Title:     title,
		Location:  location,
		Attendees: attendees,
		Start:     start,
		End:       end,
		Priority:  highPriority,
		Status:    businessv1.TaskStatus_TASK_STATUS_PENDING,
	}
	return id, conflicts, autoResolved
}

// List returns events for a subject within the given time window.
func (s *Store) List(subjectID string, from, to time.Time) []*businessv1.ScheduledEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*businessv1.ScheduledEvent
	for _, ev := range s.events {
		if ev.Status == businessv1.TaskStatus_TASK_STATUS_CANCELLED {
			continue
		}
		if !from.IsZero() && ev.Start.Before(from) {
			continue
		}
		if !to.IsZero() && ev.End.After(to) {
			continue
		}
		if subjectID != "" && !hasAttendee(ev.Attendees, subjectID) {
			continue
		}
		result = append(result, &businessv1.ScheduledEvent{
			EventId:   ev.ID,
			Title:     ev.Title,
			Location:  ev.Location,
			Attendees: ev.Attendees,
			Start:     timestamppb.New(ev.Start),
			End:       timestamppb.New(ev.End),
			Status:    ev.Status,
		})
	}
	return result
}

func rangesOverlap(s1, e1, s2, e2 time.Time) bool {
	return s1.Before(e2) && s2.Before(e1)
}

func hasCommonAttendee(a, b []string) bool {
	set := make(map[string]bool)
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if set[v] {
			return true
		}
	}
	return false
}

func hasAttendee(attendees []string, subject string) bool {
	for _, a := range attendees {
		if a == subject {
			return true
		}
	}
	return false
}

func (s *Store) seed() {
	now := time.Now()
	s.seq = 10
	s.events["evt-0001"] = &Event{
		ID: "evt-0001", Title: "Board Meeting — Stark Industries Q4",
		Attendees: []string{"tony-stark", "pepper-potts", "happy-hogan"},
		Start:     now.Add(2 * time.Hour), End: now.Add(4 * time.Hour),
		Location: "stark-tower-boardroom", Priority: true,
		Status: businessv1.TaskStatus_TASK_STATUS_PENDING,
	}
	s.events["evt-0002"] = &Event{
		ID: "evt-0002", Title: "MIT Research Lecture",
		Attendees: []string{"tony-stark"},
		Start:     now.Add(24 * time.Hour), End: now.Add(26 * time.Hour),
		Location: "mit-kresge-auditorium", Priority: false,
		Status: businessv1.TaskStatus_TASK_STATUS_PENDING,
	}
}
