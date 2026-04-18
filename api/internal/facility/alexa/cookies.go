// Package alexa provides an HTTP client for the alexa.amazon.com internal web API.
// Authentication is cookie-based, using a session exported from a logged-in browser
// via the Cookie-Editor extension (https://cookie-editor.com).
package alexa

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// cookieEntry matches the JSON structure exported by the Cookie-Editor browser extension.
type cookieEntry struct {
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Domain         string  `json:"domain"`
	Path           string  `json:"path"`
	ExpirationDate float64 `json:"expirationDate"`
	HTTPOnly       bool    `json:"httpOnly"`
	Secure         bool    `json:"secure"`
}

// CookiesExpiry returns the latest expiration time across all cookies in the
// Cookie-Editor export at path. Using the latest (rather than earliest) means
// a fully-expired session is correctly reported: when every cookie has expired
// the returned time is in the past and callers can detect expired = t < now.
// Returns zero time if no cookies carry an expiration date (session cookies only).
func CookiesExpiry(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("read cookie file %s: %w", path, err)
	}
	var entries []cookieEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return time.Time{}, fmt.Errorf("parse cookie file: %w", err)
	}
	var latest time.Time
	for _, e := range entries {
		if e.ExpirationDate == 0 {
			continue // session cookie — no fixed expiry
		}
		t := time.Unix(int64(e.ExpirationDate), 0)
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}
	return latest, nil
}

// loadCookies reads a Cookie-Editor JSON export file and returns net/http cookies.
func loadCookies(path string) ([]*http.Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cookie file %s: %w", path, err)
	}
	var entries []cookieEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse cookie file: %w", err)
	}
	cookies := make([]*http.Cookie, 0, len(entries))
	for _, e := range entries {
		// Go's net/http rejects cookie values containing double-quotes (RFC 6265
		// allows quoted-string syntax but Go is strict). Strip surrounding quotes.
		val := e.Value
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		c := &http.Cookie{
			Name:     e.Name,
			Value:    val,
			Domain:   e.Domain,
			Path:     e.Path,
			Secure:   e.Secure,
			HttpOnly: e.HTTPOnly,
		}
		if e.ExpirationDate > 0 {
			c.Expires = time.Unix(int64(e.ExpirationDate), 0)
		}
		cookies = append(cookies, c)
	}
	return cookies, nil
}
