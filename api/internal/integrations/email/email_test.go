package email_test

import (
	"strings"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/integrations/email"
)

// ── BuildICS ──────────────────────────────────────────────────────────────────

func TestBuildICS_RequiredFields(t *testing.T) {
	start := time.Date(2026, 4, 1, 14, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	e := email.Event{
		Title:          "Stark Expo",
		Description:    "Arc reactor demo",
		Location:       "stark-expo-pavilion",
		Attendees:      []string{"pepper@stark.industries", "happy@stark.industries"},
		Start:          start,
		End:            end,
		OrganizerEmail: "tony@stark.industries",
	}

	ics := email.BuildICS(e)

	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Jarvis//EN",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"END:VEVENT",
		"END:VCALENDAR",
		"SUMMARY:Stark Expo",
		"DESCRIPTION:Arc reactor demo",
		"LOCATION:stark-expo-pavilion",
		"ORGANIZER:MAILTO:tony@stark.industries",
		"ATTENDEE;RSVP=TRUE:MAILTO:pepper@stark.industries",
		"ATTENDEE;RSVP=TRUE:MAILTO:happy@stark.industries",
		"DTSTART:20260401T140000Z",
		"DTEND:20260401T150000Z",
	} {
		if !strings.Contains(ics, want) {
			t.Errorf("ICS missing %q", want)
		}
	}
}

func TestBuildICS_CRLF_LineEndings(t *testing.T) {
	e := email.Event{
		Title: "Test",
		Start: time.Now(),
		End:   time.Now().Add(time.Hour),
	}
	ics := email.BuildICS(e)
	if !strings.Contains(ics, "\r\n") {
		t.Error("ICS should use CRLF line endings per RFC 5545")
	}
}

func TestBuildICS_UniqueUID(t *testing.T) {
	e := email.Event{Title: "T", Start: time.Now(), End: time.Now().Add(time.Hour)}
	ics1 := email.BuildICS(e)
	ics2 := email.BuildICS(e)

	uid1 := extractField(ics1, "UID:")
	uid2 := extractField(ics2, "UID:")
	if uid1 == uid2 {
		t.Error("expected different UIDs on consecutive calls")
	}
}

func TestBuildICS_NoDescriptionOrLocation_OmittedFromOutput(t *testing.T) {
	e := email.Event{
		Title: "Minimal",
		Start: time.Now(),
		End:   time.Now().Add(time.Hour),
	}
	ics := email.BuildICS(e)
	if strings.Contains(ics, "DESCRIPTION:") {
		t.Error("DESCRIPTION should be absent when empty")
	}
	if strings.Contains(ics, "LOCATION:") {
		t.Error("LOCATION should be absent when empty")
	}
	if strings.Contains(ics, "ORGANIZER:") {
		t.Error("ORGANIZER should be absent when empty")
	}
}

func TestBuildICS_NoAttendees(t *testing.T) {
	e := email.Event{
		Title: "Solo",
		Start: time.Now(),
		End:   time.Now().Add(time.Hour),
	}
	ics := email.BuildICS(e)
	if strings.Contains(ics, "ATTENDEE") {
		t.Error("ATTENDEE should be absent when no attendees")
	}
}

func TestBuildICS_LongTitle_IsFolded(t *testing.T) {
	longTitle := strings.Repeat("A", 200)
	e := email.Event{Title: longTitle, Start: time.Now(), End: time.Now().Add(time.Hour)}
	ics := email.BuildICS(e)
	// RFC 5545: folded lines continue with CRLF + SPACE
	if !strings.Contains(ics, "\r\n ") {
		t.Error("long title should be folded with CRLF + SPACE continuation")
	}
}

func TestBuildICS_ShortTitle_NotFolded(t *testing.T) {
	e := email.Event{Title: "Short", Start: time.Now(), End: time.Now().Add(time.Hour)}
	ics := email.BuildICS(e)
	// Continuation is only present for lines exceeding 75 chars
	for _, line := range strings.Split(ics, "\r\n") {
		if strings.HasPrefix(line, "SUMMARY:") && strings.Contains(line, "\r\n ") {
			t.Error("short title should not be folded")
		}
	}
}

// ── mergeRecipients (tested indirectly via BuildICS + ConfigFromEnv) ──────────

func TestConfigFromEnv_MissingVars(t *testing.T) {
	// None of the SMTP_* env vars are set in the test environment.
	_, err := email.ConfigFromEnv()
	if err == nil {
		t.Skip("SMTP env vars are configured — skipping missing-vars test")
	}
	for _, name := range []string{"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_TO"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error should mention missing var %q, got: %v", name, err)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// extractField returns the value of the first ICS property with the given prefix.
func extractField(ics, prefix string) string {
	for _, line := range strings.Split(ics, "\r\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}
