// Package server — internal white-box tests for parseTextCommand and matchDevice.
// These functions are unexported so we use package server (not server_test).
package server

import (
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/facility/alexa"
)

// ── parseTextCommand ──────────────────────────────────────────────────────────

func TestParseTextCommand_TurnOnPrefix(t *testing.T) {
	action, device := parseTextCommand("turn on office lights")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "office lights" {
		t.Errorf("device: got %q, want %q", device, "office lights")
	}
}

func TestParseTextCommand_TurnOnThePrefix(t *testing.T) {
	action, device := parseTextCommand("turn on the kitchen lamp")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "kitchen lamp" {
		t.Errorf("device: got %q, want %q", device, "kitchen lamp")
	}
}

func TestParseTextCommand_TurnOffPrefix(t *testing.T) {
	action, device := parseTextCommand("turn off bedroom fan")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "bedroom fan" {
		t.Errorf("device: got %q, want %q", device, "bedroom fan")
	}
}

func TestParseTextCommand_TurnOffThePrefix(t *testing.T) {
	action, device := parseTextCommand("turn off the porch light")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "porch light" {
		t.Errorf("device: got %q, want %q", device, "porch light")
	}
}

func TestParseTextCommand_SwitchOnPrefix(t *testing.T) {
	action, device := parseTextCommand("switch on hallway light")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "hallway light" {
		t.Errorf("device: got %q, want %q", device, "hallway light")
	}
}

func TestParseTextCommand_SwitchOnThePrefix(t *testing.T) {
	action, device := parseTextCommand("switch on the hallway light")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "hallway light" {
		t.Errorf("device: got %q, want %q", device, "hallway light")
	}
}

func TestParseTextCommand_SwitchOffPrefix(t *testing.T) {
	action, device := parseTextCommand("switch off garage light")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "garage light" {
		t.Errorf("device: got %q, want %q", device, "garage light")
	}
}

func TestParseTextCommand_SwitchOffThePrefix(t *testing.T) {
	action, device := parseTextCommand("switch off the garage light")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "garage light" {
		t.Errorf("device: got %q, want %q", device, "garage light")
	}
}

// Suffix forms: "<device> on/off/turnOn/turnOff/turn on/turn off"

func TestParseTextCommand_SuffixOn(t *testing.T) {
	action, device := parseTextCommand("office lights on")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "office lights" {
		t.Errorf("device: got %q, want %q", device, "office lights")
	}
}

func TestParseTextCommand_SuffixOff(t *testing.T) {
	action, device := parseTextCommand("office lights off")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "office lights" {
		t.Errorf("device: got %q, want %q", device, "office lights")
	}
}

func TestParseTextCommand_SuffixTurnOn(t *testing.T) {
	action, device := parseTextCommand("desk lamp turn on")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "desk lamp" {
		t.Errorf("device: got %q, want %q", device, "desk lamp")
	}
}

func TestParseTextCommand_SuffixTurnOff(t *testing.T) {
	action, device := parseTextCommand("desk lamp turn off")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "desk lamp" {
		t.Errorf("device: got %q, want %q", device, "desk lamp")
	}
}

func TestParseTextCommand_SuffixTurnOnLower(t *testing.T) {
	action, device := parseTextCommand("bedroom light turnon")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	if device != "bedroom light" {
		t.Errorf("device: got %q, want %q", device, "bedroom light")
	}
}

func TestParseTextCommand_SuffixTurnOffLower(t *testing.T) {
	action, device := parseTextCommand("bedroom light turnoff")
	if action != "turnOff" {
		t.Errorf("action: got %q, want %q", action, "turnOff")
	}
	if device != "bedroom light" {
		t.Errorf("device: got %q, want %q", device, "bedroom light")
	}
}

// Case-insensitivity for prefixes.

func TestParseTextCommand_PrefixCaseInsensitive(t *testing.T) {
	action, device := parseTextCommand("TURN ON Office Lights")
	if action != "turnOn" {
		t.Errorf("action: got %q, want %q", action, "turnOn")
	}
	// Device name preserves original case from input.
	if device != "Office Lights" {
		t.Errorf("device: got %q, want %q", device, "Office Lights")
	}
}

// Empty string — no recognizable action.

func TestParseTextCommand_EmptyString(t *testing.T) {
	action, device := parseTextCommand("")
	if action != "" {
		t.Errorf("action: got %q, want empty", action)
	}
	if device != "" {
		t.Errorf("device: got %q, want empty", device)
	}
}

// No recognizable action — returns ("", trimmed text).

func TestParseTextCommand_NoRecognizableAction(t *testing.T) {
	action, device := parseTextCommand("set brightness to 50")
	if action != "" {
		t.Errorf("action: got %q, want empty", action)
	}
	if device != "set brightness to 50" {
		t.Errorf("device: got %q, want %q", device, "set brightness to 50")
	}
}

