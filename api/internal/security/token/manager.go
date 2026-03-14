// Package token handles creation and validation of JARVIS access tokens.
// Tokens are signed HMAC-SHA256 strings carrying subject, scopes and expiry.
// In production this would be replaced with a proper JWT library or an
// external identity provider (e.g. Google IAM / Workload Identity).
package token

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Claims represents the payload embedded in an access token.
type Claims struct {
	Subject   string    `json:"sub"`
	Scopes    []string  `json:"scopes"`
	IssuedAt  time.Time `json:"iat"`
	ExpiresAt time.Time `json:"exp"`
	Issuer    string    `json:"iss"`
}

// Manager issues and validates access tokens.
type Manager struct {
	secret []byte
	ttl    time.Duration
	issuer string
}

// New creates a new token Manager.
func New(secret string, ttl time.Duration, issuer string) *Manager {
	return &Manager{
		secret: []byte(secret),
		ttl:    ttl,
		issuer: issuer,
	}
}

// Issue creates a signed access token for the given subject and scopes.
func (m *Manager) Issue(subjectID string, scopes []string) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(m.ttl)

	claims := Claims{
		Subject:   subjectID,
		Scopes:    scopes,
		IssuedAt:  now,
		ExpiresAt: expiresAt,
		Issuer:    m.issuer,
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal claims: %w", err)
	}

	// nonce prevents token replay in the same second
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return "", time.Time{}, fmt.Errorf("generate nonce: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(payload) +
		"." + base64.RawURLEncoding.EncodeToString(nonce)

	sig := m.sign(encoded)
	token := encoded + "." + sig

	return token, expiresAt, nil
}

// Validate parses and verifies a token, returning its Claims on success.
func (m *Manager) Validate(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed token: expected 3 parts")
	}

	body := parts[0] + "." + parts[1]
	expectedSig := m.sign(body)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, fmt.Errorf("invalid token signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode token payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("unmarshal claims: %w", err)
	}

	if time.Now().UTC().After(claims.ExpiresAt) {
		return nil, fmt.Errorf("token expired at %s", claims.ExpiresAt.Format(time.RFC3339))
	}

	return &claims, nil
}

// HasScope checks whether a Claims object contains the required scope.
func HasScope(claims *Claims, required string) bool {
	for _, s := range claims.Scopes {
		if s == required || s == "admin" {
			return true
		}
	}
	return false
}

func (m *Manager) sign(data string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
