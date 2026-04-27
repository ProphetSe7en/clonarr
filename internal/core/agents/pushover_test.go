package agents

import (
	"strings"
	"testing"
)

// TestPushoverValidate verifies the Pushover provider's Validate logic:
// missing user key / app token and valid config.
func TestPushoverValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing credentials",
			agent:   Agent{Name: "Pushover", Type: "pushover"},
			wantErr: "pushover user key and app token are required",
		},
		{
			name: "valid",
			agent: Agent{Name: "Pushover", Type: "pushover", Config: Config{
				PushoverUserKey:  "user",
				PushoverAppToken: "app",
			}},
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

// TestPushoverMaskAndPreserve verifies the credential mask/preserve round-trip:
// MaskConfigByType replaces both Pushover credentials with placeholders, and
// PreserveConfigByType restores the originals when those placeholders are submitted back.
func TestPushoverMaskAndPreserve(t *testing.T) {
	cfg := Config{PushoverUserKey: "user", PushoverAppToken: "app"}
	masked := MaskConfigByType("pushover", cfg)
	if masked.PushoverUserKey != maskedToken || masked.PushoverAppToken != maskedToken {
		t.Fatalf("pushover credentials not masked")
	}
	restored := PreserveConfigByType("pushover", masked, cfg)
	if restored.PushoverUserKey != cfg.PushoverUserKey || restored.PushoverAppToken != cfg.PushoverAppToken {
		t.Fatalf("pushover credentials not preserved")
	}
}
