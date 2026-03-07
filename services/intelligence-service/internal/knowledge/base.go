// Package knowledge provides the intelligence knowledge base.
// In production this would be backed by a graph DB (e.g. Neo4j or Spanner).
package knowledge

import (
	"fmt"
	"strings"
	"time"

	intelligv1 "github.com/rkrimper1/jarvis/gen/intelligence"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SubjectRecord is an entry in the intelligence database.
type SubjectRecord struct {
	ID       string
	Type     intelligv1.SubjectType
	Summary  string
	Facts    []*intelligv1.IntelFact
	Related  []string
}

// Base is the in-memory knowledge base.
type Base struct {
	subjects map[string]*SubjectRecord
	graph    map[string][]*intelligv1.Relationship // subject_id → relationships
}

// New creates a seeded knowledge base.
func New() *Base {
	b := &Base{
		subjects: make(map[string]*SubjectRecord),
		graph:    make(map[string][]*intelligv1.Relationship),
	}
	b.seed()
	return b
}

// Query searches for subjects matching the query string.
func (b *Base) Query(query string, depth intelligv1.AnalysisDepth) *SubjectRecord {
	lower := strings.ToLower(query)
	// Exact ID match first
	if r, ok := b.subjects[lower]; ok {
		return r
	}
	// Fuzzy name match
	for _, r := range b.subjects {
		if strings.Contains(strings.ToLower(r.Summary), lower) ||
			strings.Contains(r.ID, lower) {
			return r
		}
	}
	// Return a synthesised "no record" entry
	return &SubjectRecord{
		ID:      query,
		Summary: fmt.Sprintf("No intelligence record found for %q. Query logged for background research.", query),
		Facts: []*intelligv1.IntelFact{{
			Source:      "STARK_DB",
			Fact:        "Subject not in database — satellite cross-check initiated.",
			Reliability: 0.5,
			RecordedAt:  timestamppb.Now(),
		}},
	}
}

// CrossReference finds relationships between two or more subjects.
func (b *Base) CrossReference(subjectIDs []string, hint string) []*intelligv1.Relationship {
	var results []*intelligv1.Relationship
	seen := make(map[string]bool)

	for _, id := range subjectIDs {
		for _, rel := range b.graph[id] {
			key := rel.SubjectA + ":" + rel.SubjectB
			if !seen[key] {
				seen[key] = true
				results = append(results, rel)
			}
		}
	}

	// If hint provided, filter to matching relationship type
	if hint != "" {
		filtered := results[:0]
		for _, r := range results {
			if strings.Contains(strings.ToLower(r.RelType), strings.ToLower(hint)) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			results = filtered
		}
	}
	return results
}

// AnalyzeArtifact performs a spectrometric analysis on scan data.
func AnalyzeArtifact(id string, scanData []byte, description string) (
	composition string,
	isKnown bool,
	isHostile bool,
	anomalies []string,
	elements map[string]float64,
) {
	desc := strings.ToLower(description)

	// Crude heuristic — in prod this feeds into an ML model
	isHostile = strings.Contains(desc, "weapon") ||
		strings.Contains(desc, "explosive") ||
		strings.Contains(desc, "unknown origin")

	isKnown = strings.Contains(desc, "stark") ||
		strings.Contains(desc, "shield") ||
		strings.Contains(desc, "vibranium")

	elements = map[string]float64{
		"Iron":      45.2,
		"Titanium":  22.8,
		"Palladium": 8.1,
		"Carbon":    15.4,
		"Unknown":   8.5,
	}
	if len(scanData) > 0 {
		elements["Unknown"] += float64(scanData[0]) / 100.0
	}

	composition = fmt.Sprintf("Primary: Iron-Titanium alloy. Traces of palladium detected. %d-byte scan processed.", len(scanData))
	if isHostile {
		anomalies = append(anomalies, "energy_signature_mismatch", "non-standard_isotope_ratio")
	}
	return
}

func (b *Base) seed() {
	now := func(daysAgo int) *timestamppb.Timestamp {
		return timestamppb.New(time.Now().AddDate(0, 0, -daysAgo))
	}

	records := []*SubjectRecord{
		{
			ID: "ivan-vanko", Type: intelligv1.SubjectType_SUBJECT_PERSON,
			Summary: "Ivan Vanko — Soviet physicist, son of Anton Vanko. Convicted of weapons trafficking. Extreme grudge against Stark family.",
			Facts: []*intelligv1.IntelFact{
				{Source: "SHIELD", Fact: "Defected from Soviet program in 1989. Served 15 years in Kopeisk prison.", Reliability: 0.95, RecordedAt: now(200)},
				{Source: "STARK_DB", Fact: "Built arc-reactor-based weaponry. Threat level HIGH.", Reliability: 0.99, RecordedAt: now(10)},
			},
			Related: []string{"whiplash-tech", "hammer-industries"},
		},
		{
			ID: "hammer-industries", Type: intelligv1.SubjectType_SUBJECT_ORGANIZATION,
			Summary: "Justin Hammer's defense contractor. Known for reverse-engineering Stark technology.",
			Facts: []*intelligv1.IntelFact{
				{Source: "SEC_FILING", Fact: "Revenue $4.2B. Primary DoD contractor.", Reliability: 0.9, RecordedAt: now(30)},
				{Source: "SHIELD", Fact: "Under investigation for weapons smuggling.", Reliability: 0.75, RecordedAt: now(5)},
			},
			Related: []string{"ivan-vanko", "justin-hammer"},
		},
		{
			ID: "vibranium", Type: intelligv1.SubjectType_SUBJECT_TECHNOLOGY,
			Summary: "Wakandan meta-stable element. Unique vibration-dampening and energy-storage properties. Only natural deposit: Wakanda.",
			Facts: []*intelligv1.IntelFact{
				{Source: "STARK_DB", Fact: "Used in Captain America's shield. Tensile strength exceeds all known alloys.", Reliability: 0.99, RecordedAt: now(500)},
			},
			Related: []string{"wakanda", "captain-america"},
		},
	}

	for _, r := range records {
		b.subjects[r.ID] = r
	}

	// Seed relationship graph
	b.graph["ivan-vanko"] = []*intelligv1.Relationship{
		{SubjectA: "ivan-vanko", SubjectB: "hammer-industries", RelType: "allied_with", Strength: 0.85, Evidence: "Monaco Grand Prix attack coordinated with Hammer Industries resources"},
		{SubjectA: "ivan-vanko", SubjectB: "stark-industries", RelType: "hostile_toward", Strength: 0.99, Evidence: "Multiple direct attacks on Tony Stark"},
	}
	b.graph["hammer-industries"] = []*intelligv1.Relationship{
		{SubjectA: "hammer-industries", SubjectB: "ivan-vanko", RelType: "allied_with", Strength: 0.85, Evidence: "Provided lab and resources"},
		{SubjectA: "hammer-industries", SubjectB: "stark-industries", RelType: "competitor", Strength: 0.9, Evidence: "Competing DoD contracts"},
	}
}
