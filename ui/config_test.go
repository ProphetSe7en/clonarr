package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateFlatNotifications verifies that a v2.0.x config file containing
// the legacy flat notification fields (discordWebhook, gotifyUrl, etc.) is
// promoted to the new NotificationAgents slice on Load(), with correct names,
// types, credentials, and event subscriptions preserved.
func TestMigrateFlatNotifications(t *testing.T) {
	// Build a minimal config JSON that mimics a pre-agent v2.0.x file.
	oldCfg := map[string]any{
		"autoSync": map[string]any{
			"enabled":            true,
			"notifyOnSuccess":    true,
			"notifyOnFailure":    true,
			"notifyOnRepoUpdate": false,
			// Discord
			"discordWebhook":        "https://discord.com/api/webhooks/111/aaa",
			"discordWebhookUpdates": "https://discord.com/api/webhooks/222/bbb",
			"discordEnabled":        true,
			// Gotify
			"gotifyUrl":              "https://gotify.example.com",
			"gotifyToken":            "tok123",
			"gotifyEnabled":          false,
			"gotifyPriorityCritical": true,
			"gotifyPriorityWarning":  true,
			"gotifyPriorityInfo":     false,
			"gotifyCriticalValue":    8,
			"gotifyWarningValue":     5,
			"gotifyInfoValue":        3,
			// Pushover
			"pushoverUserKey":  "ukey456",
			"pushoverAppToken": "atoken789",
			"pushoverEnabled":  false,
		},
	}

	raw, err := json.Marshal(oldCfg)
	if err != nil {
		t.Fatalf("marshal old config: %v", err)
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "clonarr.json")
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}

	cs := newConfigStore(dir)
	if err := cs.Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}

	agents := cs.config.AutoSync.NotificationAgents
	if len(agents) != 3 {
		t.Fatalf("want 3 agents after migration, got %d", len(agents))
	}

	// Build a map by type for order-independent assertions.
	byType := make(map[string]NotificationAgent, 3)
	for _, a := range agents {
		byType[a.Type] = a
	}

	// --- Discord ---
	d, ok := byType["discord"]
	if !ok {
		t.Fatal("no discord agent after migration")
	}
	if d.Name != "Discord" {
		t.Errorf("discord name = %q, want %q", d.Name, "Discord")
	}
	if !d.Enabled {
		t.Error("discord agent should be enabled (discordEnabled=true)")
	}
	if d.Config.DiscordWebhook != "https://discord.com/api/webhooks/111/aaa" {
		t.Errorf("discord webhook = %q", d.Config.DiscordWebhook)
	}
	if d.Config.DiscordWebhookUpdates != "https://discord.com/api/webhooks/222/bbb" {
		t.Errorf("discord updates webhook = %q", d.Config.DiscordWebhookUpdates)
	}
	if !d.Events.OnSyncSuccess {
		t.Error("discord: OnSyncSuccess should be true")
	}
	if !d.Events.OnSyncFailure {
		t.Error("discord: OnSyncFailure should be true")
	}
	if d.Events.OnRepoUpdate {
		t.Error("discord: OnRepoUpdate should be false")
	}

	// --- Gotify ---
	g, ok := byType["gotify"]
	if !ok {
		t.Fatal("no gotify agent after migration")
	}
	if g.Name != "Gotify" {
		t.Errorf("gotify name = %q, want %q", g.Name, "Gotify")
	}
	if g.Enabled {
		t.Error("gotify agent should be disabled (gotifyEnabled=false)")
	}
	if g.Config.GotifyURL != "https://gotify.example.com" {
		t.Errorf("gotify url = %q", g.Config.GotifyURL)
	}
	if g.Config.GotifyToken != "tok123" {
		t.Errorf("gotify token = %q", g.Config.GotifyToken)
	}
	if g.Config.GotifyCriticalValue == nil || *g.Config.GotifyCriticalValue != 8 {
		t.Errorf("gotify critical value wrong")
	}

	// --- Pushover ---
	p, ok := byType["pushover"]
	if !ok {
		t.Fatal("no pushover agent after migration")
	}
	if p.Name != "Pushover" {
		t.Errorf("pushover name = %q, want %q", p.Name, "Pushover")
	}
	if p.Enabled {
		t.Error("pushover agent should be disabled (pushoverEnabled=false)")
	}
	if p.Config.PushoverUserKey != "ukey456" {
		t.Errorf("pushover user key = %q", p.Config.PushoverUserKey)
	}
	if p.Config.PushoverAppToken != "atoken789" {
		t.Errorf("pushover app token = %q", p.Config.PushoverAppToken)
	}

	// --- Idempotency: second Load() must not duplicate agents ---
	if err := cs.Load(); err != nil {
		t.Fatalf("second Load(): %v", err)
	}
	agents2 := cs.config.AutoSync.NotificationAgents
	if len(agents2) != 3 {
		t.Errorf("idempotency: want 3 agents on second load, got %d", len(agents2))
	}
}

// TestMigrateFlatNotificationsEmpty verifies that Load() on a config with no
// flat notification fields produces zero agents (no phantom entries).
func TestMigrateFlatNotificationsEmpty(t *testing.T) {
	emptyCfg := map[string]any{
		"autoSync": map[string]any{"enabled": false},
	}
	raw, _ := json.Marshal(emptyCfg)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "clonarr.json"), raw, 0600)

	cs := newConfigStore(dir)
	if err := cs.Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if n := len(cs.config.AutoSync.NotificationAgents); n != 0 {
		t.Errorf("want 0 agents for empty config, got %d", n)
	}
}
