package email

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Event holds the data needed to build an iCalendar invite.
type Event struct {
	Title       string
	Description string
	Location    string
	Attendees   []string
	Start       time.Time
	End         time.Time
	OrganizerEmail string
}

// BuildICS returns an RFC 5545 iCalendar payload for the event.
func BuildICS(e Event) string {
	uid := uuid.NewString() + "@jarvis"
	now := time.Now().UTC().Format("20060102T150405Z")
	start := e.Start.UTC().Format("20060102T150405Z")
	end := e.End.UTC().Format("20060102T150405Z")

	var sb strings.Builder
	sb.WriteString("BEGIN:VCALENDAR\r\n")
	sb.WriteString("VERSION:2.0\r\n")
	sb.WriteString("PRODID:-//Jarvis//EN\r\n")
	sb.WriteString("METHOD:REQUEST\r\n")
	sb.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(&sb, "UID:%s\r\n", uid)
	fmt.Fprintf(&sb, "DTSTAMP:%s\r\n", now)
	fmt.Fprintf(&sb, "DTSTART:%s\r\n", start)
	fmt.Fprintf(&sb, "DTEND:%s\r\n", end)
	fmt.Fprintf(&sb, "SUMMARY:%s\r\n", foldLine(e.Title))
	if e.Description != "" {
		fmt.Fprintf(&sb, "DESCRIPTION:%s\r\n", foldLine(e.Description))
	}
	if e.Location != "" {
		fmt.Fprintf(&sb, "LOCATION:%s\r\n", foldLine(e.Location))
	}
	if e.OrganizerEmail != "" {
		fmt.Fprintf(&sb, "ORGANIZER:MAILTO:%s\r\n", e.OrganizerEmail)
	}
	for _, a := range e.Attendees {
		fmt.Fprintf(&sb, "ATTENDEE;RSVP=TRUE:MAILTO:%s\r\n", a)
	}
	sb.WriteString("END:VEVENT\r\n")
	sb.WriteString("END:VCALENDAR\r\n")
	return sb.String()
}

// foldLine wraps long iCal property values at 75 octets per RFC 5545 §3.1.
func foldLine(s string) string {
	const limit = 75
	if len(s) <= limit {
		return s
	}
	var sb strings.Builder
	for i, r := range s {
		if i > 0 && i%limit == 0 {
			sb.WriteString("\r\n ")
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
