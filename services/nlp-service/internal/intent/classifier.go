// Package intent provides intent classification for the NLP service.
// In production this would call an LLM or an ML model endpoint.
// For now it uses a rule-based classifier that demonstrates the contract.
package intent

import (
	"strings"

	nlpv1 "github.com/rkrimper1/jarvis/gen/nlp"
)

// ClassifyResult is the output of the classifier.
type ClassifyResult struct {
	Intent     nlpv1.Intent
	Confidence float32
	Canonical  string // normalised command string
}

// Classifier classifies raw utterances into intents.
type Classifier struct{}

// New creates a new Classifier.
func New() *Classifier {
	return &Classifier{}
}

// Classify analyses the raw text and returns a ClassifyResult.
func (c *Classifier) Classify(rawText string, contextTags []string) ClassifyResult {
	text := strings.ToLower(strings.TrimSpace(rawText))

	// Emergency overrides — always highest priority
	if containsAny(text, emergencyKeywords) {
		return ClassifyResult{
			Intent:     nlpv1.Intent_INTENT_EMERGENCY,
			Confidence: 0.97,
			Canonical:  normalise(text),
		}
	}

	// Command intent
	if containsAny(text, commandKeywords) {
		return ClassifyResult{
			Intent:     nlpv1.Intent_INTENT_COMMAND,
			Confidence: scoreByKeywordDensity(text, commandKeywords),
			Canonical:  normalise(text),
		}
	}

	// Analysis / scan request
	if containsAny(text, analysisKeywords) {
		return ClassifyResult{
			Intent:     nlpv1.Intent_INTENT_ANALYSIS_REQUEST,
			Confidence: scoreByKeywordDensity(text, analysisKeywords),
			Canonical:  normalise(text),
		}
	}

	// Query intent
	if containsAny(text, queryKeywords) {
		return ClassifyResult{
			Intent:     nlpv1.Intent_INTENT_QUERY,
			Confidence: scoreByKeywordDensity(text, queryKeywords),
			Canonical:  normalise(text),
		}
	}

	// Small talk
	if containsAny(text, smallTalkKeywords) {
		return ClassifyResult{
			Intent:     nlpv1.Intent_INTENT_SMALL_TALK,
			Confidence: 0.85,
			Canonical:  text,
		}
	}

	return ClassifyResult{
		Intent:     nlpv1.Intent_INTENT_UNKNOWN,
		Confidence: 0.0,
		Canonical:  text,
	}
}

// SuggestActions returns follow-up action suggestions based on intent.
func SuggestActions(intent nlpv1.Intent, entities []string) []string {
	switch intent {
	case nlpv1.Intent_INTENT_COMMAND:
		return []string{"confirm_command", "show_impact_preview", "schedule_instead"}
	case nlpv1.Intent_INTENT_QUERY:
		return []string{"deep_search", "cross_reference", "export_report"}
	case nlpv1.Intent_INTENT_ANALYSIS_REQUEST:
		return []string{"run_deep_scan", "compare_to_baseline", "flag_anomalies"}
	case nlpv1.Intent_INTENT_EMERGENCY:
		return []string{"execute_immediately", "alert_team", "engage_protocol"}
	case nlpv1.Intent_INTENT_SMALL_TALK:
		return []string{"continue_conversation"}
	default:
		return []string{"clarify_intent", "show_help"}
	}
}

// ── keyword tables ────────────────────────────────────────────────────

var emergencyKeywords = []string{
	"emergency", "mayday", "abort", "self-destruct", "evacuate",
	"lockdown", "clean slate", "critical failure", "under attack",
}

var commandKeywords = []string{
	"activate", "deploy", "launch", "power on", "power off", "enable",
	"disable", "start", "stop", "run", "execute", "engage", "disengage",
	"open", "close", "lock", "unlock", "reboot", "shutdown",
}

var analysisKeywords = []string{
	"analyze", "analyse", "scan", "evaluate", "assess", "diagnose",
	"inspect", "check", "detect", "identify", "measure", "calculate",
}

var queryKeywords = []string{
	"what", "who", "where", "when", "how", "tell me", "show me",
	"find", "search", "look up", "status", "report", "list", "get",
}

var smallTalkKeywords = []string{
	"hello", "hi", "hey", "good morning", "good evening",
	"how are you", "thanks", "thank you", "nice", "great",
}

// ── helpers ──────────────────────────────────────────────────────────

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func scoreByKeywordDensity(text string, keywords []string) float32 {
	hits := 0
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			hits++
		}
	}
	base := float32(0.70)
	bonus := float32(hits) * 0.04
	if base+bonus > 0.97 {
		return 0.97
	}
	return base + bonus
}

func normalise(text string) string {
	// trim filler words for a cleaner canonical form
	fillers := []string{"please ", "could you ", "jarvis ", "j.a.r.v.i.s. "}
	result := text
	for _, f := range fillers {
		result = strings.ReplaceAll(result, f, "")
	}
	return strings.TrimSpace(result)
}
