package middleware_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rkrimper1/jarvis/gateway/internal/middleware"
)

// ── helpers ───────────────────────────────────────────────────────────

func noop() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

func makeToken(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// ── RequestID ────────────────────────────────────────────────────────

func TestRequestID_GeneratesIDWhenMissing(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	middleware.RequestID(noop()).ServeHTTP(rec, req)

	id := rec.Header().Get(middleware.RequestIDHeader)
	if id == "" {
		t.Error("expected X-Request-ID to be set in response")
	}
}

func TestRequestID_PreservesClientID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(middleware.RequestIDHeader, "client-provided-id")

	middleware.RequestID(noop()).ServeHTTP(rec, req)

	if got := rec.Header().Get(middleware.RequestIDHeader); got != "client-provided-id" {
		t.Errorf("X-Request-ID = %q, want client-provided-id", got)
	}
}

// ── CORS ─────────────────────────────────────────────────────────────

func TestCORS_SetsHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/nlp/parse", nil)
	req.Header.Set("Origin", "https://stark-tower.io")

	middleware.CORS([]string{"*"})(noop()).ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
}

func TestCORS_PreflightReturns204(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/v1/nlp/parse", nil)
	req.Header.Set("Origin", "https://stark-tower.io")

	middleware.CORS([]string{"*"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", rec.Code)
	}
}

// ── Auth ─────────────────────────────────────────────────────────────

const testSecret = "test-jarvis-secret"

func TestAuth_PublicPath_NoToken(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	middleware.Auth(testSecret, []string{"/healthz"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for public path", rec.Code)
	}
}

func TestAuth_PublicPath_Authenticate(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/security/authenticate", nil)

	middleware.Auth(testSecret, []string{"/v1/security/authenticate"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for authenticate endpoint", rec.Code)
	}
}

func TestAuth_MissingToken_Returns401(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/status", nil)

	middleware.Auth(testSecret, []string{"/healthz"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuth_ValidToken_Passes(t *testing.T) {
	token := makeToken("tony-stark:admin,read", testSecret)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	middleware.Auth(testSecret, []string{"/healthz"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for valid token", rec.Code)
	}
}

func TestAuth_InvalidToken_Returns401(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/intel/query", nil)
	req.Header.Set("Authorization", "Bearer totally.invalid.token")

	middleware.Auth(testSecret, []string{"/healthz"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for invalid token", rec.Code)
	}
}

func TestAuth_WrongScheme_Returns401(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/agents/status", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	middleware.Auth(testSecret, []string{"/healthz"})(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for Basic auth scheme", rec.Code)
	}
}

// ── Recovery ─────────────────────────────────────────────────────────

func TestRecovery_CatchesPanic(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong!")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/nlp/parse", nil)

	middleware.Recovery(log)(panicHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 after panic", rec.Code)
	}
}

// ── Logger ────────────────────────────────────────────────────────────

func TestLogger_DoesNotChangeStatus(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/nlp/dialogue", nil)

	middleware.Logger(log)(noop()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("logger changed status to %d", rec.Code)
	}
}
