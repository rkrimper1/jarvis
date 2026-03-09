// Package protocol executes emergency security protocols.
package protocol

import (
	"fmt"
	"log/slog"

	securityv1 "github.com/rkrimper1/jarvis/gen/security"
)

// ExecutionResult describes what happened when a protocol ran.
type ExecutionResult struct {
	Protocol    securityv1.ProtocolType
	ActionsTaken []string
}

// Executor runs security protocols.
type Executor struct {
	log *slog.Logger
}

// New creates a new Executor.
func New(log *slog.Logger) *Executor {
	return &Executor{log: log}
}

// Execute runs the requested protocol and returns a result.
// In production, each case would trigger real side effects:
// calling FacilityService, HardwareService, AgentCoordinator, etc.
// via their gRPC clients. The interfaces are left as log statements
// to keep the service self-contained at this stage.
func (e *Executor) Execute(
	p securityv1.ProtocolType,
	reason string,
) (ExecutionResult, error) {

	switch p {
	case securityv1.ProtocolType_PROTOCOL_TYPE_LOCKDOWN:
		return e.runLockdown(reason)

	case securityv1.ProtocolType_PROTOCOL_TYPE_EVACUATION:
		return e.runEvacuation(reason)

	case securityv1.ProtocolType_PROTOCOL_TYPE_CLEAN_SLATE:
		return e.runCleanSlate(reason)

	case securityv1.ProtocolType_PROTOCOL_TYPE_BLACKOUT:
		return e.runBlackout(reason)

	default:
		return ExecutionResult{}, fmt.Errorf("unknown protocol: %v", p)
	}
}

func (e *Executor) runLockdown(reason string) (ExecutionResult, error) {
	e.log.Warn("PROTOCOL: LOCKDOWN initiated", slog.String("reason", reason))
	actions := []string{
		"all_exterior_doors_sealed",
		"access_tokens_revoked_non_admin",
		"security_cameras_set_to_max_resolution",
		"iron_legion_deployed_to_perimeter",
		"facility_service_lockdown_rpc_called",
	}
	return ExecutionResult{Protocol: securityv1.ProtocolType_PROTOCOL_TYPE_LOCKDOWN, ActionsTaken: actions}, nil
}

func (e *Executor) runEvacuation(reason string) (ExecutionResult, error) {
	e.log.Warn("PROTOCOL: EVACUATION initiated", slog.String("reason", reason))
	actions := []string{
		"evacuation_routes_illuminated",
		"all_personnel_alerted_via_comms",
		"elevator_disabled_stairwells_unlocked",
		"emergency_contacts_notified",
		"headcount_roster_generated",
	}
	return ExecutionResult{Protocol: securityv1.ProtocolType_PROTOCOL_TYPE_EVACUATION, ActionsTaken: actions}, nil
}

func (e *Executor) runCleanSlate(reason string) (ExecutionResult, error) {
	// Clean Slate is the most destructive — log at error to create a hard audit trail
	e.log.Error("PROTOCOL: CLEAN SLATE initiated — irreversible",
		slog.String("reason", reason),
	)
	actions := []string{
		"all_iron_man_suits_self_destruct_sequence_initiated",
		"sensitive_data_wipe_queued",
		"arc_reactor_shutdown_sequence_started",
		"hardware_service_clean_slate_rpc_called",
		"audit_log_encrypted_and_archived",
	}
	return ExecutionResult{Protocol: securityv1.ProtocolType_PROTOCOL_TYPE_CLEAN_SLATE, ActionsTaken: actions}, nil
}

func (e *Executor) runBlackout(reason string) (ExecutionResult, error) {
	e.log.Warn("PROTOCOL: BLACKOUT initiated", slog.String("reason", reason))
	actions := []string{
		"all_external_network_interfaces_disabled",
		"satellite_uplinks_severed",
		"internal_comms_switched_to_air_gap_mode",
		"facility_service_comms_blackout_called",
	}
	return ExecutionResult{Protocol: securityv1.ProtocolType_PROTOCOL_TYPE_BLACKOUT, ActionsTaken: actions}, nil
}
