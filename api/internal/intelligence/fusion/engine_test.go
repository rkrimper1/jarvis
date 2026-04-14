package fusion_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/rkrimper1/jarvis/api/internal/intelligence/fusion"
	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

// anthropicResponse builds a minimal Anthropic Messages API response body
// containing a single text block.
func anthropicResponse(text string) string {
	resp := map[string]any{
		"id":    "msg_test",
		"type":  "message",
		"role":  "assistant",
		"model": "claude-haiku-4-5-20251001",
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 50},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// mockServer starts a test HTTP server that returns statusCode and body for
// every request. The caller must call ts.Close().
func mockServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	}))
	return ts
}

// newTestEngine creates a FusionEngine pointed at the mock server.
func newTestEngine(t *testing.T, ts *httptest.Server) *fusion.AnthropicEngine {
	t.Helper()
	e, err := fusion.New(fusion.Config{
		APIKey:        "test-key",
		Model:         "claude-haiku-4-5-20251001",
		MaxTokens:     512,
		ConfImmediate: 0.90,
		ConfVerify:    0.70,
		ConfReview:    0.50,
	}, option.WithBaseURL(ts.URL+"/"))
	if err != nil {
		t.Fatalf("fusion.New: %v", err)
	}
	return e
}

// ── happy path ────────────────────────────────────────────────────────────────

func TestFuse_ValidJSON(t *testing.T) {
	payload := `{"title":"Acme drops enterprise price","summary":"Acme Corp cut enterprise SaaS pricing by 15%. This directly undercuts Stark Industries' mid-market positioning. Customer retention risk is elevated for renewals this quarter.","opportunity_type":"TACTICAL","confidence_score":0.88,"suggested_action":"Brief the sales team on competitive pricing response before end of day."}`

	ts := mockServer(t, http.StatusOK, anthropicResponse(payload))
	defer ts.Close()

	result, err := newTestEngine(t, ts).Fuse(context.Background(), "Acme cuts prices")
	if err != nil {
		t.Fatalf("Fuse: %v", err)
	}
	if result.Title != "Acme drops enterprise price" {
		t.Errorf("title = %q", result.Title)
	}
	if result.OpportunityType != intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL {
		t.Errorf("opportunity_type = %v, want TACTICAL", result.OpportunityType)
	}
	if result.ConfidenceScore != 0.88 {
		t.Errorf("confidence_score = %v, want 0.88", result.ConfidenceScore)
	}
	if result.SuggestedAction == "" {
		t.Error("expected non-empty suggested_action")
	}
}

func TestFuse_AllOpportunityTypes(t *testing.T) {
	cases := []struct {
		raw  string
		want intelligv1.OpportunityType
	}{
		{"TACTICAL", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL},
		{"STRATEGIC", intelligv1.OpportunityType_OPPORTUNITY_TYPE_STRATEGIC},
		{"RESOURCE", intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE},
		{"THREAT_MITIGATION", intelligv1.OpportunityType_OPPORTUNITY_TYPE_THREAT_MITIGATION},
		{"unknown_value", intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL}, // default
	}
	for _, tc := range cases {
		payload := `{"title":"T","summary":"S","opportunity_type":"` + tc.raw + `","confidence_score":0.7,"suggested_action":"Act"}`
		ts := mockServer(t, http.StatusOK, anthropicResponse(payload))
		result, err := newTestEngine(t, ts).Fuse(context.Background(), "signal")
		ts.Close()
		if err != nil {
			t.Errorf("opportunity_type %q: Fuse error: %v", tc.raw, err)
			continue
		}
		if result.OpportunityType != tc.want {
			t.Errorf("opportunity_type %q: got %v, want %v", tc.raw, result.OpportunityType, tc.want)
		}
	}
}

// ── markdown fence stripping ──────────────────────────────────────────────────

