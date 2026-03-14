package zone_test

import (
	"testing"

	facilityv1 "github.com/rkrimper1/jarvis/api/pb/facility"
	"github.com/rkrimper1/jarvis/api/internal/facility/zone"
)

func TestStore_GetSeededZone(t *testing.T) {
	s := zone.New()
	z, ok := s.Get("workshop")
	if !ok {
		t.Fatal("expected workshop to be seeded")
	}
	if z.Zone.Type != facilityv1.ZoneType_ZONE_TYPE_WORKSHOP {
		t.Errorf("zone type = %v, want ZONE_WORKSHOP", z.Zone.Type)
	}
}

func TestStore_ControlSystem_On(t *testing.T) {
	s := zone.New()
	newState, _, err := s.ControlSystem("lab-01", facilityv1.SystemType_SYSTEM_TYPE_LIGHTING, "ON", nil)
	if err != nil {
		t.Fatalf("ControlSystem: %v", err)
	}
	if newState != "ON" {
		t.Errorf("newState = %q, want ON", newState)
	}
}

func TestStore_ControlSystem_Toggle(t *testing.T) {
	s := zone.New()
	state1, _, _ := s.ControlSystem("lab-01", facilityv1.SystemType_SYSTEM_TYPE_LIGHTING, "OFF", nil)
	state2, _, _ := s.ControlSystem("lab-01", facilityv1.SystemType_SYSTEM_TYPE_LIGHTING, "TOGGLE", nil)
	if state1 == state2 {
		t.Error("TOGGLE should change state")
	}
}

func TestStore_ControlSystem_Set(t *testing.T) {
	s := zone.New()
	settings := map[string]string{"temperature": "22", "fan_speed": "low"}
	_, _, err := s.ControlSystem("residence", facilityv1.SystemType_SYSTEM_TYPE_HVAC, "SET", settings)
	if err != nil {
		t.Fatalf("ControlSystem SET: %v", err)
	}
}

func TestStore_ControlSystem_UnknownZone(t *testing.T) {
	s := zone.New()
	_, _, err := s.ControlSystem("ghost-zone", facilityv1.SystemType_SYSTEM_TYPE_POWER, "ON", nil)
	if err == nil {
		t.Error("expected error for unknown zone")
	}
}

func TestStore_ControlSystem_UnknownCommand(t *testing.T) {
	s := zone.New()
	_, _, err := s.ControlSystem("workshop", facilityv1.SystemType_SYSTEM_TYPE_LIGHTING, "EXPLODE", nil)
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestStore_ControlSystem_HVAC_SideEffects(t *testing.T) {
	s := zone.New()
	_, effects, err := s.ControlSystem("server-room", facilityv1.SystemType_SYSTEM_TYPE_HVAC, "OFF", nil)
	if err != nil {
		t.Fatalf("ControlSystem: %v", err)
	}
	if len(effects) == 0 {
		t.Error("expected HVAC OFF to produce side effects")
	}
}
