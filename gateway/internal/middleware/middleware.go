// Package middleware provides HTTP middleware for the JARVIS API Gateway.
//
// Middleware chain (outermost → innermost):
//   Recovery → RequestID → Logger → CORS → Auth → Handler
package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Recovery ─────────────────────────────────────────────────────────

// Recovery catches panics in downstream handlers and returns 500.
func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("path", r.URL.Path),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ── Request ID ───────────────────────────────────────────────────────

const RequestIDHeader = "X-Request-ID"

// RequestID injects a unique request ID into every request/response pair.
// Client-supplied IDs are preserved; missing IDs get a new UUID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = uuid.New().String()
		}
		w.Header().Set(RequestIDHeader, id)
		r.Header.Set(RequestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}

// ── Logger ───────────────────────────────────────────────────────────

// Logger records method, path, status, and latency for every request.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			log.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", r.Header.Get(RequestIDHeader)),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// ── CORS ─────────────────────────────────────────────────────────────

// CORS adds Access-Control headers. Pass "*" as origin for local dev.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			for _, o := range allowedOrigins {
				if o == "*" || o == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, Authorization, X-Request-ID, Grpc-Metadata-Request-Id")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── Auth ─────────────────────────────────────────────────────────────

// Auth validates Bearer tokens on all non-public paths.
//
// Token format (matches SecurityService token.Manager):
//   base64url(payload) + "." + base64url(HMAC-SHA256(payload, secret))
//
// This middleware validates the MAC locally — no round-trip to SecurityService
// per request. Add expiry and scope enforcement here as needed.
func Auth(secret string, publicPaths []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health check always passes
			if r.URL.Path == "/healthz" {
				next.ServeHTTP(w, r)
				return
			}
			for _, p := range publicPaths {
				if r.URL.Path == p || strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}

			raw := r.Header.Get("Authorization")
			if raw == "" {
				writeUnauthorized(w, "Authorization header required")
				return
			}
			parts := strings.SplitN(raw, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeUnauthorized(w, "Bearer token required")
				return
			}
			if err := validateToken(parts[1], secret); err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func validateToken(token, secret string) error {
	dot := strings.LastIndex(token, ".")
	if dot < 0 {
		return fmt.Errorf("malformed token")
	}
	payload := token[:dot]
	macB64 := token[dot+1:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(macB64)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", `Bearer realm="jarvis"`)
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
