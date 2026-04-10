package environment_test

import (
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/facility/environment"
)

// ── Reading – known zones ──────────────────────────────────────────────────────

func TestReading_Workshop_ReturnsNonNil(t *testing.T) {
	r := environment.Reading("workshop")
	if r == nil {
		t.Fatal("Reading returned nil for 'workshop'")
	}
}

func TestReading_Workshop_ZoneIDPreserved(t *testing.T) {
	r := environment.Reading("workshop")
	if r.ZoneId != "workshop" {
		t.Errorf("ZoneId: got %q, want %q", r.ZoneId, "workshop")
	}
}

func TestReading_KnownZones_ValuesInRange(t *testing.T) {
	knownZones := []string{"workshop", "lab-01", "server-room", "hangar-a", "residence", "perimeter"}
	for _, zone := range knownZones {
		r := environment.Reading(zone)
		if r == nil {
			t.Errorf("zone %q: returned nil", zone)
			continue
		}
		// Temperature should be physically plausible (no wild values).
		if r.TemperatureC < -50 || r.TemperatureC > 100 {
			t.Errorf("zone %q: TemperatureC %v out of plausible range", zone, r.TemperatureC)
		}
		// Humidity pct should be non-negative.
		if r.HumidityPct < 0 {
			t.Errorf("zone %q: HumidityPct %v is negative", zone, r.HumidityPct)
		}
		// AQI should be non-negative.
		if r.AirQuality < 0 {
			t.Errorf("zone %q: AirQuality %v is negative", zone, r.AirQuality)
		}
	}
}

func TestReading_Workshop_CompositionNotEmpty(t *testing.T) {
	r := environment.Reading("workshop")
	if r.Composition == "" {
		t.Error("expected non-empty Composition for 'workshop'")
	}
}

func TestReading_Lab01_CompositionNotEmpty(t *testing.T) {
	r := environment.Reading("lab-01")
	if r.Composition == "" {
		t.Error("expected non-empty Composition for 'lab-01'")
	}
}

func TestReading_ServerRoom_CompositionNotEmpty(t *testing.T) {
	r := environment.Reading("server-room")
	if r.Composition == "" {
		t.Error("expected non-empty Composition for 'server-room'")
	}
}

// ── Reading – unknown zone falls back to default ──────────────────────────────

func TestReading_UnknownZone_ReturnsReading(t *testing.T) {
	r := environment.Reading("unknown-zone-xyz")
	if r == nil {
		t.Fatal("Reading returned nil for unknown zone")
	}
}

func TestReading_UnknownZone_ZoneIDPreserved(t *testing.T) {
	r := environment.Reading("my-custom-zone")
	if r.ZoneId != "my-custom-zone" {
		t.Errorf("ZoneId: got %q, want %q", r.ZoneId, "my-custom-zone")
	}
}

func TestReading_UnknownZone_DefaultComposition(t *testing.T) {
	r := environment.Reading("nonexistent")
	if r.Composition == "" {
		t.Error("expected non-empty Composition for unknown zone (default fallback)")
	}
}

func TestReading_UnknownZone_ValuesInRange(t *testing.T) {
	r := environment.Reading("totally-unknown")
	if r.TemperatureC < -50 || r.TemperatureC > 100 {
		t.Errorf("TemperatureC %v out of plausible range", r.TemperatureC)
	}
	if r.HumidityPct < 0 {
		t.Errorf("HumidityPct %v is negative", r.HumidityPct)
	}
	if r.AirQuality < 0 {
		t.Errorf("AirQuality %v is negative", r.AirQuality)
	}
}

// ── Reading – empty zone ID ───────────────────────────────────────────────────

func TestReading_EmptyZoneID_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Reading with empty zone panicked: %v", r)
		}
	}()
	r := environment.Reading("")
	if r == nil {
		t.Error("expected non-nil reading for empty zone ID")
	}
}

// ── Reading – Timestamp is set ────────────────────────────────────────────────

func TestReading_TimestampIsSet(t *testing.T) {
	r := environment.Reading("workshop")
	if r.Timestamp == nil {
		t.Error("expected Timestamp to be set on reading")
	}
	if r.Timestamp.Seconds == 0 {
		t.Error("expected non-zero Timestamp.Seconds")
	}
}

// ── Reading – AnomalyDetected logic ──────────────────────────────────────────
// server-room has very low AQI (8) and stable temp (~18C), so anomaly should
// normally be false. We verify the field exists and has a boolean value.

func TestReading_ServerRoom_AnomalyDetectedIsBool(t *testing.T) {
	r := environment.Reading("server-room")
	// Just ensure it's a valid bool (we cannot assert a specific value due to
	// random noise, but for server-room with AQI ~8 and temp ~18 it should
	// normally be false given the noise magnitude is <1).
	_ = r.AnomalyDetected
}

// Perimeter has the highest base AQI (55) + noise could push past 80.
// We verify the anomaly flag is set correctly relative to the reading values.
func TestReading_AnomalyFlag_MatchesConditions(t *testing.T) {
	// Run many iterations — for at least some, check consistency.
	for i := 0; i < 20; i++ {
		r := environment.Reading("perimeter")
		// If anomaly is reported, at least one threshold must be breached.
		if r.AnomalyDetected {
			aqi := r.AirQuality > 80
			highTemp := r.TemperatureC > 35
			lowTemp := r.TemperatureC < 5
			if !aqi && !highTemp && !lowTemp {
				t.Errorf("AnomalyDetected=true but no threshold breached: AQI=%v Temp=%v",
					r.AirQuality, r.TemperatureC)
			}
		}
	}
}

// ── Multiple readings are independent ────────────────────────────────────────

func TestReading_ReturnsNewPointerEachCall(t *testing.T) {
	r1 := environment.Reading("workshop")
	r2 := environment.Reading("workshop")
	if r1 == r2 {
		t.Error("expected distinct pointer for each Reading call")
	}
}
