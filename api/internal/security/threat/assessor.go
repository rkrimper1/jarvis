// Package threat provides threat assessment and live alert broadcasting.
package threat

import (
	"fmt"
	"strings"
	"sync"
	"time"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
)

// AssessmentResult is the output of a threat assessment.
type AssessmentResult struct {
	Level              securityv1.ThreatLevel
	Confidence         float32
	Summary            string
	RecommendedActions []string
}

// Assessor scores threats based on observed signals.
type Assessor struct{}

// New creates a new Assessor.
func New() *Assessor { return &Assessor{} }

// Assess evaluates a subject + signals and returns a threat assessment.
func (a *Assessor) Assess(subjectID, location string, signals []string) AssessmentResult {
	score := 0
	matched := []string{}

	for _, sig := range signals {
		low := strings.ToLower(sig)
		for keyword, weight := range signalWeights {
			if strings.Contains(low, keyword) {
				score += weight
				matched = append(matched, keyword)
			}
		}
	}

	level, confidence := scoreToLevel(score)
	summary := buildSummary(subjectID, location, level, matched)
	actions := recommendActions(level)

	return AssessmentResult{
		Level:              level,
		Confidence:         confidence,
		Summary:            summary,
		RecommendedActions: actions,
	}
}

// signalWeights maps observed signal keywords to threat weight values.
var signalWeights = map[string]int{
	// Critical indicators
	"weapon":         30,
	"explosive":      35,
	"armed":          30,
	"hostile":        25,
	"breach":         25,
	"unauthorised":   20,
	"unauthorized":   20,
	"intruder":       25,
	"hacking":        20,
	"malware":        20,
	"arc reactor":    15, // energy source = potential weapon
	// Moderate indicators
	"unknown":        10,
	"anomaly":        10,
	"suspicious":     10,
	"elevated":       8,
	"perimeter":      5,
	// Low indicators
	"visitor":        2,
	"unscheduled":    3,
	"after hours":    5,
}

func scoreToLevel(score int) (securityv1.ThreatLevel, float32) {
	switch {
	case score >= 80:
		return securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL, 0.95
	case score >= 50:
		return securityv1.ThreatLevel_THREAT_LEVEL_HIGH, 0.85
	case score >= 25:
		return securityv1.ThreatLevel_THREAT_LEVEL_MODERATE, 0.75
	case score >= 5:
		return securityv1.ThreatLevel_THREAT_LEVEL_LOW, 0.65
	default:
		return securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED, 0.90
	}
}

func buildSummary(subjectID, location string, level securityv1.ThreatLevel, signals []string) string {
	if level == securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED {
		return fmt.Sprintf("No threat detected for subject %q at %q.", subjectID, location)
	}
	return fmt.Sprintf(
		"Threat level %s detected for subject %q at %q. Triggered signals: %s.",
		level.String(), subjectID, location, strings.Join(signals, ", "),
	)
}

func recommendActions(level securityv1.ThreatLevel) []string {
	switch level {
	case securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL:
		return []string{
			"engage_lockdown",
			"alert_all_personnel",
			"dispatch_iron_legion",
			"notify_avengers",
			"seal_perimeter",
		}
	case securityv1.ThreatLevel_THREAT_LEVEL_HIGH:
		return []string{
			"restrict_zone_access",
			"alert_security_team",
			"increase_surveillance",
			"prepare_countermeasures",
		}
	case securityv1.ThreatLevel_THREAT_LEVEL_MODERATE:
		return []string{
			"monitor_subject",
			"verify_identity",
			"log_for_review",
		}
	case securityv1.ThreatLevel_THREAT_LEVEL_LOW:
		return []string{
			"log_incident",
			"passive_monitoring",
		}
	default:
		return []string{"no_action_required"}
	}
}

// ── Alert broadcaster ─────────────────────────────────────────────────
// Subscribers receive live threat assessment updates via server-streaming.

// AlertBroadcaster fans out threat assessments to all active subscribers.
type AlertBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]chan AssessmentResult
}

// NewBroadcaster creates a new AlertBroadcaster.
func NewBroadcaster() *AlertBroadcaster {
	return &AlertBroadcaster{
		subscribers: make(map[string]chan AssessmentResult),
	}
}

// Subscribe registers a subscriber and returns a channel + unsubscribe func.
func (b *AlertBroadcaster) Subscribe(id string) (<-chan AssessmentResult, func()) {
	ch := make(chan AssessmentResult, 16)
	b.mu.Lock()
	b.subscribers[id] = ch
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		delete(b.subscribers, id)
		close(ch)
		b.mu.Unlock()
	}
	return ch, unsub
}

// Publish sends an assessment to all current subscribers.
func (b *AlertBroadcaster) Publish(result AssessmentResult) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subscribers {
		select {
		case ch <- result:
		default:
			// subscriber is slow — drop rather than block the broadcaster
		}
	}
}

// SimulatePatrolScan periodically generates synthetic patrol assessments so
// the streaming RPC always has data to push — useful in dev/demo.
func (b *AlertBroadcaster) SimulatePatrolScan(assessor *Assessor, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		zones := []struct {
			subject  string
			location string
			signals  []string
		}{
			{"unknown-drone", "perimeter", []string{"unscheduled", "perimeter"}},
			{"visitor-007", "lobby", []string{"visitor", "after hours"}},
			{"system-health", "server room", []string{"anomaly elevated"}},
		}

		i := 0
		for range ticker.C {
			z := zones[i%len(zones)]
			result := assessor.Assess(z.subject, z.location, z.signals)
			b.Publish(result)
			i++
		}
	}()
}
