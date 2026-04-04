package alexa_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/facility/alexa"
)

// ── loadCookies (via LoadCookiesForTest) ──────────────────────────────────────

func TestLoadCookies_MissingFile(t *testing.T) {
	_, err := alexa.LoadCookiesForTest("/no/such/file.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadCookies_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not valid}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := alexa.LoadCookiesForTest(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestLoadCookies_StripsSurroundingQuotes(t *testing.T) {
	// Cookie-Editor sometimes exports values wrapped in double quotes.
	path := writeCookieFile(t, []map[string]any{
		{"name": "tok", "value": `"abc123"`, "domain": ".amazon.com", "path": "/", "expirationDate": 0, "httpOnly": false, "secure": false},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if cookies[0].Value != "abc123" {
		t.Errorf("expected value %q, got %q", "abc123", cookies[0].Value)
	}
}

func TestLoadCookies_ValueWithoutQuotesUnchanged(t *testing.T) {
	path := writeCookieFile(t, []map[string]any{
		{"name": "plain", "value": "noQuotes", "domain": ".amazon.com", "path": "/", "expirationDate": 0, "httpOnly": false, "secure": false},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cookies[0].Value != "noQuotes" {
		t.Errorf("expected %q, got %q", "noQuotes", cookies[0].Value)
	}
}

func TestLoadCookies_SetsExpiresWhenExpirationDatePositive(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	path := writeCookieFile(t, []map[string]any{
		{"name": "session-id", "value": "xyz", "domain": ".amazon.com", "path": "/",
			"expirationDate": float64(future.Unix()), "httpOnly": true, "secure": true},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Expires.IsZero() {
		t.Error("expected Expires to be set for cookie with ExpirationDate > 0")
	}
	diff := c.Expires.Unix() - future.Unix()
	if diff < -2 || diff > 2 {
		t.Errorf("Expires mismatch: got %v, want ~%v", c.Expires.Unix(), future.Unix())
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly=true")
	}
	if !c.Secure {
		t.Error("expected Secure=true")
	}
}

func TestLoadCookies_NoExpiresForSessionCookie(t *testing.T) {
	path := writeCookieFile(t, []map[string]any{
		{"name": "temp", "value": "t", "domain": ".amazon.com", "path": "/",
			"expirationDate": 0, "httpOnly": false, "secure": false},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}
	if !cookies[0].Expires.IsZero() {
		t.Errorf("expected zero Expires for session cookie, got %v", cookies[0].Expires)
	}
}

func TestLoadCookies_EmptyArray(t *testing.T) {
	path := writeCookieFile(t, []map[string]any{})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies, got %d", len(cookies))
	}
}

func TestLoadCookies_MultipleCookies(t *testing.T) {
	future := time.Now().Add(2 * time.Hour)
	path := writeCookieFile(t, []map[string]any{
		{"name": "a", "value": "1", "domain": ".amazon.com", "path": "/",
			"expirationDate": float64(future.Unix()), "httpOnly": false, "secure": true},
		{"name": "b", "value": "2", "domain": ".amazon.com", "path": "/",
			"expirationDate": 0, "httpOnly": true, "secure": false},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}
}

func TestLoadCookies_ValueSingleChar_NoStrip(t *testing.T) {
	// A single-char value that starts/ends with quote — only strip if BOTH ends are quotes.
	path := writeCookieFile(t, []map[string]any{
		{"name": "x", "value": `"`, "domain": ".amazon.com", "path": "/",
			"expirationDate": 0, "httpOnly": false, "secure": false},
	})
	cookies, err := alexa.LoadCookiesForTest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A single `"` character — length is 1, so the len>=2 guard prevents stripping.
	if cookies[0].Value != `"` {
		t.Errorf("expected value unchanged, got %q", cookies[0].Value)
	}
}
