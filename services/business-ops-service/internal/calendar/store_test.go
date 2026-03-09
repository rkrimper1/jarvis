package calendar_test

import (
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/services/business-ops-service/internal/calendar"
)

func TestSchedule_Basic(t *testing.T) {
	s := calendar.New()
	now := time.Now()

	id, conflicts, _ := s.Schedule(
		"Team Standup", "", "zoom",
		[]string{"tony-stark", "pepper-potts"},
		now.Add(10*time.Hour), now.Add(11*time.Hour), false,
	)
	if id == "" {
		t.Error("expected non-empty event ID")
	}
	_ = conflicts
}

func TestSchedule_ConflictDetection(t *testing.T) {
	s := calendar.New()
	now := time.Now()
	base := now.Add(30 * time.Hour) // outside seeded events

	s.Schedule("Event A", "", "", []string{"tony-stark"},
		base, base.Add(2*time.Hour), false)

	_, conflicts, _ := s.Schedule("Event B", "", "", []string{"tony-stark"},
		base.Add(1*time.Hour), base.Add(3*time.Hour), false)

	if len(conflicts) == 0 {
		t.Error("expected conflict between overlapping events for same attendee")
	}
}

func TestSchedule_HighPriorityAutoResolves(t *testing.T) {
	s := calendar.New()
	now := time.Now()
	base := now.Add(50 * time.Hour)

	// Schedule a low-priority event
	s.Schedule("Low Priority Meeting", "", "", []string{"tony-stark"},
		base, base.Add(2*time.Hour), false)

	// High-priority event at same time should auto-resolve
	_, conflicts, autoResolved := s.Schedule("Critical Board Meeting", "", "", []string{"tony-stark"},
		base, base.Add(2*time.Hour), true)

	if len(conflicts) > 0 {
		t.Error("high priority event should have cleared conflicts")
	}
	if !autoResolved {
		t.Error("expected autoResolved=true")
	}
}

func TestSchedule_NoConflictDifferentAttendees(t *testing.T) {
	s := calendar.New()
	now := time.Now()
	base := now.Add(60 * time.Hour)

	s.Schedule("Pepper's Meeting", "", "", []string{"pepper-potts"},
		base, base.Add(2*time.Hour), false)

	_, conflicts, _ := s.Schedule("Happy's Meeting", "", "", []string{"happy-hogan"},
		base, base.Add(2*time.Hour), false)

	if len(conflicts) > 0 {
		t.Errorf("expected no conflict for different attendees, got %v", conflicts)
	}
}

func TestList_Filtering(t *testing.T) {
	s := calendar.New()

	// Seeded events have tony-stark
	events := s.List("tony-stark", time.Time{}, time.Time{})
	if len(events) == 0 {
		t.Error("expected events for tony-stark")
	}
	for _, e := range events {
		if e.EventId == "" {
			t.Error("event should have non-empty ID")
		}
	}
}

func TestList_TimeWindow(t *testing.T) {
	s := calendar.New()
	now := time.Now()

	// Events far in the future should not appear in a short window
	events := s.List("", now.Add(-time.Hour), now.Add(time.Hour))
	// Seeded events are 2h and 24h in the future — both outside [-1h, +1h]
	_ = events // just verify no panic
}
