package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// discordProvider implements Provider for Discord webhook notifications.
// It supports two independent webhook channels (main and updates) selected
// via payload Route. Messages are delivered as Discord embeds with a colored
// sidebar and a footer containing the Clonarr version.
//
// Security: Discord webhooks are user-supplied URLs, so all HTTP calls go
// through Runtime.SafeClient (SSRF-protected). The URL is also validated
// against known Discord API prefixes before every send.
type discordProvider struct{}

// Compile-time check: discordProvider satisfies the Provider interface.
var _ Provider = discordProvider{}

func init() {
	registerProvider(discordProvider{})
}

// Type returns the provider registration key used in Agent.Type.
func (discordProvider) Type() string {
	return "discord"
}

// Async returns true because Discord webhook sends are dispatched in background
// workers. This prevents the sync goroutine from blocking when Discord rate-limits
// (429 + Retry-After) or experiences high latency.
func (discordProvider) Async() bool {
	return true
}

// MaskConfig hides Discord webhook credentials for API responses.
func (discordProvider) MaskConfig(cfg Config) Config {
	cfg.DiscordWebhook = maskSecret(cfg.DiscordWebhook, maskedDiscordWebhook)
	cfg.DiscordWebhookUpdates = maskSecret(cfg.DiscordWebhookUpdates, maskedDiscordWebhook)
	return cfg
}

// PreserveConfig keeps existing credentials when masked placeholders are posted back.
func (discordProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.DiscordWebhook = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhook), existing.DiscordWebhook, maskedDiscordWebhook)
	incoming.DiscordWebhookUpdates = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhookUpdates), existing.DiscordWebhookUpdates, maskedDiscordWebhook)
	return incoming
}

// Validate checks that the main webhook URL is present and well-formed,
// and optionally validates the updates webhook URL if provided.
func (discordProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.DiscordWebhook) == "" {
		return fmt.Errorf("discord webhook is required")
	}
	webhook := strings.TrimSpace(agent.Config.DiscordWebhook)
	if !isDiscordWebhookURL(webhook) {
		return fmt.Errorf("discord webhook must start with https://discord.com/api/webhooks/ or https://discordapp.com/api/webhooks/")
	}
	if u := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); u != "" {
		if !isDiscordWebhookURL(u) {
			return fmt.Errorf("discord updates webhook must start with https://discord.com/api/webhooks/ or https://discordapp.com/api/webhooks/")
		}
	}
	return nil
}

// Test sends one test embed per configured webhook channel (main and, if
// different, updates). Returns one TestResult per channel so the UI can
// show per-channel pass/fail feedback.
func (d discordProvider) Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	mainWebhook := strings.TrimSpace(cfg.DiscordWebhook)
	updatesWebhook := strings.TrimSpace(cfg.DiscordWebhookUpdates)
	if mainWebhook == "" {
		return nil, fmt.Errorf("discord webhook is required")
	}

	results := make([]TestResult, 0, 2)

	res := TestResult{Label: "Sync webhook", Status: statusOK}
	if err := d.sendWebhook(ctx, runtime, mainWebhook, testTitle, testMessage("Discord"), testColor); err != nil {
		res.Status = statusError
		res.Error = err.Error()
	}
	results = append(results, res)

	if updatesWebhook != "" && updatesWebhook != mainWebhook {
		res := TestResult{Label: "Updates webhook", Status: statusOK}
		if err := d.sendWebhook(ctx, runtime, updatesWebhook, testTitle, testMessage("Discord"), testColor); err != nil {
			res.Status = statusError
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	return results, nil
}

// Notify sends one outbound notification to the route-resolved webhook.
func (d discordProvider) Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error {
	webhook := d.resolveWebhook(agent, payload.Route)
	if webhook == "" {
		return nil
	}
	return d.sendWebhook(ctx, runtime, webhook, payload.Title, payload.Message, payload.Color)
}

// resolveWebhook chooses the updates webhook for RouteUpdates, falling back to main.
func (discordProvider) resolveWebhook(agent Agent, route Route) string {
	if route == RouteUpdates {
		if webhook := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); webhook != "" {
			return webhook
		}
	}
	return strings.TrimSpace(agent.Config.DiscordWebhook)
}

// sendWebhook posts one Discord embed to the given webhook URL.
// The embed includes a title, description, colored sidebar, and a version
// footer. Returns an error if the HTTP client is missing, the URL fails
// validation, or Discord responds with a 4xx/5xx status.
func (discordProvider) sendWebhook(ctx context.Context, runtime Runtime, webhook, title, description string, color int) error {
	if runtime.SafeClient == nil {
		return fmt.Errorf("discord client not configured")
	}

	webhook = strings.TrimSpace(webhook)
	if !isDiscordWebhookURL(webhook) {
		return fmt.Errorf("discord webhook must start with https://discord.com/api/webhooks/ or https://discordapp.com/api/webhooks/")
	}

	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"footer":      map[string]string{"text": "Clonarr " + runtime.Version + " by ProphetSe7en"},
	}
	body, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := runtime.SafeClient.Do(req)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return httpError("discord", resp)
	}
	return nil
}

// isDiscordWebhookURL returns true when raw starts with an accepted Discord
// webhook API prefix. Both discord.com and the legacy discordapp.com domains
// are accepted.
func isDiscordWebhookURL(raw string) bool {
	return strings.HasPrefix(raw, "https://discord.com/api/webhooks/") ||
		strings.HasPrefix(raw, "https://discordapp.com/api/webhooks/")
}
