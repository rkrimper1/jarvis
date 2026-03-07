// Package device manages the registry of all hardware devices known to JARVIS.
package device

import (
	"fmt"
	"sync"

	hardwarev1 "github.com/rkrimper1/jarvis/gen/hardware"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Registry is a thread-safe store of Device records.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]*hardwarev1.Device
}

// New creates a Registry pre-seeded with the Iron Man hardware inventory.
func New() *Registry {
	r := &Registry{devices: make(map[string]*hardwarev1.Device)}
	r.seed()
	return r
}

// Get returns a device by ID.
func (r *Registry) Get(id string) (*hardwarev1.Device, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.devices[id]
	return d, ok
}

// List returns all devices, optionally filtered by type.
func (r *Registry) List() []*hardwarev1.Device {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*hardwarev1.Device, 0, len(r.devices))
	for _, d := range r.devices {
		out = append(out, d)
	}
	return out
}

// SetPowerState updates a device's power state.
func (r *Registry) SetPowerState(id string, state hardwarev1.PowerState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return fmt.Errorf("device %q not found", id)
	}
	d.PowerState = state
	d.LastSync = timestamppb.Now()
	return nil
}

// ExecuteCommand applies a command to a device and returns a result detail string.
func (r *Registry) ExecuteCommand(id, command string, params map[string]string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	d, ok := r.devices[id]
	if !ok {
		return "", fmt.Errorf("device %q not found", id)
	}

	switch command {
	case "POWER_ON":
		d.PowerState = hardwarev1.PowerState_POWER_ON
		return fmt.Sprintf("Device %s powered on. All systems nominal.", d.Name), nil
	case "POWER_OFF":
		d.PowerState = hardwarev1.PowerState_POWER_OFF
		return fmt.Sprintf("Device %s powered off gracefully.", d.Name), nil
	case "STANDBY":
		d.PowerState = hardwarev1.PowerState_POWER_STANDBY
		return fmt.Sprintf("Device %s entering standby mode.", d.Name), nil
	case "REBOOT":
		d.PowerState = hardwarev1.PowerState_POWER_STANDBY
		return fmt.Sprintf("Device %s rebooting... estimated time: 30 seconds.", d.Name), nil
	case "REROUTE_POWER":
		target := params["target"]
		return fmt.Sprintf("Power rerouted from %s to %s.", d.Name, target), nil
	case "SELF_DESTRUCT":
		// Intentionally alarming — requires Clean Slate protocol authorisation
		return fmt.Sprintf("Self-destruct sequence initiated on %s. God speed, sir.", d.Name), nil
	default:
		return "", fmt.Errorf("unknown command %q for device %q", command, id)
	}
}

// RunDiagnostics performs a health check on a device.
func (r *Registry) RunDiagnostics(id string, deepScan bool) (warnings, errors []string, score float32, stats map[string]string) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	d, ok := r.devices[id]
	if !ok {
		return nil, []string{fmt.Sprintf("device %q not found", id)}, 0, nil
	}

	stats = map[string]string{
		"device_id":    d.DeviceId,
		"firmware":     d.FirmwareVer,
		"power_state":  d.PowerState.String(),
		"power_level":  fmt.Sprintf("%.1f%%", d.PowerLevel),
		"location":     d.Location,
	}

	score = 1.0
	if d.PowerState == hardwarev1.PowerState_POWER_CRITICAL {
		warnings = append(warnings, "power level critical — immediate recharge recommended")
		score -= 0.4
	}
	if d.PowerLevel < 20 {
		warnings = append(warnings, fmt.Sprintf("low power: %.1f%%", d.PowerLevel))
		score -= 0.2
	}
	if deepScan {
		stats["deep_scan"] = "true"
		stats["integrity_check"] = "passed"
		stats["calibration_status"] = "nominal"
	}
	if score < 0 {
		score = 0
	}
	return warnings, errors, score, stats
}

// seed populates the registry with Tony Stark's hardware inventory.
func (r *Registry) seed() {
	devices := []*hardwarev1.Device{
		{DeviceId: "mark-ii-suit", Name: "Iron Man Mark II", Type: hardwarev1.DeviceType_DEVICE_IRON_SUIT,
			PowerState: hardwarev1.PowerState_POWER_STANDBY, PowerLevel: 100, FirmwareVer: "2.1.0", Location: "hangar-a"},
		{DeviceId: "mark-vii-suit", Name: "Iron Man Mark VII", Type: hardwarev1.DeviceType_DEVICE_IRON_SUIT,
			PowerState: hardwarev1.PowerState_POWER_ON, PowerLevel: 87.5, FirmwareVer: "7.3.2", Location: "hangar-b"},
		{DeviceId: "arc-reactor-primary", Name: "Primary Arc Reactor", Type: hardwarev1.DeviceType_DEVICE_ARC_REACTOR,
			PowerState: hardwarev1.PowerState_POWER_ON, PowerLevel: 98.1, FirmwareVer: "3.0.1", Location: "basement"},
		{DeviceId: "arc-reactor-backup", Name: "Backup Arc Reactor", Type: hardwarev1.DeviceType_DEVICE_ARC_REACTOR,
			PowerState: hardwarev1.PowerState_POWER_STANDBY, PowerLevel: 100, FirmwareVer: "3.0.1", Location: "basement"},
		{DeviceId: "stark-satellite-1", Name: "Stark Satellite Alpha", Type: hardwarev1.DeviceType_DEVICE_SATELLITE,
			PowerState: hardwarev1.PowerState_POWER_ON, PowerLevel: 91.0, FirmwareVer: "1.4.0", Location: "orbit-leo"},
		{DeviceId: "hud-mk7", Name: "HUD Mark VII", Type: hardwarev1.DeviceType_DEVICE_HUD,
			PowerState: hardwarev1.PowerState_POWER_ON, PowerLevel: 99.9, FirmwareVer: "7.1.0", Location: "mark-vii-suit"},
	}
	for _, d := range devices {
		d.LastSync = timestamppb.Now()
		r.devices[d.DeviceId] = d
	}
}
