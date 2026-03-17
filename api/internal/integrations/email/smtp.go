package email

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
)

// Config holds SMTP connection settings read from environment variables.
type Config struct {
	Host      string // SMTP_HOST  e.g. smtp.gmail.com
	Port      string // SMTP_PORT  e.g. 587
	User      string // SMTP_USER  e.g. rkrimper@gmail.com
	Pass      string // SMTP_PASS  Gmail App Password
	Organizer string // SMTP_TO    organizer's address — always receives a copy
}

// ConfigFromEnv reads SMTP settings from the environment.
// Returns an error if any required variable is missing.
func ConfigFromEnv() (Config, error) {
	c := Config{
		Host:      os.Getenv("SMTP_HOST"),
		Port:      os.Getenv("SMTP_PORT"),
		User:      os.Getenv("SMTP_USER"),
		Pass:      os.Getenv("SMTP_PASS"),
		Organizer: os.Getenv("SMTP_TO"),
	}
	var missing []string
	for k, v := range map[string]string{
		"SMTP_HOST": c.Host,
		"SMTP_PORT": c.Port,
		"SMTP_USER": c.User,
		"SMTP_PASS": c.Pass,
		"SMTP_TO":   c.Organizer,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing SMTP env vars: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

// SendInvite sends a calendar invite to all recipients.
// The organizer (SMTP_TO) is always included even if not in the attendees list.
func SendInvite(cfg Config, subject, icsPayload string, attendees []string) error {
	recipients := mergeRecipients(cfg.Organizer, attendees)

	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)
	addr := cfg.Host + ":" + cfg.Port

	msg, err := buildMIME(cfg.User, recipients, subject, icsPayload)
	if err != nil {
		return fmt.Errorf("build MIME: %w", err)
	}

	return smtp.SendMail(addr, auth, cfg.User, recipients, msg)
}

// mergeRecipients returns a deduplicated list containing the organizer plus all attendees.
func mergeRecipients(organizer string, attendees []string) []string {
	seen := map[string]struct{}{organizer: {}}
	out := []string{organizer}
	for _, a := range attendees {
		if a == "" {
			continue
		}
		if _, ok := seen[a]; !ok {
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	return out
}

// buildMIME assembles a multipart/mixed message with:
//   - text/plain  body
//   - text/calendar attachment (the .ics invite)
func buildMIME(from string, to []string, subject, icsPayload string) ([]byte, error) {
	var buf bytes.Buffer
	boundary := "----=_JarvisInviteBoundary"

	// Headers
	fmt.Fprintf(&buf, "From: %s\r\n", from)
	fmt.Fprintf(&buf, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	buf.WriteString("\r\n")

	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary(boundary); err != nil {
		return nil, err
	}

	// Part 1: plain text body
	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	textHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	textPart, err := mw.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	qpw := quotedprintable.NewWriter(textPart)
	fmt.Fprintf(qpw, "You have a new calendar event: %s\r\nOpen the attached .ics file to add it to your calendar.", subject)
	qpw.Close()

	// Part 2: text/calendar attachment
	calHeader := textproto.MIMEHeader{}
	calHeader.Set("Content-Type", `text/calendar; charset=UTF-8; method=REQUEST`)
	calHeader.Set("Content-Transfer-Encoding", "base64")
	calHeader.Set("Content-Disposition", `attachment; filename="invite.ics"`)
	calPart, err := mw.CreatePart(calHeader)
	if err != nil {
		return nil, err
	}
	enc := base64.NewEncoder(base64.StdEncoding, calPart)
	enc.Write([]byte(icsPayload))
	enc.Close()

	mw.Close()
	return buf.Bytes(), nil
}
