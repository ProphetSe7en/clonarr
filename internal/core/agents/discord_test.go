package agents

import (
	"strings"
	"testing"
)

func TestDiscordValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing webhook",
			agent:   Agent{Name: "Discord", Type: "discord"},
			wantErr: "discord webhook is required",
		},
		{
			name: "invalid webhook",
			agent: Agent{Name: "Discord", Type: "discord", Config: Config{
				DiscordWebhook: "http://example.com/webhook",
			}},
			wantErr: "discord webhook must start with https://discord.com/api/webhooks/",
		},
		{
			name: "invalid updates webhook",
			agent: Agent{Name: "Discord", Type: "discord", Config: Config{
				DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
				DiscordWebhookUpdates: "http://example.com/webhook",
			}},
			wantErr: "discord updates webhook must start with https://discord.com/api/webhooks/",
		},
		{
			name: "valid",
			agent: Agent{Name: "Discord", Type: "discord", Config: Config{
				DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
				DiscordWebhookUpdates: "https://discord.com/api/webhooks/222/bbb",
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

func TestDiscordMaskAndPreserve(t *testing.T) {
	cfg := Config{
		DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
		DiscordWebhookUpdates: "https://discord.com/api/webhooks/222/bbb",
	}

	masked := MaskConfigByType("discord", cfg)
	if masked.DiscordWebhook != maskedDiscordWebhook {
		t.Fatalf("discord webhook not masked")
	}
	if masked.DiscordWebhookUpdates != maskedDiscordWebhook {
		t.Fatalf("discord updates webhook not masked")
	}

	restored := PreserveConfigByType("discord", masked, cfg)
	if restored.DiscordWebhook != cfg.DiscordWebhook {
		t.Fatalf("discord webhook not preserved")
	}
	if restored.DiscordWebhookUpdates != cfg.DiscordWebhookUpdates {
		t.Fatalf("discord updates webhook not preserved")
	}
}

func TestDiscordResolveWebhook(t *testing.T) {
	p := discordProvider{}
	agent := Agent{Config: Config{
		DiscordWebhook:        "https://discord.com/api/webhooks/main/token",
		DiscordWebhookUpdates: "https://discord.com/api/webhooks/updates/token",
	}}

	if got := p.resolveWebhook(agent, RouteDefault); got != agent.Config.DiscordWebhook {
		t.Fatalf("default route webhook = %q", got)
	}
	if got := p.resolveWebhook(agent, RouteUpdates); got != agent.Config.DiscordWebhookUpdates {
		t.Fatalf("updates route webhook = %q", got)
	}

	agent.Config.DiscordWebhookUpdates = ""
	if got := p.resolveWebhook(agent, RouteUpdates); got != agent.Config.DiscordWebhook {
		t.Fatalf("updates fallback webhook = %q", got)
	}
}
