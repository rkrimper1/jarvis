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
		c := &http.Cookie{
			Name:     e.Name,
			Value:    e.Value,
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
