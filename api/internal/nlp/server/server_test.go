package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	nlpv1 "github.com/rkrimper1/jarvis/api/pb/nlp"
	commonv1 "github.com/rkrimper1/jarvis/api/pb/common"
	nlpconfig "github.com/rkrimper1/jarvis/api/internal/nlp/config"
	"github.com/rkrimper1/jarvis/api/internal/nlp/server"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer builds an NLPServer pointing Redis at a non-existent address.
// Tests that only exercise ParseIntent (no dialogue) will never touch Redis.
func newTestServer(t *testing.T) *server.NLPServer {
	t.Helper()
	cfg := &nlpconfig.Config{
		Dialogue: nlpconfig.DialogueConfig{
			MaxHistoryTurns:  5,
			SessionTTL:       0,
			ConfidenceThresh: 0.6,
		},
		Claude: nlpconfig.ClaudeConfig{
			APIKey:    "test-fake-key",
			Model:     "claude-test",
			MaxTokens: 100,
		},
		Redis: nlpconfig.RedisConfig{
			Addr: "127.0.0.1:19999", // nothing listening here; OK for ParseIntent tests
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

// ── ParseIntent ───────────────────────────────────────────────────────────────

func TestParseIntent_NilMeta(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{})
	assertCode(t, err, codes.InvalidArgument)
}

func TestParseIntent_EmptyRequestID(t *testing.T) {
	s := newTestServer(t)
	_, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{
		Meta: &commonv1.RequestMeta{},
	})
	assertCode(t, err, codes.InvalidArgument)
}

func TestParseIntent_HappyPath_EmptyText(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{
		Meta:    metaFor("pi-001"),
		RawText: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta == nil {
		t.Error("expected non-nil Meta")
	}
}

func TestParseIntent_HappyPath_KnownText(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{
		Meta:    metaFor("pi-002"),
		RawText: "lock down the facility",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Intent == nlpv1.Intent_INTENT_UNSPECIFIED {
		t.Error("expected a classified intent for lockdown text")
	}
	if resp.Confidence <= 0 {
		t.Error("expected positive confidence")
	}
}

func TestParseIntent_HappyPath_WithContextTags(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{
		Meta:        metaFor("pi-003"),
		RawText:     "status update please",
		ContextTags: []string{"facility", "monitoring"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
}

func TestParseIntent_ResponseHasRequestID(t *testing.T) {
	s := newTestServer(t)
	resp, err := s.ParseIntent(context.Background(), &nlpv1.ParseIntentRequest{
		Meta:    metaFor("pi-id-check"),
		RawText: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Meta.GetRequestId() != "pi-id-check" {
		t.Errorf("request_id = %q, want %q", resp.Meta.GetRequestId(), "pi-id-check")
	}
}
