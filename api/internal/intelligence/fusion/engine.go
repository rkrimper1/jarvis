// Package fusion provides the FusionEngine — the AI step that converts a raw
// signal into a structured IntelCard using the Anthropic Claude API.
package fusion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opencensus.io/trace"

	intelligv1 "github.com/rkrimper1/jarvis/api/pb/intelligence"
)

// Engine is the interface for the fusion step. Accepting an interface here
// allows the IngestHandler to be tested with a mock instead of a live API call.
type Engine interface {
	Fuse(ctx context.Context, rawContent string) (*Result, error)
}

// Result holds the structured fields extracted from a raw signal.
type Result struct {
	Title           string
	Summary         string
	OpportunityType intelligv1.OpportunityType
	ConfidenceScore float32
	SuggestedAction string
}

// Config holds tunable parameters for the fusion engine.
// All fields have safe defaults; zero values disable the live API call
// (APIKey == "" → New returns an error).
type Config struct {
	APIKey        string  // ANTHROPIC_API_KEY
	Model         string  // FUSION_MODEL; default: claude-haiku-4-5-20251001
	MaxTokens     int     // FUSION_MAX_TOKENS; default: 512
	ConfImmediate float32 // FUSION_CONF_IMMEDIATE; default: 0.90
	ConfVerify    float32 // FUSION_CONF_VERIFY;    default: 0.70
	ConfReview    float32 // FUSION_CONF_REVIEW;    default: 0.50
}

// defaults fills zero values with safe production defaults.
func (c *Config) defaults() {
	if c.Model == "" {
		c.Model = "claude-haiku-4-5-20251001"
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = 512
	}
	if c.ConfImmediate == 0 {
		c.ConfImmediate = 0.90
	}
	if c.ConfVerify == 0 {
		c.ConfVerify = 0.70
	}
	if c.ConfReview == 0 {
		c.ConfReview = 0.50
	}
}

// AnthropicEngine is the live Engine implementation backed by the Claude API.
type AnthropicEngine struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	prompt    string // pre-built, threshold values already interpolated
}

// New creates an AnthropicEngine from cfg. Additional SDK options (e.g.
// option.WithBaseURL for tests) may be passed as variadic arguments.
// Returns an error if APIKey is empty or thresholds are inverted.
func New(cfg Config, opts ...option.RequestOption) (*AnthropicEngine, error) {
	cfg.defaults()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("fusion: ANTHROPIC_API_KEY is required")
	}
	if err := validateThresholds(cfg); err != nil {
		return nil, err
	}
	allOpts := append([]option.RequestOption{option.WithAPIKey(cfg.APIKey)}, opts...)
	return &AnthropicEngine{
		client:    anthropic.NewClient(allOpts...),
		model:     anthropic.Model(cfg.Model),
		maxTokens: int64(cfg.MaxTokens),
		prompt:    buildPrompt(cfg),
	}, nil
}

// Fuse sends rawContent to Claude and returns a structured Result.
// It always returns a Result on success — even low-confidence signals produce
// a card (operator can dismiss it).
func (e *AnthropicEngine) Fuse(ctx context.Context, rawContent string) (*Result, error) {
	ctx, span := trace.StartSpan(ctx, "jarvis/fusion.Fuse")
	defer span.End()

	msg, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: e.maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: e.prompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(rawContent)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fusion: claude api: %w", err)
	}
	if len(msg.Content) == 0 {
		return nil, fmt.Errorf("fusion: claude returned empty response")
	}

	return parseResponse(msg.Content[0].Text)
}

// ── prompt builder ────────────────────────────────────────────────────────────