func TestFuse_StripsMarkdownFence(t *testing.T) {
	payload := "```json\n{\"title\":\"T\",\"summary\":\"S\",\"opportunity_type\":\"TACTICAL\",\"confidence_score\":0.75,\"suggested_action\":\"Act\"}\n```"

	ts := mockServer(t, http.StatusOK, anthropicResponse(payload))
	defer ts.Close()

	result, err := newTestEngine(t, ts).Fuse(context.Background(), "signal")
	if err != nil {
		t.Fatalf("Fuse with fenced JSON: %v", err)
	}
	if result.Title != "T" {
		t.Errorf("title = %q, want %q", result.Title, "T")
	}
}

func TestFuse_StripsMarkdownFenceNoLang(t *testing.T) {
	payload := "```\n{\"title\":\"T\",\"summary\":\"S\",\"opportunity_type\":\"STRATEGIC\",\"confidence_score\":0.6,\"suggested_action\":\"Act\"}\n```"

	ts := mockServer(t, http.StatusOK, anthropicResponse(payload))
	defer ts.Close()

	if _, err := newTestEngine(t, ts).Fuse(context.Background(), "signal"); err != nil {
		t.Fatalf("Fuse with bare-fenced JSON: %v", err)
	}
}

// ── error cases ───────────────────────────────────────────────────────────────

func TestFuse_APIError(t *testing.T) {
	ts := mockServer(t, http.StatusInternalServerError,
		`{"type":"error","error":{"type":"api_error","message":"internal server error"}}`)
	defer ts.Close()

	_, err := newTestEngine(t, ts).Fuse(context.Background(), "signal")
	if err == nil {
		t.Error("expected error from 500 response")
	}
}

func TestFuse_InvalidJSON(t *testing.T) {
	ts := mockServer(t, http.StatusOK, anthropicResponse("not valid json at all"))
	defer ts.Close()

	_, err := newTestEngine(t, ts).Fuse(context.Background(), "signal")
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestFuse_MissingRequiredFields(t *testing.T) {
	// title present but summary and suggested_action empty
	ts := mockServer(t, http.StatusOK, anthropicResponse(`{"title":"T","summary":"","opportunity_type":"TACTICAL","confidence_score":0.8,"suggested_action":""}`))
	defer ts.Close()

	_, err := newTestEngine(t, ts).Fuse(context.Background(), "signal")
	if err == nil {
		t.Error("expected error for missing required fields")
	}
}

// ── Config validation ─────────────────────────────────────────────────────────

func TestNew_EmptyAPIKey(t *testing.T) {
	_, err := fusion.New(fusion.Config{})
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected ANTHROPIC_API_KEY error, got %v", err)
	}
}

func TestNew_InvertedThresholds(t *testing.T) {
	cases := []struct {
		name          string
		confImmediate float32
		confVerify    float32
		confReview    float32
	}{
		{"immediate <= verify", 0.70, 0.70, 0.50},
		{"verify <= review", 0.90, 0.50, 0.50},
		{"review <= 0", 0.90, 0.70, -0.01},
	}
	for _, tc := range cases {
		_, err := fusion.New(fusion.Config{
			APIKey:        "key",
			ConfImmediate: tc.confImmediate,
			ConfVerify:    tc.confVerify,
			ConfReview:    tc.confReview,
		})
		if err == nil {
			t.Errorf("%s: expected threshold validation error", tc.name)
		}
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	ts := mockServer(t, http.StatusOK, anthropicResponse(`{"title":"T","summary":"S","opportunity_type":"TACTICAL","confidence_score":0.8,"suggested_action":"Act"}`))
	defer ts.Close()

	// Zero MaxTokens and thresholds — defaults should fill in
	e, err := fusion.New(fusion.Config{APIKey: "key"}, option.WithBaseURL(ts.URL+"/"))
	if err != nil {
		t.Fatalf("New with zero config: %v", err)
	}
	if _, err := e.Fuse(context.Background(), "signal"); err != nil {
		t.Fatalf("Fuse with defaults: %v", err)
	}
}
