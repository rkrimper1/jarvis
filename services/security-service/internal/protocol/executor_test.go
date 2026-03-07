package protocol_test

import (
	"log/slog"
	"os"
	"testing"

	securityv1 "github.com/rkrimper1/jarvis/gen/security"
	"github.com/rkrimper1/jarvis/services/security-service/internal/protocol"
)

func newExecutor() *protocol.Executor {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return protocol.New(log)
}

func TestExecute_AllProtocols(t *testing.T) {
	e := newExecutor()

	protocols := []securityv1.ProtocolType{
		securityv1.ProtocolType_PROTOCOL_LOCKDOWN,
		securityv1.ProtocolType_PROTOCOL_EVACUATION,
		securityv1.ProtocolType_PROTOCOL_CLEAN_SLATE,
		securityv1.ProtocolType_PROTOCOL_BLACKOUT,
	}

	for _, p := range protocols {
		t.Run(p.String(), func(t *testing.T) {
			result, err := e.Execute(p, "test reason")
			if err != nil {
				t.Fatalf("Execute(%v): unexpected error: %v", p, err)
			}
			if result.Protocol != p {
				t.Errorf("result.Protocol = %v, want %v", result.Protocol, p)
			}
			if len(result.ActionsTaken) == 0 {
				t.Errorf("expected at least one action taken for protocol %v", p)
			}
		})
	}
}

func TestExecute_UnknownProtocol(t *testing.T) {
	e := newExecutor()
	_, err := e.Execute(securityv1.ProtocolType_PROTOCOL_UNSPECIFIED, "test")
	if err == nil {
		t.Error("expected error for PROTOCOL_UNSPECIFIED, got nil")
	}
}

func TestExecute_LockdownActions(t *testing.T) {
	e := newExecutor()
	result, err := e.Execute(securityv1.ProtocolType_PROTOCOL_LOCKDOWN, "intruder detected")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lockdown must always seal doors and revoke access
	mustContain := []string{"all_exterior_doors_sealed", "iron_legion_deployed_to_perimeter"}
	actionSet := make(map[string]bool)
	for _, a := range result.ActionsTaken {
		actionSet[a] = true
	}
	for _, required := range mustContain {
		if !actionSet[required] {
			t.Errorf("expected action %q in lockdown result", required)
		}
	}
}

func TestExecute_CleanSlateActions(t *testing.T) {
	e := newExecutor()
	result, err := e.Execute(securityv1.ProtocolType_PROTOCOL_CLEAN_SLATE, "Pepper said so")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ActionsTaken) < 4 {
		t.Errorf("Clean Slate should have at least 4 actions, got %d", len(result.ActionsTaken))
	}
}
