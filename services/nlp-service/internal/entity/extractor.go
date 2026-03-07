// Package entity provides named entity recognition for the NLP service.
// Extracts PERSON, LOCATION, SYSTEM, DEVICE, PROTOCOL entities from text.
package entity

import (
	"strings"

	nlpv1 "github.com/rkrimper1/jarvis/gen/nlp"
)

// Extractor extracts named entities from raw text.
type Extractor struct{}

// New creates a new Extractor.
func New() *Extractor {
	return &Extractor{}
}

// Extract returns all recognised entities from the given text.
func (e *Extractor) Extract(text string) []*nlpv1.Entity {
	lower := strings.ToLower(text)
	var entities []*nlpv1.Entity

	entities = append(entities, matchEntities(lower, knownSystems, "SYSTEM", 0.90)...)
	entities = append(entities, matchEntities(lower, knownDevices, "DEVICE", 0.88)...)
	entities = append(entities, matchEntities(lower, knownLocations, "LOCATION", 0.85)...)
	entities = append(entities, matchEntities(lower, knownPeople, "PERSON", 0.92)...)
	entities = append(entities, matchEntities(lower, knownProtocols, "PROTOCOL", 0.95)...)

	return entities
}

// ── known entity tables ───────────────────────────────────────────────
// In production, these would come from a knowledge-graph or vector store.

var knownSystems = []string{
	"arc reactor", "repulsor", "Friday", "mark ii", "mark iii",
	"iron legion", "hulkbuster", "stark tower", "avengers database",
	"shield network", "jarvis", "clean slate",
}

var knownDevices = []string{
	"iron man suit", "suit", "gauntlet", "drone",
	"satellite", "turret", "hud", "helmet",
}

var knownLocations = []string{
	"malibu", "stark tower", "avengers compound", "workshop",
	"lab", "hangar", "rooftop", "server room", "basement",
}

var knownPeople = []string{
	"tony", "stark", "pepper", "rhodey", "happy", "nick fury",
	"natasha", "bruce", "thor", "steve", "ultron", "thanos",
}

var knownProtocols = []string{
	"lockdown", "clean slate", "evacuation", "blackout",
	"house party", "mark protocol", "endgame",
}

// ── helpers ───────────────────────────────────────────────────────────

func matchEntities(text string, table []string, entityType string, baseScore float32) []*nlpv1.Entity {
	var found []*nlpv1.Entity
	for _, term := range table {
		if strings.Contains(text, strings.ToLower(term)) {
			found = append(found, &nlpv1.Entity{
				Type:  entityType,
				Value: term,
				Score: baseScore,
			})
		}
	}
	return found
}
