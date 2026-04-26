package threat

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"go.opencensus.io/trace"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
)

// SceneAnalyzer sends a camera frame to Claude Vision and returns a threat assessment.
type SceneAnalyzer struct {
	client    *anthropic.Client
	model     string
	maxTokens int64
}

// NewSceneAnalyzer returns a SceneAnalyzer. apiKey must be non-empty.
func NewSceneAnalyzer(apiKey, model string) *SceneAnalyzer {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	cc := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &SceneAnalyzer{client: &cc, model: model, maxTokens: 512}
}

const scenePrompt = `You are J.A.R.V.I.S., Tony Stark's AI security system. Analyze this camera frame for potential security threats.

Reply with EXACTLY this format, nothing else:
LEVEL: [LOW|MODERATE|HIGH|CRITICAL]
CONFIDENCE: [0.0–1.0]
SUMMARY: [one sentence, max 20 words, JARVIS dry tone]
ACTION: [concise recommended action]
ACTION: [second action if needed, otherwise omit]`

// SceneResult is Claude's assessment of a camera frame.
type SceneResult struct {
	Level       securityv1.ThreatLevel
	Confidence  float32
	Summary     string
	Actions     []string
}

// Analyze sends imageData (raw JPEG/PNG bytes) to Claude and returns a threat assessment.
// detectedObjects is an optional list of COCO-SSD labels to include as context.
func (a *SceneAnalyzer) Analyze(ctx context.Context, imageData []byte, detectedObjects []string, mediaType string) SceneResult {
	ctx, span := trace.StartSpan(ctx, "jarvis/threat.SceneAnalyzer.Analyze")
	defer span.End()

	if mediaType == "" {
		mediaType = "image/jpeg"
	}

	// Build the prompt — include detected objects as context if provided
	prompt := scenePrompt
	if len(detectedObjects) > 0 {
		prompt = fmt.Sprintf("Client-side object detection found: %s.\n\n%s",
			strings.Join(detectedObjects, ", "), scenePrompt)
	}

	b64 := base64.StdEncoding.EncodeToString(imageData)
	msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: a.maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64(mediaType, b64),
				anthropic.NewTextBlock(prompt),
			),
		},
	})
	if err != nil || len(msg.Content) == 0 {
		return sceneStub()
	}

	return parseSceneResponse(msg.Content[0].Text)
}

func parseSceneResponse(text string) SceneResult {
	res := sceneStub()
	res.Actions = nil // cleared so only parsed ACTION lines are returned
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "LEVEL:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "LEVEL:"))
			res.Level = parseThreatLevel(val)
		case strings.HasPrefix(line, "CONFIDENCE:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "CONFIDENCE:"))
			var f float64
			fmt.Sscanf(val, "%f", &f)
			res.Confidence = float32(f)
		case strings.HasPrefix(line, "SUMMARY:"):
			res.Summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
		case strings.HasPrefix(line, "ACTION:"):
			action := strings.TrimSpace(strings.TrimPrefix(line, "ACTION:"))
			if action != "" {
				res.Actions = append(res.Actions, action)
			}
		}
	}
	return res
}

func parseThreatLevel(s string) securityv1.ThreatLevel {
	switch strings.ToUpper(s) {
	case "LOW":
		return securityv1.ThreatLevel_THREAT_LEVEL_LOW
	case "MODERATE":
		return securityv1.ThreatLevel_THREAT_LEVEL_MODERATE
	case "HIGH":
		return securityv1.ThreatLevel_THREAT_LEVEL_HIGH
	case "CRITICAL":
		return securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL
	default:
		return securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED
	}
}

func sceneStub() SceneResult {
	return SceneResult{
		Level:      securityv1.ThreatLevel_THREAT_LEVEL_LOW,
		Confidence: 0.5,
		Summary:    "Sir, threat vision module is processing.",
		Actions:    []string{"Stand by for assessment."},
	}
}