const promptTemplate = `You are a business intelligence analyst. Your task is to extract structured intelligence from raw signals and return a single JSON object.

Domain: business and competitive intelligence — market signals, competitor moves, supply chain events, pricing shifts, M&A activity, regulatory changes, and talent movements.

Classify using these opportunity types:
- TACTICAL: near-term action required (competitor product launch, pricing change, key hire, customer win/loss)
- STRATEGIC: long-term positioning signal (market shift, competitor pivot, new market entrant, platform or technology change)
- RESOURCE: supply chain, funding, capacity, or talent availability signal
- THREAT_MITIGATION: regulatory, legal, reputational, or supply disruption risk

Calibrate confidence_score using these thresholds:
- %.2f–1.00  multiple corroborating sources; action recommended immediately
- %.2f–%.2f  single credible source; verify before acting
- %.2f–%.2f  fragmentary signal; flag for human review
- below %.2f  noise or insufficient signal; log but deprioritise

For TACTICAL signals, suggested_action must be achievable within 24 hours.
For all other types, suggested_action should reflect the appropriate time horizon.

Return ONLY a valid JSON object — no markdown, no explanation, no text outside the JSON:
{"title":"<short descriptive title, max 10 words>","summary":"<2–3 sentence factual summary of the signal and its business implication>","opportunity_type":"TACTICAL|STRATEGIC|RESOURCE|THREAT_MITIGATION","confidence_score":0.0,"suggested_action":"<one concrete next step for the operator>"}`

func buildPrompt(cfg Config) string {
	return fmt.Sprintf(promptTemplate,
		cfg.ConfImmediate,
		cfg.ConfVerify, cfg.ConfImmediate,
		cfg.ConfReview, cfg.ConfVerify,
		cfg.ConfReview,
	)
}

// ── response parser ───────────────────────────────────────────────────────────

// rawFusionResponse mirrors the JSON the model is instructed to return.
type rawFusionResponse struct {
	Title           string  `json:"title"`
	Summary         string  `json:"summary"`
	OpportunityType string  `json:"opportunity_type"`
	ConfidenceScore float32 `json:"confidence_score"`
	SuggestedAction string  `json:"suggested_action"`
}

func parseResponse(raw string) (*Result, error) {
	raw = stripMarkdownFence(raw)
	var r rawFusionResponse
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, fmt.Errorf("fusion: parse response: %w (raw: %.200s)", err, raw)
	}
	if r.Title == "" || r.Summary == "" || r.SuggestedAction == "" {
		return nil, fmt.Errorf("fusion: incomplete response — missing required fields")
	}
	return &Result{
		Title:           r.Title,
		Summary:         r.Summary,
		OpportunityType: opportunityTypeFromString(r.OpportunityType),
		ConfidenceScore: r.ConfidenceScore,
		SuggestedAction: r.SuggestedAction,
	}, nil
}

// stripMarkdownFence removes ```json ... ``` wrappers that models sometimes
// include despite being told not to.
func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}
		s = strings.TrimSpace(s)
	}
	return s
}

// ── helpers ───────────────────────────────────────────────────────────────────

func validateThresholds(cfg Config) error {
	if cfg.ConfImmediate <= cfg.ConfVerify {
		return fmt.Errorf("fusion: FUSION_CONF_IMMEDIATE (%.2f) must be greater than FUSION_CONF_VERIFY (%.2f)", cfg.ConfImmediate, cfg.ConfVerify)
	}
	if cfg.ConfVerify <= cfg.ConfReview {
		return fmt.Errorf("fusion: FUSION_CONF_VERIFY (%.2f) must be greater than FUSION_CONF_REVIEW (%.2f)", cfg.ConfVerify, cfg.ConfReview)
	}
	if cfg.ConfReview <= 0 {
		return fmt.Errorf("fusion: FUSION_CONF_REVIEW (%.2f) must be greater than 0", cfg.ConfReview)
	}
	return nil
}

func opportunityTypeFromString(s string) intelligv1.OpportunityType {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "STRATEGIC":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_STRATEGIC
	case "RESOURCE":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_RESOURCE
	case "THREAT_MITIGATION":
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_THREAT_MITIGATION
	default:
		return intelligv1.OpportunityType_OPPORTUNITY_TYPE_TACTICAL
	}
}
