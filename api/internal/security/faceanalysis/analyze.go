package faceanalysis

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"golang.org/x/image/draw"
)

// FaceResult holds Claude's assessment for a single face.
type FaceResult struct {
	Sentiment  string // one-word emotion label
	Commentary string // short funny comment
}

// Analyzer wraps the Claude client for per-face sentiment + commentary.
type Analyzer struct {
	client    *anthropic.Client
	model     string
	maxTokens int64
}

// NewAnalyzer returns an Analyzer. If apiKey is empty, Analyze falls back to
// stub responses so the feature works without Claude configured.
func NewAnalyzer(apiKey, model string) *Analyzer {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	var c *anthropic.Client
	if apiKey != "" {
		cc := anthropic.NewClient(option.WithAPIKey(apiKey))
		c = &cc
	}
	return &Analyzer{client: c, model: model, maxTokens: 120}
}

const facePrompt = `You are J.A.R.V.I.S., Tony Stark's AI. Analyze the facial expression in this image.
Reply with EXACTLY two lines, nothing else:
SENTIMENT: [one word — e.g. happy, sad, angry, surprised, smug, confused, terrified, bored]
COMMENTARY: [one short funny comment, max 10 words, in JARVIS's dry witty style]`

// Analyze crops the detected face from img, sends it to Claude, and returns
// the sentiment label + commentary. Falls back to stub values on any error.
func (a *Analyzer) Analyze(ctx context.Context, img image.Image, det Detection) FaceResult {
	if a.client == nil {
		return FaceResult{Sentiment: "UNKNOWN", Commentary: "Sir, facial recognition module offline."}
	}

	crop := cropFace(img, det)
	b64, err := encodeBase64PNG(crop)
	if err != nil {
		return stubResult()
	}

	msg, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: a.maxTokens,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64("image/png", b64),
				anthropic.NewTextBlock(facePrompt),
			),
		},
	})
	if err != nil {
		return stubResult()
	}
	if len(msg.Content) == 0 {
		return stubResult()
	}

	return parseResponse(msg.Content[0].Text)
}

func parseResponse(text string) FaceResult {
	res := FaceResult{Sentiment: "UNKNOWN", Commentary: "Expression: indeterminate."}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SENTIMENT:") {
			res.Sentiment = strings.TrimSpace(strings.TrimPrefix(line, "SENTIMENT:"))
		} else if strings.HasPrefix(line, "COMMENTARY:") {
			res.Commentary = strings.TrimSpace(strings.TrimPrefix(line, "COMMENTARY:"))
		}
	}
	return res
}

func stubResult() FaceResult {
	return FaceResult{
		Sentiment:  "NEUTRAL",
		Commentary: "Threat assessment: suspiciously average.",
	}
}

// cropFace extracts the face region from img, resized to 128×128 for the API call.
func cropFace(img image.Image, det Detection) image.Image {
	src := img
	crop := image.NewRGBA(image.Rect(0, 0, det.W, det.H))
	srcRect := image.Rect(det.X, det.Y, det.X+det.W, det.Y+det.H)
	draw.BiLinear.Scale(crop, crop.Bounds(), src, srcRect, draw.Over, nil)

	// Resize to 128×128 to keep the Claude API request small
	dst := image.NewRGBA(image.Rect(0, 0, 128, 128))
	draw.BiLinear.Scale(dst, dst.Bounds(), crop, crop.Bounds(), draw.Over, nil)
	return dst
}

func encodeBase64PNG(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode face PNG: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
