package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type discordProvider struct{}

var _ Provider = discordProvider{}

func init() {
	registerProvider(discordProvider{})
}

func (discordProvider) Type() string {
	return "discord"
}

func (discordProvider) Async() bool {
	return false
}

func (discordProvider) MaskConfig(cfg Config) Config {
	cfg.DiscordWebhook = maskSecret(cfg.DiscordWebhook, maskedDiscordWebhook)
	cfg.DiscordWebhookUpdates = maskSecret(cfg.DiscordWebhookUpdates, maskedDiscordWebhook)
	return cfg
}

func (discordProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.DiscordWebhook = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhook), existing.DiscordWebhook, maskedDiscordWebhook)
	incoming.DiscordWebhookUpdates = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhookUpdates), existing.DiscordWebhookUpdates, maskedDiscordWebhook)
	return incoming
}

func (discordProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.DiscordWebhook) == "" {
		return fmt.Errorf("discord webhook is required")
	}
	webhook := strings.TrimSpace(agent.Config.DiscordWebhook)
	if !isDiscordWebhookURL(webhook) {
		return fmt.Errorf("discord webhook must start with https://discord.com/api/webhooks/")
	}
	if u := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); u != "" {
		if !isDiscordWebhookURL(u) {
			return fmt.Errorf("discord updates webhook must start with https://discord.com/api/webhooks/")
		}
	}
	return nil
}

func (d discordProvider) Test(runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	mainWebhook := strings.TrimSpace(cfg.DiscordWebhook)
	updatesWebhook := strings.TrimSpace(cfg.DiscordWebhookUpdates)

	results := make([]TestResult, 0, 2)

	if mainWebhook != "" {
		res := TestResult{Label: "Sync webhook", Status: statusOK}
		if err := d.sendWebhook(runtime, mainWebhook, "Clonarr Test", "If you see this, Discord is configured correctly!", 0x58a6ff); err != nil {
			res.Status = statusError
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	if updatesWebhook != "" && updatesWebhook != mainWebhook {
		res := TestResult{Label: "Updates webhook", Status: statusOK}
		if err := d.sendWebhook(runtime, updatesWebhook, "Clonarr Test", "If you see this, Discord is configured correctly!", 0x58a6ff); err != nil {
			res.Status = statusError
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("At least one webhook URL is required")
	}

	return results, nil
}

func (d discordProvider) Notify(runtime Runtime, agent Agent, payload Payload) error {
	webhook := d.resolveWebhook(agent, payload.Route)
	if webhook == "" {
		return nil
	}
	return d.sendWebhook(runtime, webhook, payload.Title, payload.Message, payload.Color)
}

func (discordProvider) resolveWebhook(agent Agent, route Route) string {
	if route == RouteUpdates {
		if webhook := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); webhook != "" {
			return webhook
		}
	}
	return strings.TrimSpace(agent.Config.DiscordWebhook)
}

func (discordProvider) sendWebhook(runtime Runtime, webhook, title, description string, color int) error {
	if runtime.SafeClient == nil {
		return fmt.Errorf("discord client not configured")
	}

	webhook = strings.TrimSpace(webhook)
	if !isDiscordWebhookURL(webhook) {
		return fmt.Errorf("must start with https://discord.com/api/webhooks/")
	}

	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"footer":      map[string]string{"text": "Clonarr " + runtime.Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}

	resp, err := runtime.SafeClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned %d", resp.StatusCode)
	}
	return nil
}

func isDiscordWebhookURL(raw string) bool {
	return strings.HasPrefix(raw, "https://discord.com/api/webhooks/") ||
		strings.HasPrefix(raw, "https://discordapp.com/api/webhooks/")
}
