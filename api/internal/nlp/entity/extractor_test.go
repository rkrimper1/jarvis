package entity_test

import (
	"testing"

	"github.com/rkrimper1/jarvis/api/internal/nlp/entity"
)

func TestExtractor_Extract(t *testing.T) {
	e := entity.New()

	tests := []struct {
		name         string
		input        string
		wantTypes    []string
		wantMinCount int
	}{
		{
			name:         "detects system entity",
			input:        "Power up the arc reactor in the lab",
			wantTypes:    []string{"SYSTEM", "LOCATION"},
			wantMinCount: 2,
		},
		{
			name:         "detects person entity",
			input:        "Send a message to Pepper",
			wantTypes:    []string{"PERSON"},
			wantMinCount: 1,
		},
		{
			name:         "detects protocol entity",
			input:        "Initiate clean slate protocol",
			wantTypes:    []string{"PROTOCOL"},
			wantMinCount: 1,
		},
		{
			name:         "detects device entity",
			input:        "Deploy the suit from the hangar",
			wantTypes:    []string{"DEVICE", "LOCATION"},
			wantMinCount: 2,
		},
		{
			name:         "no entities in gibberish",
			input:        "xyzzy flibble blorb",
			wantTypes:    []string{},
			wantMinCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entities := e.Extract(tt.input)

			if len(entities) < tt.wantMinCount {
				t.Errorf("got %d entities, want at least %d (input: %q)", len(entities), tt.wantMinCount, tt.input)
			}

			// Check that all expected types appear at least once
			typeSet := make(map[string]bool)
			for _, ent := range entities {
				typeSet[ent.Type] = true
			}
			for _, wt := range tt.wantTypes {
				if !typeSet[wt] {
					t.Errorf("expected entity type %q not found in results (input: %q)", wt, tt.input)
				}
			}

			// All entities must have a positive score
			for _, ent := range entities {
				if ent.Score <= 0 {
					t.Errorf("entity %q has non-positive score: %f", ent.Value, ent.Score)
				}
			}
		})
	}
}
