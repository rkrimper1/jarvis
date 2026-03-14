// Package zone manages building zones and their active systems.
package zone

import (
	"fmt"
	"sync"
	"time"

	facilityv1 "github.com/rkrimper1/jarvis/api/pb/facility"
)

// SystemState holds the current on/off state and settings for one system in a zone.
type SystemState struct {
	Active   bool
	Settings map[string]string
}

// ZoneRecord is the internal zone representation.
type ZoneRecord struct {
	Zone     *facilityv1.Zone
	Systems  map[facilityv1.SystemType]*SystemState
	UpdatedAt time.Time
}

// Store is a thread-safe zone registry.
type Store struct {
	mu    sync.RWMutex
	zones map[string]*ZoneRecord
}

// New creates a Store seeded with Stark Tower / Malibu compound zones.
func New() *Store {
	s := &Store{zones: make(map[string]*ZoneRecord)}
	s.seed()
	return s
}

// Get returns a zone record by ID.
func (s *Store) Get(zoneID string) (*ZoneRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	z, ok := s.zones[zoneID]
	return z, ok
}

// ControlSystem toggles or configures a system within a zone.
// Returns the new state and any side-effects.
func (s *Store) ControlSystem(zoneID string, sys facilityv1.SystemType, command string, settings map[string]string) (newState string, sideEffects []string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	z, ok := s.zones[zoneID]
	if !ok {
		return "", nil, fmt.Errorf("zone %q not found", zoneID)
	}

	state, exists := z.Systems[sys]
	if !exists {
		state = &SystemState{Settings: make(map[string]string)}
		z.Systems[sys] = state
	}

	switch command {
	case "ON":
		state.Active = true
		newState = "ON"
	case "OFF":
		state.Active = false
		newState = "OFF"
	case "TOGGLE":
		state.Active = !state.Active
		if state.Active {
			newState = "ON"
		} else {
			newState = "OFF"
		}
	case "SET":
		state.Active = true
		for k, v := range settings {
			state.Settings[k] = v
		}
		newState = fmt.Sprintf("SET:%v", settings)
	default:
		return "", nil, fmt.Errorf("unknown command %q", command)
	}

	sideEffects = computeSideEffects(sys, newState, z)
	z.UpdatedAt = time.Now()
	return newState, sideEffects, nil
}

func computeSideEffects(sys facilityv1.SystemType, state string, z *ZoneRecord) []string {
	var effects []string
	switch sys {
	case facilityv1.SystemType_SYSTEM_TYPE_HVAC:
		effects = append(effects, "HVAC rebalanced across all floors")
		if state == "OFF" {
			effects = append(effects, "backup ventilation engaged")
		}
	case facilityv1.SystemType_SYSTEM_TYPE_POWER:
		if state == "OFF" {
			effects = append(effects, "emergency lighting activated", "UPS engaged")
		}
	case facilityv1.SystemType_SYSTEM_TYPE_ACCESS_DOORS:
		if state == "OFF" {
			effects = append(effects, "all doors locked", "security notified")
		}
	}
	return effects
}

func (s *Store) seed() {
	zones := []struct {
		id       string
		name     string
		zoneType facilityv1.ZoneType
		building string
		floor    int32
	}{
		{"workshop", "Tony's Workshop", facilityv1.ZoneType_ZONE_TYPE_WORKSHOP, "malibu-mansion", -1},
		{"lab-01", "Research Lab Alpha", facilityv1.ZoneType_ZONE_TYPE_LAB, "stark-tower", 40},
		{"server-room", "Primary Server Room", facilityv1.ZoneType_ZONE_TYPE_SERVER_ROOM, "stark-tower", 38},
		{"hangar-a", "Hangar A", facilityv1.ZoneType_ZONE_TYPE_HANGAR, "malibu-mansion", 0},
		{"residence", "Penthouse Residence", facilityv1.ZoneType_ZONE_TYPE_RESIDENCE, "stark-tower", 93},
		{"perimeter", "Outer Perimeter", facilityv1.ZoneType_ZONE_TYPE_PERIMETER, "malibu-mansion", 0},
	}

	allSystems := []facilityv1.SystemType{
		facilityv1.SystemType_SYSTEM_TYPE_LIGHTING,
		facilityv1.SystemType_SYSTEM_TYPE_HVAC,
		facilityv1.SystemType_SYSTEM_TYPE_POWER,
		facilityv1.SystemType_SYSTEM_TYPE_ACCESS_DOORS,
		facilityv1.SystemType_SYSTEM_TYPE_CAMERAS,
		facilityv1.SystemType_SYSTEM_TYPE_COMMS,
	}

	for _, z := range zones {
		systems := make(map[facilityv1.SystemType]*SystemState)
		activeNames := make([]string, 0, len(allSystems))
		for _, sys := range allSystems {
			systems[sys] = &SystemState{Active: true, Settings: make(map[string]string)}
			activeNames = append(activeNames, sys.String())
		}
		s.zones[z.id] = &ZoneRecord{
			Zone: &facilityv1.Zone{
				ZoneId:        z.id,
				Name:          z.name,
				Type:          z.zoneType,
				BuildingId:    z.building,
				Floor:         z.floor,
				ActiveSystems: activeNames,
			},
			Systems:   systems,
			UpdatedAt: time.Now(),
		}
	}
}