func TestParseTextCommand_WhitespaceOnly(t *testing.T) {
	action, device := parseTextCommand("   ")
	if action != "" {
		t.Errorf("action: got %q, want empty", action)
	}
	if device != "" {
		t.Errorf("device: got %q, want empty for whitespace-only input", device)
	}
}

// ── matchDevice ───────────────────────────────────────────────────────────────

func makeSmartHomeDevice(id, displayName string) alexa.SmartHomeDevice {
	return alexa.SmartHomeDevice{ID: id, DisplayName: displayName}
}

func makeEndpointState(id string) (string, alexa.EndpointState) {
	return id, alexa.EndpointState{PowerOn: true, Online: true}
}

func TestMatchDevice_ExactMatch(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{
		"id-1": makeSmartHomeDevice("id-1", "Office Light"),
		"id-2": makeSmartHomeDevice("id-2", "Kitchen Lamp"),
	}
	states := map[string]alexa.EndpointState{"id-1": {}, "id-2": {}}

	id, name := matchDevice("Office Light", smByID, states)
	if id != "id-1" {
		t.Errorf("id: got %q, want %q", id, "id-1")
	}
	if name != "Office Light" {
		t.Errorf("name: got %q, want %q", name, "Office Light")
	}
}

func TestMatchDevice_ExactMatchCaseInsensitive(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{
		"id-1": makeSmartHomeDevice("id-1", "Office Light"),
	}
	states := map[string]alexa.EndpointState{"id-1": {}}

	id, name := matchDevice("office light", smByID, states)
	if id != "id-1" {
		t.Errorf("id: got %q, want %q", id, "id-1")
	}
	if name != "Office Light" {
		t.Errorf("name: got %q, want %q", name, "Office Light")
	}
}

func TestMatchDevice_SubstringMatch(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{
		"id-3": makeSmartHomeDevice("id-3", "Living Room Lamp"),
	}
	states := map[string]alexa.EndpointState{"id-3": {}}

	id, name := matchDevice("room lamp", smByID, states)
	if id != "id-3" {
		t.Errorf("id: got %q, want %q", id, "id-3")
	}
	if name != "Living Room Lamp" {
		t.Errorf("name: got %q, want %q", name, "Living Room Lamp")
	}
}

func TestMatchDevice_NoMatch(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{
		"id-1": makeSmartHomeDevice("id-1", "Office Light"),
	}
	states := map[string]alexa.EndpointState{"id-1": {}}

	id, name := matchDevice("garage fan", smByID, states)
	if id != "" {
		t.Errorf("expected no match, got id=%q", id)
	}
	if name != "" {
		t.Errorf("expected empty name, got %q", name)
	}
}

func TestMatchDevice_EmptyHint(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{
		"id-1": makeSmartHomeDevice("id-1", "Office Light"),
	}
	states := map[string]alexa.EndpointState{"id-1": {}}

	id, name := matchDevice("", smByID, states)
	if id != "" || name != "" {
		t.Errorf("expected empty result for empty hint, got id=%q name=%q", id, name)
	}
}

func TestMatchDevice_OnlyValidEndpointsConsidered(t *testing.T) {
	// "id-2" is NOT in endpointStates (no valid GraphQL endpoint), so it must not match.
	smByID := map[string]alexa.SmartHomeDevice{
		"id-1": makeSmartHomeDevice("id-1", "Garage Light"),
		"id-2": makeSmartHomeDevice("id-2", "Garage Fan"),
	}
	// Only id-1 is a valid endpoint.
	states := map[string]alexa.EndpointState{"id-1": {}}

	id, name := matchDevice("Garage Fan", smByID, states)
	// "Garage Fan" is only in id-2 which is not a valid endpoint — no match.
	if id != "" {
		t.Errorf("expected no match for non-endpoint device, got id=%q name=%q", id, name)
	}
}

func TestMatchDevice_EmptyMaps(t *testing.T) {
	id, name := matchDevice("anything", map[string]alexa.SmartHomeDevice{}, map[string]alexa.EndpointState{})
	if id != "" || name != "" {
		t.Errorf("expected empty result for empty maps, got id=%q name=%q", id, name)
	}
}

func TestMatchDevice_WhitespaceHintTrimmed(t *testing.T) {
	smByID := map[string]alexa.SmartHomeDevice{}
	states := map[string]alexa.EndpointState{}
	id, name := matchDevice("   ", smByID, states)
	if id != "" || name != "" {
		t.Errorf("expected empty for whitespace hint, got id=%q name=%q", id, name)
	}
}
