package core

import (
	"strings"
	"testing"
)

func TestSupportedNotificationAgentTypesIncludesBuiltins(t *testing.T) {
	types := SupportedNotificationAgentTypes()
	if len(types) < 3 {
		t.Fatalf("expected at least 3 notification providers, got %d", len(types))
	}

	expected := []string{"discord", "gotify", "pushover"}
	for _, typ := range expected {
		if _, ok := GetNotificationAgentProvider(typ); !ok {
			t.Fatalf("missing registered notification provider %q", typ)
		}
	}
}

func TestValidateNotificationAgent(t *testing.T) {
	tests := []struct {
		name    string
		agent   NotificationAgent
		wantErr string
	}{
		{
			name: "missing name",
			agent: NotificationAgent{Type: "discord", Config: NotificationConfig{
				DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
			}},
			wantErr: "name is required",
		},
		{
			name:    "unknown type",
			agent:   NotificationAgent{Name: "Custom", Type: "smtp"},
			wantErr: "unknown agent type",
		},
		{
			name:    "discord requires webhook",
			agent:   NotificationAgent{Name: "Discord", Type: "discord"},
			wantErr: "discord webhook is required",
		},
		{
			name: "valid discord",
			agent: NotificationAgent{Name: "Discord", Type: "discord", Config: NotificationConfig{
				DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
			}},
		},
		{
			name:    "gotify requires url and token",
			agent:   NotificationAgent{Name: "Gotify", Type: "gotify"},
			wantErr: "gotify URL and token are required",
		},
		{
			name:    "pushover requires credentials",
			agent:   NotificationAgent{Name: "Pushover", Type: "pushover"},
			wantErr: "pushover user key and app token are required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNotificationAgent(tc.agent)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateNotificationAgent() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateNotificationAgent() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateNotificationAgent() error = %q, want contains %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestMaskAndPreserveNotificationAgentConfig(t *testing.T) {
	discord := NotificationConfig{
		DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
		DiscordWebhookUpdates: "https://discord.com/api/webhooks/222/bbb",
	}
	maskedDiscord := MaskNotificationAgentConfig("discord", discord)
	if maskedDiscord.DiscordWebhook != maskedDiscordWebhook {
		t.Fatalf("discord webhook not masked")
	}
	if maskedDiscord.DiscordWebhookUpdates != maskedDiscordWebhook {
		t.Fatalf("discord updates webhook not masked")
	}
	restoredDiscord := PreserveNotificationAgentConfig("discord", maskedDiscord, discord)
	if restoredDiscord.DiscordWebhook != discord.DiscordWebhook {
		t.Fatalf("discord webhook not preserved")
	}
	if restoredDiscord.DiscordWebhookUpdates != discord.DiscordWebhookUpdates {
		t.Fatalf("discord updates webhook not preserved")
	}

	gotify := NotificationConfig{GotifyURL: "https://gotify.example.com", GotifyToken: "tok123"}
	maskedGotify := MaskNotificationAgentConfig("gotify", gotify)
	if maskedGotify.GotifyToken != maskedToken {
		t.Fatalf("gotify token not masked")
	}
	restoredGotify := PreserveNotificationAgentConfig("gotify", maskedGotify, gotify)
	if restoredGotify.GotifyToken != gotify.GotifyToken {
		t.Fatalf("gotify token not preserved")
	}

	pushover := NotificationConfig{PushoverUserKey: "user", PushoverAppToken: "app"}
	maskedPushover := MaskNotificationAgentConfig("pushover", pushover)
	if maskedPushover.PushoverUserKey != maskedToken || maskedPushover.PushoverAppToken != maskedToken {
		t.Fatalf("pushover credentials not masked")
	}
	restoredPushover := PreserveNotificationAgentConfig("pushover", maskedPushover, pushover)
	if restoredPushover.PushoverUserKey != pushover.PushoverUserKey || restoredPushover.PushoverAppToken != pushover.PushoverAppToken {
		t.Fatalf("pushover credentials not preserved")
	}
}
