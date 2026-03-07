package intent_test

import (
	"testing"

	nlpv1 "github.com/rkrimper1/jarvis/gen/nlp"
	"github.com/rkrimper1/jarvis/services/nlp-service/internal/intent"
)

func TestClassifier_Classify(t *testing.T) {
	c := intent.New()

	tests := []struct {
		name           string
		input          string
		wantIntent     nlpv1.Intent
		minConfidence  float32
	}{
		{
			name:          "emergency keyword triggers emergency intent",
			input:         "Mayday! We are under attack!",
			wantIntent:    nlpv1.Intent_INTENT_EMERGENCY,
			minConfidence: 0.90,
		},
		{
			name:          "activate command",
			input:         "Activate the Iron Legion",
			wantIntent:    nlpv1.Intent_INTENT_COMMAND,
			minConfidence: 0.70,
		},
		{
			name:          "scan request maps to analysis",
			input:         "Scan the artifact for energy signatures",
			wantIntent:    nlpv1.Intent_INTENT_ANALYSIS_REQUEST,
			minConfidence: 0.70,
		},
		{
			name:          "status query",
			input:         "What is the status of the arc reactor?",
			wantIntent:    nlpv1.Intent_INTENT_QUERY,
			minConfidence: 0.70,
		},
		{
			name:          "greeting small talk",
			input:         "Hello Jarvis",
			wantIntent:    nlpv1.Intent_INTENT_SMALL_TALK,
			minConfidence: 0.80,
		},
		{
			name:          "jarvis prefix stripped for canonical",
			input:         "Jarvis activate the repulsor",
			wantIntent:    nlpv1.Intent_INTENT_COMMAND,
			minConfidence: 0.70,
		},
		{
			name:          "unknown input returns unknown intent",
			input:         "xyzzy blorp flibble",
			wantIntent:    nlpv1.Intent_INTENT_UNKNOWN,
			minConfidence: 0.0,
		},
		{
			name:          "lockdown emergency",
			input:         "Initiate lockdown now",
			wantIntent:    nlpv1.Intent_INTENT_EMERGENCY,
			minConfidence: 0.90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := c.Classify(tt.input, nil)

			if result.Intent != tt.wantIntent {
				t.Errorf("intent = %v, want %v (input: %q)", result.Intent, tt.wantIntent, tt.input)
			}
			if result.Confidence < tt.minConfidence {
				t.Errorf("confidence = %.2f, want >= %.2f (input: %q)",
					result.Confidence, tt.minConfidence, tt.input)
			}
			if result.Canonical == "" {
				t.Errorf("canonical should not be empty (input: %q)", tt.input)
			}
		})
	}
}

func TestSuggestActions(t *testing.T) {
	tests := []struct {
		intent      nlpv1.Intent
		wantNonEmpty bool
	}{
		{nlpv1.Intent_INTENT_COMMAND, true},
		{nlpv1.Intent_INTENT_QUERY, true},
		{nlpv1.Intent_INTENT_ANALYSIS_REQUEST, true},
		{nlpv1.Intent_INTENT_EMERGENCY, true},
		{nlpv1.Intent_INTENT_SMALL_TALK, true},
		{nlpv1.Intent_INTENT_UNKNOWN, true},
	}

	for _, tt := range tests {
		t.Run(tt.intent.String(), func(t *testing.T) {
			suggestions := intent.SuggestActions(tt.intent, nil)
			if tt.wantNonEmpty && len(suggestions) == 0 {
				t.Errorf("expected non-empty suggestions for intent %v", tt.intent)
			}
		})
	}
}
