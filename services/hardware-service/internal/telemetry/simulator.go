// Package telemetry simulates live sensor readings from hardware devices.
package telemetry

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	commonv1 "github.com/rkrimper1/jarvis/gen/common"
	hardwarev1 "github.com/rkrimper1/jarvis/gen/hardware"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Stream emits a sequence of TelemetryReadings for a given device.
// It closes the returned channel when ctx is done or count readings have been sent.
// count=0 streams indefinitely until the channel is no longer consumed.
func Stream(deviceID string, interval time.Duration, out chan<- *hardwarev1.TelemetryReading) {
	sensors := sensorsFor(deviceID)
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			for _, s := range sensors {
				reading := simulate(deviceID, s)
				select {
				case out <- reading:
				default:
					// subscriber too slow — drop reading
				}
			}
		}
	}()
}

// Reading generates a single on-demand reading for a device/sensor pair.
func Reading(deviceID, sensorType string) *hardwarev1.TelemetryReading {
	return simulate(deviceID, sensorType)
}

// ── sensor tables ─────────────────────────────────────────────────────

func sensorsFor(deviceID string) []string {
	switch {
	case len(deviceID) >= 4 && deviceID[:4] == "mark":
		return []string{"ALTITUDE", "SPEED", "TEMPERATURE", "POWER", "REPULSOR_CHARGE"}
	case len(deviceID) >= 11 && deviceID[:11] == "arc-reactor":
		return []string{"OUTPUT_TERAJOULES", "TEMPERATURE", "STABILITY"}
	case len(deviceID) >= 10 && deviceID[:10] == "stark-sate":
		return []string{"SIGNAL_STRENGTH", "ORBIT_ALTITUDE", "SOLAR_POWER"}
	default:
		return []string{"TEMPERATURE", "POWER"}
	}
}

type sensorSpec struct {
	unit  string
	base  float64
	noise float64
	warn  float64 // value above which we emit a WARNING
	crit  float64 // value above which we emit CRITICAL
}

var sensorSpecs = map[string]sensorSpec{
	"ALTITUDE":           {unit: "m", base: 3000, noise: 150, warn: 8000, crit: 12000},
	"SPEED":              {unit: "km/h", base: 400, noise: 50, warn: 1500, crit: 2500},
	"TEMPERATURE":        {unit: "°C", base: 22, noise: 3, warn: 80, crit: 120},
	"POWER":              {unit: "%", base: 85, noise: 5, warn: 20, crit: 5},
	"REPULSOR_CHARGE":    {unit: "%", base: 90, noise: 8, warn: 30, crit: 10},
	"OUTPUT_TERAJOULES":  {unit: "TJ", base: 3.5, noise: 0.1, warn: 4.8, crit: 5.5},
	"STABILITY":          {unit: "%", base: 99, noise: 0.5, warn: 90, crit: 80},
	"SIGNAL_STRENGTH":    {unit: "dBm", base: -45, noise: 5, warn: -85, crit: -100},
	"ORBIT_ALTITUDE":     {unit: "km", base: 550, noise: 2, warn: 400, crit: 300},
	"SOLAR_POWER":        {unit: "%", base: 78, noise: 10, warn: 30, crit: 10},
}

var readingCounter int64

func simulate(deviceID, sensorType string) *hardwarev1.TelemetryReading {
	readingCounter++
	spec, ok := sensorSpecs[sensorType]
	if !ok {
		spec = sensorSpec{unit: "raw", base: 50, noise: 10, warn: 90, crit: 99}
	}

	// Add a subtle sine wave to make the data look more realistic
	t := float64(time.Now().UnixNano()) / 1e9
	value := spec.base + spec.noise*(rand.Float64()-0.5)*2 + math.Sin(t/30)*spec.noise*0.3

	alert := commonv1.Severity_SEVERITY_INFO
	if value >= spec.crit {
		alert = commonv1.Severity_SEVERITY_CRITICAL
	} else if value >= spec.warn {
		alert = commonv1.Severity_SEVERITY_WARNING
	}

	return &hardwarev1.TelemetryReading{
		DeviceId:   deviceID,
		SensorType: sensorType,
		Value:      value,
		Unit:       spec.unit,
		AlertLevel: alert,
		Timestamp:  timestamppb.Now(),
	}
}

// EnergySourceScan simulates a scan for energy sources near a location.
func EnergySourceScan(location string, radiusKm float32) []*hardwarev1.EnergySource {
	// Seed deterministically so the same location always returns the same sources
	sources := []*hardwarev1.EnergySource{
		{SourceId: fmt.Sprintf("src-%s-arc-1", location), Type: "ARC",
			PowerLevel: 3.5 + rand.Float32()*0.5, Location: location, IsHostile: false},
		{SourceId: fmt.Sprintf("src-%s-unknown-1", location), Type: "UNKNOWN",
			PowerLevel: 0.8 + rand.Float32()*0.4, Location: location + "-perimeter", IsHostile: true},
	}
	if radiusKm > 10 {
		sources = append(sources, &hardwarev1.EnergySource{
			SourceId: fmt.Sprintf("src-%s-nuclear-1", location), Type: "NUCLEAR",
			PowerLevel: 12.0 + rand.Float32()*2, Location: location + "-offshore", IsHostile: false,
		})
	}
	return sources
}
