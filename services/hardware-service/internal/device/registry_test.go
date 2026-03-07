package device_test

import (
	"testing"

	hardwarev1 "github.com/rkrimper1/jarvis/gen/hardware"
	"github.com/rkrimper1/jarvis/services/hardware-service/internal/device"
)

func TestRegistry_GetSeededDevice(t *testing.T) {
	r := device.New()
	d, ok := r.Get("arc-reactor-primary")
	if !ok {
		t.Fatal("expected arc-reactor-primary to be seeded")
	}
	if d.Type != hardwarev1.DeviceType_DEVICE_ARC_REACTOR {
		t.Errorf("type = %v, want DEVICE_ARC_REACTOR", d.Type)
	}
}

func TestRegistry_ExecuteCommand_PowerOn(t *testing.T) {
	r := device.New()
	detail, err := r.ExecuteCommand("mark-ii-suit", "POWER_ON", nil)
	if err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	if detail == "" {
		t.Error("expected non-empty result detail")
	}
	d, _ := r.Get("mark-ii-suit")
	if d.PowerState != hardwarev1.PowerState_POWER_ON {
		t.Errorf("power_state = %v, want POWER_ON", d.PowerState)
	}
}

func TestRegistry_ExecuteCommand_Unknown(t *testing.T) {
	r := device.New()
	_, err := r.ExecuteCommand("mark-ii-suit", "MAKE_COFFEE", nil)
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestRegistry_ExecuteCommand_DeviceNotFound(t *testing.T) {
	r := device.New()
	_, err := r.ExecuteCommand("ghost-device", "POWER_ON", nil)
	if err == nil {
		t.Error("expected error for unknown device")
	}
}

func TestRegistry_RunDiagnostics_HealthyDevice(t *testing.T) {
	r := device.New()
	warnings, errors, score, stats := r.RunDiagnostics("arc-reactor-primary", false)
	if len(errors) > 0 {
		t.Errorf("expected no errors, got: %v", errors)
	}
	if score <= 0 {
		t.Errorf("health score should be > 0, got %.2f", score)
	}
	if stats["device_id"] != "arc-reactor-primary" {
		t.Errorf("stats device_id = %q", stats["device_id"])
	}
	_ = warnings
}

func TestRegistry_RunDiagnostics_DeepScan(t *testing.T) {
	r := device.New()
	_, _, _, stats := r.RunDiagnostics("mark-vii-suit", true)
	if stats["deep_scan"] != "true" {
		t.Error("deep scan stats should include deep_scan=true")
	}
}

func TestRegistry_RunDiagnostics_UnknownDevice(t *testing.T) {
	r := device.New()
	_, errors, score, _ := r.RunDiagnostics("nonexistent", false)
	if len(errors) == 0 {
		t.Error("expected error entry for unknown device")
	}
	if score != 0 {
		t.Errorf("health score should be 0 for unknown device, got %.2f", score)
	}
}

func TestRegistry_List(t *testing.T) {
	r := device.New()
	devices := r.List()
	if len(devices) == 0 {
		t.Error("expected seeded devices in list")
	}
}

func TestRegistry_SetPowerState(t *testing.T) {
	r := device.New()
	if err := r.SetPowerState("hud-mk7", hardwarev1.PowerState_POWER_OFF); err != nil {
		t.Fatalf("SetPowerState: %v", err)
	}
	d, _ := r.Get("hud-mk7")
	if d.PowerState != hardwarev1.PowerState_POWER_OFF {
		t.Errorf("power_state = %v, want POWER_OFF", d.PowerState)
	}
}
