package agents

import (
	"strings"
	"testing"
)

func TestSupportedTypesIncludesBuiltins(t *testing.T) {
	types := SupportedTypes()
	if len(types) < 3 {
		t.Fatalf("expected at least 3 notification providers, got %d", len(types))
	}

	expected := []string{"discord", "gotify", "pushover"}
	for _, typ := range expected {
		if _, ok := GetProvider(typ); !ok {
			t.Fatalf("missing registered notification provider %q", typ)
		}
	}
}

func TestValidateAgentCommon(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name: "missing name",
			agent: Agent{Type: "discord", Config: Config{
				DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
			}},
			wantErr: "name is required",
		},
		{
			name:    "unknown type",
			agent:   Agent{Name: "Custom", Type: "smtp"},
			wantErr: "unknown agent type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgent(tc.agent)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAgent() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAgent() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateAgent() error = %q, want contains %q", err.Error(), tc.wantErr)
			}
		})
	}
}
