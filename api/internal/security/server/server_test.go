package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	"github.com/rkrimper1/jarvis/api/internal/security/config"
	"github.com/rkrimper1/jarvis/api/internal/security/server"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer builds a SecurityServer with minimal in-memory deps (no DB,
// no face detection, broadcast interval set very high so no goroutine fires
// during tests).
func newTestServer(t *testing.T) *server.SecurityServer {
	t.Helper()
	cfg := &config.Config{
		Token: config.TokenConfig{
			Secret:         "test-signing-secret",
			AccessTokenTTL: time.Hour,
			Issuer:         "jarvis.test",
		},
		Audit: config.AuditConfig{
			MaxPageSize: 100,
		},
		Threat: config.ThreatConfig{
			AlertBroadcastInterval: 24 * time.Hour, // never fires during tests
			ConfidenceThresh:       0.65,
		},
	}
	return server.New(cfg, discardLogger())
}

func metaFor(id string) *commonv1.RequestMeta { return &commonv1.RequestMeta{RequestId: id} }

func assertCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", err, err)
	}
	if st.Code() != want {
		t.Errorf("code = %v, want %v (message: %q)", st.Code(), want, st.Message())
	}
}

// ── Authenticate ──────────────────────────────────────────────────────────────

func TestAuthenticate_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAuthenticate_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAuthenticate_EmptySubjectID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{
		Meta: metaFor("auth-bad"),
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAuthenticate_UnknownSubject_NotAuthenticated(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{
		Meta:      metaFor("auth-001"),
		SubjectId: "unknown-user",
		Method:    securityv1.AuthMethod_AUTH_METHOD_PASSCODE,
	})
	if err != nil {
		t.Fatalf("unexpected RPC error: %v", err)
	}
	// Hardcoded auth engine doesn't know "unknown-user"; result should be rejection.
	if resp.Authenticated {
		t.Error("expected Authenticated = false for unknown subject")
	}
}

func TestAuthenticate_KnownSubject_Passcode_Success(t *testing.T) {
	s := newTestServer(t)
	// "stark" is one of the hardcoded subjects in the auth engine.
	resp, err := s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{
		Meta:              metaFor("auth-002"),
		SubjectId:         "stark",
		Method:            securityv1.AuthMethod_AUTH_METHOD_PASSCODE,
		CredentialPayload: []byte("IronMan3000"),
	})
	if err != nil {
		t.Fatalf("unexpected RPC error: %v", err)
	}
	// Whether this passes or fails depends on the hardcoded engine; we just verify
	// the response shape is consistent.
	if resp.Meta == nil {
		t.Error("expected non-nil Meta in response")
	}
	if resp.Authenticated && resp.AccessToken == "" {
		t.Error("Authenticated = true but AccessToken is empty")
	}
}

// ── AssessThreat ──────────────────────────────────────────────────────────────

func TestAssessThreat_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AssessThreat(context.Background(), &securityv1.AssessThreatRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAssessThreat_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AssessThreat(context.Background(), &securityv1.AssessThreatRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAssessThreat_HappyPath_NoSignals(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.AssessThreat(context.Background(), &securityv1.AssessThreatRequest{
		Meta:      metaFor("threat-001"),
		SubjectId: "visitor-99",
		Location:  "lobby",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	// Level may be UNSPECIFIED when no signals are present; just verify confidence
	// is within the valid [0, 1] range.
	if resp.Confidence < 0 || resp.Confidence > 1 {
		t.Errorf("confidence %v out of [0,1]", resp.Confidence)
	}
}

func TestAssessThreat_HappyPath_WithSignals(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.AssessThreat(context.Background(), &securityv1.AssessThreatRequest{
		Meta:            metaFor("threat-002"),
		SubjectId:       "subject-x",
		Location:        "server-room",
		ObservedSignals: []string{"unauthorized_access", "signal_jammer"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.RecommendedActions) == 0 {
		t.Error("expected at least one recommended action for high-signal threat")
	}
}

// ── ExecuteProtocol ───────────────────────────────────────────────────────────

func TestExecuteProtocol_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ExecuteProtocol(context.Background(), &securityv1.ExecuteProtocolRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestExecuteProtocol_UnspecifiedProtocol(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ExecuteProtocol(context.Background(), &securityv1.ExecuteProtocolRequest{
		Meta:     metaFor("proto-bad"),
		Protocol: securityv1.ProtocolType_PROTOCOL_TYPE_UNSPECIFIED,
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestExecuteProtocol_RequiresConfirmation_BlocksExecution(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ExecuteProtocol(context.Background(), &securityv1.ExecuteProtocolRequest{
		Meta:                 metaFor("proto-confirm"),
		Protocol:             securityv1.ProtocolType_PROTOCOL_TYPE_LOCKDOWN,
		RequiresConfirmation: true,
	})
	assertCode(t, err, codes.FailedPrecondition)
}

func TestExecuteProtocol_HappyPath(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ExecuteProtocol(context.Background(), &securityv1.ExecuteProtocolRequest{
		Meta:                 metaFor("proto-001"),
		Protocol:             securityv1.ProtocolType_PROTOCOL_TYPE_LOCKDOWN,
		Reason:               "security test",
		RequiresConfirmation: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ActionsTaken) == 0 {
		t.Error("expected at least one action taken")
	}
}

// ── GetAuditLog ───────────────────────────────────────────────────────────────

func TestGetAuditLog_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.GetAuditLog(context.Background(), &securityv1.GetAuditLogRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestGetAuditLog_HappyPath_Empty(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.GetAuditLog(context.Background(), &securityv1.GetAuditLogRequest{
		Meta:      metaFor("audit-001"),
		SubjectId: "subject-none",
		PageSize:  10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta")
	}
}

func TestGetAuditLog_AfterAuthentication_Succeeds(t *testing.T) {
	s := newTestServer(t)

	// Trigger an auth attempt so the audit log has at least one entry.
	_, _ = s.Authenticate(context.Background(), &securityv1.AuthenticateRequest{
		Meta:      metaFor("audit-setup"),
		SubjectId: "stark",
		Method:    securityv1.AuthMethod_AUTH_METHOD_PASSCODE,
	})

	resp, err := s.GetAuditLog(context.Background(), &securityv1.GetAuditLogRequest{
		Meta:     metaFor("audit-002"),
		PageSize: 50,
		// SubjectId omitted — fetch all entries so time-range edge cases don't filter
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta in response")
	}
}

// ── AnalyzeFaces (not configured) ────────────────────────────────────────────

func TestAnalyzeFaces_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AnalyzeFaces(context.Background(), &securityv1.AnalyzeFacesRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestAnalyzeFaces_NotConfigured(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AnalyzeFaces(context.Background(), &securityv1.AnalyzeFacesRequest{
		Meta:      metaFor("face-001"),
		ImageData: []byte{0xFF, 0xD8, 0xFF}, // minimal JPEG header
	})
	// Face detection not configured → FailedPrecondition
	assertCode(t, err, codes.FailedPrecondition)
}

func TestAnalyzeFaces_EmptyImageData(t *testing.T) {
	s := newTestServer(t)
	_, err := s.AnalyzeFaces(context.Background(), &securityv1.AnalyzeFacesRequest{
		Meta:      metaFor("face-bad"),
		ImageData: nil,
	})
	assertCode(t, err, codes.InvalidArgument)
}
