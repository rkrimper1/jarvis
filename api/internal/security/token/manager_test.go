package token_test

import (
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/security/token"
)

func TestIssueAndValidate(t *testing.T) {
	m := token.New("test-secret", time.Hour, "jarvis.test")

	scopes := []string{"admin", "suit:control"}
	tok, expiresAt, err := m.Issue("tony-stark", scopes)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	if expiresAt.Before(time.Now()) {
		t.Error("expiresAt should be in the future")
	}

	claims, err := m.Validate(tok)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.Subject != "tony-stark" {
		t.Errorf("subject = %q, want %q", claims.Subject, "tony-stark")
	}
	if len(claims.Scopes) != len(scopes) {
		t.Errorf("scopes len = %d, want %d", len(claims.Scopes), len(scopes))
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	m := token.New("test-secret", -time.Second, "jarvis.test") // already expired
	tok, _, err := m.Issue("tony-stark", nil)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	_, err = m.Validate(tok)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestValidate_TamperedToken(t *testing.T) {
	m := token.New("test-secret", time.Hour, "jarvis.test")
	tok, _, _ := m.Issue("tony-stark", nil)

	tampered := tok[:len(tok)-4] + "xxxx"
	_, err := m.Validate(tampered)
	if err == nil {
		t.Fatal("expected error for tampered token, got nil")
	}
}

func TestValidate_WrongSecret(t *testing.T) {
	m1 := token.New("secret-A", time.Hour, "jarvis.test")
	m2 := token.New("secret-B", time.Hour, "jarvis.test")

	tok, _, _ := m1.Issue("tony-stark", nil)
	_, err := m2.Validate(tok)
	if err == nil {
		t.Fatal("expected error when validating with wrong secret")
	}
}

func TestHasScope(t *testing.T) {
	claims := &token.Claims{
		Subject: "tony",
		Scopes:  []string{"suit:control", "facility:read"},
	}

	if !token.HasScope(claims, "suit:control") {
		t.Error("expected HasScope to return true for 'suit:control'")
	}
	if token.HasScope(claims, "intel:all") {
		t.Error("expected HasScope to return false for 'intel:all'")
	}
}

func TestHasScope_AdminBypassesAll(t *testing.T) {
	claims := &token.Claims{
		Subject: "tony",
		Scopes:  []string{"admin"},
	}
	if !token.HasScope(claims, "any:scope:at:all") {
		t.Error("admin should bypass all scope checks")
	}
}
