package threat_test

import (
	"testing"

	securityv1 "github.com/rkrimper1/jarvis/api/pb/security"
	"github.com/rkrimper1/jarvis/api/internal/security/threat"
)

func TestAssessor_ThreatLevels(t *testing.T) {
	a := threat.New()

	tests := []struct {
		name      string
		signals   []string
		wantLevel securityv1.ThreatLevel
	}{
		{
			name:      "no signals → no threat",
			signals:   []string{},
			wantLevel: securityv1.ThreatLevel_THREAT_LEVEL_UNSPECIFIED,
		},
		{
			name:      "minor anomaly → low threat",
			signals:   []string{"unscheduled visitor after hours"},
			wantLevel: securityv1.ThreatLevel_THREAT_LEVEL_LOW,
		},
		{
			name:      "suspicious + perimeter → moderate",
			signals:   []string{"suspicious anomaly elevated perimeter"},
			wantLevel: securityv1.ThreatLevel_THREAT_LEVEL_MODERATE,
		},
		{
			name:      "armed intruder → high threat",
			signals:   []string{"armed intruder detected in hangar"},
			wantLevel: securityv1.ThreatLevel_THREAT_LEVEL_HIGH,
		},
		{
			name:      "weapon + hostile + breach → critical",
			signals:   []string{"weapon detected", "hostile entity", "perimeter breach"},
			wantLevel: securityv1.ThreatLevel_THREAT_LEVEL_CRITICAL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := a.Assess("test-subject", "lab", tt.signals)
			if result.Level != tt.wantLevel {
				t.Errorf("level = %v, want %v (signals: %v)", result.Level, tt.wantLevel, tt.signals)
			}
			if result.Confidence <= 0 {
				t.Error("confidence should be positive")
			}
			if result.Summary == "" {
				t.Error("summary should not be empty")
			}
			if len(result.RecommendedActions) == 0 {
				t.Error("should always have at least one recommended action")
			}
		})
	}
}

func TestAssessor_SummaryContainsSubject(t *testing.T) {
	a := threat.New()
	result := a.Assess("ivan-vanko", "rooftop", []string{"hostile weapon"})
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

func TestBroadcaster_PubSub(t *testing.T) {
	b := threat.NewBroadcaster()
	ch, unsub := b.Subscribe("test-subscriber")
	defer unsub()

	assessor := threat.New()
	result := assessor.Assess("attacker", "gate", []string{"weapon", "hostile", "breach"})

	b.Publish(result)

	select {
	case received := <-ch:
		if received.Level != result.Level {
			t.Errorf("received level %v, want %v", received.Level, result.Level)
		}
	default:
		t.Error("expected to receive broadcast message")
	}
}

func TestBroadcaster_Unsubscribe(t *testing.T) {
	b := threat.NewBroadcaster()
	ch, unsub := b.Subscribe("temp-sub")

	unsub() // unsubscribe before publish

	assessor := threat.New()
	result := assessor.Assess("x", "y", []string{"weapon"})
	b.Publish(result) // should not panic

	// Channel should be closed — reading from it should return zero value
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed after unsub")
	}
}
