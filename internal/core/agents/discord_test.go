package agents

import (
	"context"
	"strings"
	"testing"
)

// TestDiscordValidate verifies the Discord provider's Validate logic:
// missing webhook, invalid URL prefix, invalid updates webhook, and valid config.
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
			wantErr: "discord webhook must start with https://discord.com/api/webhooks/ or https://discordapp.com/api/webhooks/",
		},
		{
			name: "invalid updates webhook",
			agent: Agent{Name: "Discord", Type: "discord", Config: Config{
				DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
				DiscordWebhookUpdates: "http://example.com/webhook",
			}},
			wantErr: "discord updates webhook must start with https://discord.com/api/webhooks/ or https://discordapp.com/api/webhooks/",
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

// TestDiscordMaskAndPreserve verifies the credential mask/preserve round-trip:
// MaskConfigByType replaces webhook URLs with placeholders, and PreserveConfigByType
// restores the originals when those placeholders are submitted back.
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

// TestDiscordResolveWebhook verifies route-based webhook selection:
// RouteDefault → main webhook, RouteUpdates → updates webhook (with fallback
// to main when updates webhook is empty).
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

// TestDiscordTestHappyPath verifies that Test sends to both webhooks and
// returns OK results when the HTTP client returns 200.
func TestDiscordTestHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Discord", Type: "discord", Config: Config{
		DiscordWebhook:        "https://discord.com/api/webhooks/111/aaa",
		DiscordWebhookUpdates: "https://discord.com/api/webhooks/222/bbb",
	}}

	results, err := discordProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (main + updates), got %d", len(results))
	}
	for _, r := range results {
		if r.Status != statusOK {
			t.Errorf("result %q: status=%q, error=%q", r.Label, r.Status, r.Error)
		}
	}
}

// TestDiscordTestHTTPError verifies that Test captures per-channel errors
// when the HTTP client returns an error.
func TestDiscordTestHTTPError(t *testing.T) {
	mock := newErrorPoster("connection refused")
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Discord", Type: "discord", Config: Config{
		DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
	}}

	results, err := discordProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() should not return top-level error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != statusError {
		t.Fatalf("expected error status, got %q", results[0].Status)
	}
	if !strings.Contains(results[0].Error, "connection refused") {
		t.Fatalf("error should contain 'connection refused', got %q", results[0].Error)
	}
}

// TestDiscordTestNoWebhook verifies that Test returns an error when no
// webhook URL is configured.
func TestDiscordTestNoWebhook(t *testing.T) {
	rt := testRuntime(nil, newOKPoster())
	agent := Agent{Name: "Discord", Type: "discord", Config: Config{}}

	_, err := discordProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when no webhook configured")
	}
}

// TestDiscordNotifyHappyPath verifies that Notify sends to the correct
// webhook and returns nil on success.
func TestDiscordNotifyHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Discord", Type: "discord", Enabled: true, Config: Config{
		DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
	}}
	payload := Payload{
		Title:    "Test Title",
		Message:  "Test body",
		Color:    0x00ff00,
		Severity: SeverityInfo,
		Route:    RouteDefault,
	}

	err := discordProvider{}.Notify(context.Background(), rt, agent, payload)
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if !strings.Contains(mock.lastURL, "discord.com/api/webhooks/") {
		t.Fatalf("expected Discord webhook URL, got %q", mock.lastURL)
	}
}

// TestDiscordNotifyHTTPError verifies that Notify returns errors from the
// HTTP client.
func TestDiscordNotifyHTTPError(t *testing.T) {
	mock := newStatusPoster(429)
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Discord", Type: "discord", Enabled: true, Config: Config{
		DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
	}}

	err := discordProvider{}.Notify(context.Background(), rt, agent, Payload{Route: RouteDefault})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("error should mention status code: %v", err)
	}
}

// TestDiscordNotifyNilClient verifies that Notify returns an error when
// the SafeClient is not configured.
func TestDiscordNotifyNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Discord", Type: "discord", Enabled: true, Config: Config{
		DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
	}}

	err := discordProvider{}.Notify(context.Background(), rt, agent, Payload{Route: RouteDefault})
	if err == nil {
		t.Fatal("expected error when SafeClient is nil")
	}
}

// TestDiscordNotifyEmptyWebhook verifies that Notify silently skips when
// the resolved webhook is empty.
func TestDiscordNotifyEmptyWebhook(t *testing.T) {
	rt := testRuntime(nil, newOKPoster())
	agent := Agent{Name: "Discord", Type: "discord", Enabled: true, Config: Config{}}

	err := discordProvider{}.Notify(context.Background(), rt, agent, Payload{Route: RouteDefault})
	if err != nil {
		t.Fatalf("expected nil for empty webhook, got: %v", err)
	}
}

// TestDiscordNotifyRouteUpdates verifies that Notify uses the updates
// webhook when Route is RouteUpdates.
func TestDiscordNotifyRouteUpdates(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Discord", Type: "discord", Enabled: true, Config: Config{
		DiscordWebhook:        "https://discord.com/api/webhooks/main/token",
		DiscordWebhookUpdates: "https://discord.com/api/webhooks/updates/token",
	}}

	err := discordProvider{}.Notify(context.Background(), rt, agent, Payload{Route: RouteUpdates})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if !strings.Contains(mock.lastURL, "/updates/") {
		t.Fatalf("expected updates webhook URL, got %q", mock.lastURL)
	}
}
