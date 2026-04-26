package threat

import (
	"testing"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
)

// ── parseThreatLevel ─────────────────────────────────────────────────────────

func TestParseThreatLevel_KnownValues(t *testing.T) {
	cases := []struct {
		input string
		want  securityv1.ThreatLevel
	}{
		{"LOW", securityv1.ThreatLevel_THREAT_LEVEL_LOW},
		{"MODERATE", securityv1.ThreatLevel_THREAT_LEVEL_MODERATE},
		{"HIGH", securityv1.ThreatLevel_THREAT_LEVEL_HIGH},
		{"CRITICAL", securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL},
	}
	for _, tc := range cases {
		got := parseThreatLevel(tc.input)
		if got != tc.want {
			t.Errorf("parseThreatLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseThreatLevel_CaseInsensitive(t *testing.T) {
	cases := []string{"low", "Low", "lOw", "moderate", "Moderate", "high", "High", "critical", "Critical"}
	for _, s := range cases {
		got := parseThreatLevel(s)
		if got == securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED {
			t.Errorf("parseThreatLevel(%q) returned UNSPECIFIED, want a specific level", s)
		}
	}
}

func TestParseThreatLevel_UnknownReturnsUnspecified(t *testing.T) {
	cases := []string{"", "UNKNOWN", "EXTREME", "none", "0", "bogus"}
	for _, s := range cases {
		got := parseThreatLevel(s)
		if got != securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED {
			t.Errorf("parseThreatLevel(%q) = %v, want UNSPECIFIED", s, got)
		}
	}
}

// ── sceneStub ────────────────────────────────────────────────────────────────

func TestSceneStub_ReturnsNonZeroResult(t *testing.T) {
	r := sceneStub()
	if r.Summary == "" {
		t.Error("stub Summary should be non-empty")
	}
	if len(r.Actions) == 0 {
		t.Error("stub Actions should be non-empty")
	}
	if r.Confidence <= 0 {
		t.Error("stub Confidence should be > 0")
	}
}

// ── parseSceneResponse ───────────────────────────────────────────────────────

func TestParseSceneResponse_HappyPath(t *testing.T) {
	input := `LEVEL: HIGH
CONFIDENCE: 0.91
SUMMARY: Armed individual detected at entrance.
ACTION: Lock down the area.
ACTION: Alert security personnel.`

	r := parseSceneResponse(input)
	if r.Level != securityv1.ThreatLevel_THREAT_LEVEL_HIGH {
		t.Errorf("Level = %v, want HIGH", r.Level)
	}
	if r.Confidence < 0.90 || r.Confidence > 0.92 {
		t.Errorf("Confidence = %f, want ~0.91", r.Confidence)
	}
	if r.Summary == "" {
		t.Error("Summary should not be empty")
	}
	if len(r.Actions) != 2 {
		t.Errorf("Actions len = %d, want 2", len(r.Actions))
	}
}

func TestParseSceneResponse_SingleAction(t *testing.T) {
	input := `LEVEL: LOW
CONFIDENCE: 0.55
SUMMARY: No threat detected.
ACTION: Continue monitoring.`

	r := parseSceneResponse(input)
	if r.Level != securityv1.ThreatLevel_THREAT_LEVEL_LOW {
		t.Errorf("Level = %v, want LOW", r.Level)
	}
	if len(r.Actions) != 1 {
		t.Errorf("Actions len = %d, want 1", len(r.Actions))
	}
}

func TestParseSceneResponse_NoActions(t *testing.T) {
	input := `LEVEL: MODERATE
CONFIDENCE: 0.70
SUMMARY: Unidentified visitor at gate.`

	r := parseSceneResponse(input)
	if r.Level != securityv1.ThreatLevel_THREAT_LEVEL_MODERATE {
		t.Errorf("Level = %v, want MODERATE", r.Level)
	}
	if len(r.Actions) != 0 {
		t.Errorf("expected no actions, got %v", r.Actions)
	}
}

func TestParseSceneResponse_ExtraWhitespace(t *testing.T) {
	input := "  LEVEL:   CRITICAL  \n  CONFIDENCE:  1.0  \n  SUMMARY:  Threat confirmed.  \n  ACTION:  Evacuate.  "
	r := parseSceneResponse(input)
	if r.Level != securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL {
		t.Errorf("Level = %v, want CRITICAL", r.Level)
	}
	if r.Confidence != 1.0 {
		t.Errorf("Confidence = %f, want 1.0", r.Confidence)
	}
}

func TestParseSceneResponse_EmptyInput(t *testing.T) {
	r := parseSceneResponse("")
	// Should fall back to stub defaults
	if r.Summary == "" {
		t.Error("empty input should produce stub Summary")
	}
}

func TestParseSceneResponse_MalformedLines(t *testing.T) {
	input := `not a valid line
LEVEL: HIGH
random garbage here
CONFIDENCE: abc
SUMMARY: Valid summary.`

	r := parseSceneResponse(input)
	if r.Level != securityv1.ThreatLevel_THREAT_LEVEL_HIGH {
		t.Errorf("Level = %v, want HIGH", r.Level)
	}
	// Malformed CONFIDENCE: fmt.Sscanf("abc") returns 0, overwriting the stub default
	if r.Confidence != 0.0 {
		t.Errorf("malformed CONFIDENCE: got %f, want 0.0", r.Confidence)
	}
	if r.Summary != "Valid summary." {
		t.Errorf("Summary = %q, want 'Valid summary.'", r.Summary)
	}
}

func TestParseSceneResponse_AllLevelStrings(t *testing.T) {
	levels := []struct {
		text string
		want securityv1.ThreatLevel
	}{
		{"LOW", securityv1.ThreatLevel_THREAT_LEVEL_LOW},
		{"MODERATE", securityv1.ThreatLevel_THREAT_LEVEL_MODERATE},
		{"HIGH", securityv1.ThreatLevel_THREAT_LEVEL_HIGH},
		{"CRITICAL", securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL},
	}
	for _, tc := range levels {
		r := parseSceneResponse("LEVEL: " + tc.text + "\nCONFIDENCE: 0.5\nSUMMARY: test.")
		if r.Level != tc.want {
			t.Errorf("LEVEL %q → %v, want %v", tc.text, r.Level, tc.want)
		}
	}
}

// ── NewSceneAnalyzer ─────────────────────────────────────────────────────────

func TestNewSceneAnalyzer_DefaultModel(t *testing.T) {
	a := NewSceneAnalyzer("fake-key", "")
	if a == nil {
		t.Fatal("expected non-nil SceneAnalyzer")
	}
	if a.model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want default 'claude-sonnet-4-6'", a.model)
	}
}

func TestNewSceneAnalyzer_CustomModel(t *testing.T) {
	a := NewSceneAnalyzer("fake-key", "claude-opus-4-7")
	if a.model != "claude-opus-4-7" {
		t.Errorf("model = %q, want 'claude-opus-4-7'", a.model)
	}
}
