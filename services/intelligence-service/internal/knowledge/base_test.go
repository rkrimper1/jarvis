package knowledge_test

import (
	"testing"

	intelligv1 "github.com/rkrimper1/jarvis/gen/intelligence"
	"github.com/rkrimper1/jarvis/services/intelligence-service/internal/knowledge"
)

func TestBase_QueryExactID(t *testing.T) {
	b := knowledge.New()
	r := b.Query("ivan-vanko", intelligv1.AnalysisDepth_DEPTH_STANDARD)
	if r.ID != "ivan-vanko" {
		t.Errorf("ID = %q, want ivan-vanko", r.ID)
	}
	if len(r.Facts) == 0 {
		t.Error("expected facts for ivan-vanko")
	}
}

func TestBase_QueryFuzzy(t *testing.T) {
	b := knowledge.New()
	r := b.Query("hammer", intelligv1.AnalysisDepth_DEPTH_SURFACE)
	if r.ID == "" {
		t.Error("expected a result for fuzzy query 'hammer'")
	}
}

func TestBase_QueryUnknownSubject(t *testing.T) {
	b := knowledge.New()
	r := b.Query("xyz-unknown-entity-9999", intelligv1.AnalysisDepth_DEPTH_SURFACE)
	if r.Summary == "" {
		t.Error("expected a fallback summary for unknown subject")
	}
}

func TestBase_CrossReference_FindsRelationships(t *testing.T) {
	b := knowledge.New()
	rels := b.CrossReference([]string{"ivan-vanko", "hammer-industries"}, "")
	if len(rels) == 0 {
		t.Error("expected relationships between ivan-vanko and hammer-industries")
	}
}

func TestBase_CrossReference_WithHint(t *testing.T) {
	b := knowledge.New()
	rels := b.CrossReference([]string{"ivan-vanko"}, "hostile")
	for _, r := range rels {
		if r.SubjectA == "" || r.SubjectB == "" {
			t.Error("relationship should have non-empty subjects")
		}
	}
}

func TestAnalyzeArtifact_HostileDescription(t *testing.T) {
	_, _, isHostile, anomalies, elements := knowledge.AnalyzeArtifact(
		"weapon-x", []byte{0x1, 0x2}, "unknown origin weapon device",
	)
	if !isHostile {
		t.Error("expected isHostile=true for weapon description")
	}
	if len(anomalies) == 0 {
		t.Error("expected anomalies for hostile artifact")
	}
	if elements["Iron"] == 0 {
		t.Error("expected Iron in element breakdown")
	}
}

func TestAnalyzeArtifact_KnownTechnology(t *testing.T) {
	_, isKnown, _, _, _ := knowledge.AnalyzeArtifact(
		"shield-tech-1", nil, "stark designed vibranium composite",
	)
	if !isKnown {
		t.Error("expected isKnown=true for stark/vibranium description")
	}
}

func TestAnalyzeArtifact_EmptyScanData(t *testing.T) {
	composition, _, _, _, _ := knowledge.AnalyzeArtifact("benign-device", nil, "standard device")
	if composition == "" {
		t.Error("expected non-empty composition summary")
	}
}
