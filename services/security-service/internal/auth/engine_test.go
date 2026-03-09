package auth_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	securityv1 "github.com/rkrimper1/jarvis/gen/security"
	"github.com/rkrimper1/jarvis/services/security-service/internal/auth"
)

func sha256Bytes(s string) []byte {
	// Return raw bytes that, when hashed again by the engine, produce the stored hash.
	// The engine does sha256(payload), so we pass the pre-image directly.
	return []byte(s)
}

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestVerify_VoicePrint_Success(t *testing.T) {
	e := auth.New()
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_VOICE_PRINT, []byte("tony-stark-voiceprint"))
	if !result.Valid {
		t.Errorf("expected valid=true, got reason: %s", result.Reason)
	}
	if len(result.GrantedScopes) == 0 {
		t.Error("expected non-empty scopes")
	}
}

func TestVerify_VoicePrint_Mismatch(t *testing.T) {
	e := auth.New()
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_VOICE_PRINT, []byte("wrong-voice"))
	if result.Valid {
		t.Error("expected valid=false for wrong voiceprint")
	}
}

func TestVerify_Passcode_Success(t *testing.T) {
	e := auth.New()
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_PASSCODE, []byte("ironman3000"))
	if !result.Valid {
		t.Errorf("expected valid=true, got: %s", result.Reason)
	}
}

func TestVerify_Passcode_Wrong(t *testing.T) {
	e := auth.New()
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_PASSCODE, []byte("wrongpassword"))
	if result.Valid {
		t.Error("expected valid=false for wrong passcode")
	}
}

func TestVerify_UnknownSubject(t *testing.T) {
	e := auth.New()
	result := e.Verify("thanos", securityv1.AuthMethod_AUTH_METHOD_PASSCODE, []byte("anything"))
	if result.Valid {
		t.Error("expected valid=false for unknown subject")
	}
	if result.Reason != "unknown subject" {
		t.Errorf("reason = %q, want %q", result.Reason, "unknown subject")
	}
}

func TestVerify_UnenrolledMethod(t *testing.T) {
	e := auth.New()
	// pepper-potts has no voiceprint enrolled
	result := e.Verify("pepper-potts", securityv1.AuthMethod_AUTH_METHOD_VOICE_PRINT, []byte("any"))
	if result.Valid {
		t.Error("expected valid=false for unenrolled method")
	}
}

func TestVerify_TokenMethod(t *testing.T) {
	e := auth.New()
	// AUTH_TOKEN bypasses payload check — assumes upstream validation
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_TOKEN, nil)
	if !result.Valid {
		t.Errorf("expected valid=true for AUTH_TOKEN, got: %s", result.Reason)
	}
}

func TestVerify_AdminHasFullScopes(t *testing.T) {
	e := auth.New()
	result := e.Verify("tony-stark", securityv1.AuthMethod_AUTH_METHOD_PASSCODE, []byte("ironman3000"))
	hasAdmin := false
	for _, s := range result.GrantedScopes {
		if s == "admin" {
			hasAdmin = true
		}
	}
	if !hasAdmin {
		t.Errorf("tony-stark should have 'admin' scope, got: %v", result.GrantedScopes)
	}
}
