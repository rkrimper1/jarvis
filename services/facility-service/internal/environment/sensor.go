// Package environment simulates building environment sensor readings.
package environment

import (
	"math"
	"math/rand"
	"time"

	facilityv1 "github.com/rkrimper1/jarvis/gen/facility"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// baseConditions maps zone IDs to their typical environment profile.
var baseConditions = map[string]struct {
	tempC    float32
	humidity float32
	aqi      float32
	comp     string
}{
	"workshop":    {22.0, 45.0, 35.0, "N2:78% O2:21% trace-elements"},
	"lab-01":      {20.0, 40.0, 12.0, "N2:78% O2:21% filtered"},
	"server-room": {18.0, 30.0, 8.0, "N2:78% O2:21% climate-controlled"},
	"hangar-a":    {15.0, 55.0, 42.0, "N2:78% O2:21% ambient"},
	"residence":   {21.0, 50.0, 15.0, "N2:78% O2:21% filtered"},
	"perimeter":   {17.0, 60.0, 55.0, "N2:78% O2:21% outdoor"},
}

// Reading generates a single environment reading for a zone.
func Reading(zoneID string) *facilityv1.EnvironmentReading {
	base, ok := baseConditions[zoneID]
	if !ok {
		base = struct {
			tempC    float32
			humidity float32
			aqi      float32
			comp     string
		}{20.0, 50.0, 25.0, "N2:78% O2:21%"}
	}

	t := float64(time.Now().UnixNano()) / 1e9
	noise := func(n float32) float32 { return n * float32(rand.Float64()-0.5) * 0.1 }
	wave := float32(math.Sin(t/60) * 0.5)

	temp := base.tempC + noise(base.tempC) + wave
	hum := base.humidity + noise(base.humidity)
	aqi := base.aqi + noise(base.aqi)

	anomaly := aqi > 80 || temp > 35 || temp < 5

	return &facilityv1.EnvironmentReading{
		ZoneId:          zoneID,
		TemperatureC:    temp,
		HumidityPct:     hum,
		AirQuality:      aqi,
		Composition:     base.comp,
		AnomalyDetected: anomaly,
		Timestamp:       timestamppb.Now(),
	}
}
