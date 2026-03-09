// Package auth handles credential verification for all AuthMethods.
// Each method has its own Verifier. In production each would call a
// dedicated biometric / identity backend. Here they demonstrate the
// interface and testable contract clearly.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	securityv1 "github.com/rkrimper1/jarvis/gen/security"
)

// VerifyResult is the result of a credential verification attempt.
type VerifyResult struct {
	Valid         bool
	GrantedScopes []string
	Reason        string
}

// Engine dispatches credential verification to the correct handler.
type Engine struct {
	knownSubjects map[string]SubjectRecord
}

// SubjectRecord holds the registered credentials for a subject.
type SubjectRecord struct {
	SubjectID    string
	VoicePrint   string // SHA-256 hash of enrolled voiceprint bytes
	RetinalHash  string // SHA-256 hash of enrolled retinal scan bytes
	PasscodeHash string // SHA-256 hash of passcode bytes
	Roles        []string
}

// New creates a new Engine pre-loaded with known subjects.
// In production this reads from a secrets manager or identity DB.
func New() *Engine {
	return &Engine{
		knownSubjects: map[string]SubjectRecord{
			"tony-stark": {
				SubjectID:    "tony-stark",
				VoicePrint:   sha256Hex([]byte("tony-stark-voiceprint")),
				RetinalHash:  sha256Hex([]byte("tony-stark-retina")),
				PasscodeHash: sha256Hex([]byte("ironman3000")),
				Roles:        []string{"admin", "suit:control", "facility:all", "intel:all"},
			},
			"pepper-potts": {
				SubjectID:    "pepper-potts",
				PasscodeHash: sha256Hex([]byte("ceo-stark-industries")),
				Roles:        []string{"business:all", "facility:read"},
			},
			"james-rhodes": {
				SubjectID:    "james-rhodes",
				VoicePrint:   sha256Hex([]byte("rhodey-voiceprint")),
				PasscodeHash: sha256Hex([]byte("warmachine")),
				Roles:        []string{"suit:control", "security:alert"},
			},
		},
	}
}

// Verify checks the credential payload against the stored record for the subject.
func (e *Engine) Verify(
	subjectID string,
	method securityv1.AuthMethod,
	credentialPayload []byte,
) VerifyResult {

	record, ok := e.knownSubjects[subjectID]
	if !ok {
		return VerifyResult{Valid: false, Reason: "unknown subject"}
	}

	payloadHash := sha256Hex(credentialPayload)

	switch method {
	case securityv1.AuthMethod_AUTH_METHOD_VOICE_PRINT:
		if record.VoicePrint == "" {
			return VerifyResult{Valid: false, Reason: "voiceprint not enrolled"}
		}
		if payloadHash != record.VoicePrint {
			return VerifyResult{Valid: false, Reason: "voiceprint mismatch"}
		}

	case securityv1.AuthMethod_AUTH_METHOD_RETINAL_SCAN:
		if record.RetinalHash == "" {
			return VerifyResult{Valid: false, Reason: "retinal scan not enrolled"}
		}
		if payloadHash != record.RetinalHash {
			return VerifyResult{Valid: false, Reason: "retinal scan mismatch"}
		}

	case securityv1.AuthMethod_AUTH_METHOD_PASSCODE:
		if payloadHash != record.PasscodeHash {
			return VerifyResult{Valid: false, Reason: "incorrect passcode"}
		}

	case securityv1.AuthMethod_AUTH_METHOD_BIOMETRIC:
		// Biometric = voice + retinal combined; accept if either passes (demo logic)
		vpOK := record.VoicePrint != "" && payloadHash == record.VoicePrint
		retOK := record.RetinalHash != "" && payloadHash == record.RetinalHash
		if !vpOK && !retOK {
			return VerifyResult{Valid: false, Reason: "biometric verification failed"}
		}

	case securityv1.AuthMethod_AUTH_METHOD_TOKEN:
		// Token validation is handled upstream by the token.Manager.
		// If we reach here the token has already been validated.
		return VerifyResult{Valid: true, GrantedScopes: record.Roles}

	default:
		return VerifyResult{Valid: false, Reason: fmt.Sprintf("unsupported auth method: %v", method)}
	}

	return VerifyResult{Valid: true, GrantedScopes: record.Roles}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
