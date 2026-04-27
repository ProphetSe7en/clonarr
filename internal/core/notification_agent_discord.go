package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type discordNotificationProvider struct{}

func init() {
	registerNotificationAgentProvider(discordNotificationProvider{})
}

func (discordNotificationProvider) Type() string {
	return "discord"
}

func (discordNotificationProvider) Async() bool {
	return false
}

func (discordNotificationProvider) MaskConfig(nc NotificationConfig) NotificationConfig {
	nc.DiscordWebhook = maskSecret(nc.DiscordWebhook, maskedDiscordWebhook)
	nc.DiscordWebhookUpdates = maskSecret(nc.DiscordWebhookUpdates, maskedDiscordWebhook)
	return nc
}

func (discordNotificationProvider) PreserveConfig(incoming, existing NotificationConfig) NotificationConfig {
	incoming.DiscordWebhook = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhook), existing.DiscordWebhook, maskedDiscordWebhook)
	incoming.DiscordWebhookUpdates = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhookUpdates), existing.DiscordWebhookUpdates, maskedDiscordWebhook)
	return incoming
}

func (discordNotificationProvider) Validate(agent NotificationAgent) error {
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

func (d discordNotificationProvider) Test(app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	nc := agent.Config
	mainWebhook := strings.TrimSpace(nc.DiscordWebhook)
	updatesWebhook := strings.TrimSpace(nc.DiscordWebhookUpdates)

	results := make([]NotificationTestResult, 0, 2)

	if mainWebhook != "" {
		res := NotificationTestResult{Label: "Sync webhook", Status: notificationStatusOK}
		if err := d.sendWebhook(app, mainWebhook, "Clonarr Test", "If you see this, Discord is configured correctly!", 0x58a6ff); err != nil {
			res.Status = notificationStatusError
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	if updatesWebhook != "" && updatesWebhook != mainWebhook {
		res := NotificationTestResult{Label: "Updates webhook", Status: notificationStatusOK}
		if err := d.sendWebhook(app, updatesWebhook, "Clonarr Test", "If you see this, Discord is configured correctly!", 0x58a6ff); err != nil {
			res.Status = notificationStatusError
			res.Error = err.Error()
		}
		results = append(results, res)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("At least one webhook URL is required")
	}

	return results, nil
}

func (d discordNotificationProvider) Notify(app *App, agent NotificationAgent, payload NotificationPayload) error {
	webhook := d.resolveWebhook(agent, payload.Route)
	if webhook == "" {
		return nil
	}
	return d.sendWebhook(app, webhook, payload.Title, payload.Message, payload.Color)
}

func (discordNotificationProvider) resolveWebhook(agent NotificationAgent, route NotificationRoute) string {
	if route == NotificationRouteUpdates {
		if webhook := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); webhook != "" {
			return webhook
		}
	}
	return strings.TrimSpace(agent.Config.DiscordWebhook)
}

func (discordNotificationProvider) sendWebhook(app *App, webhook, title, description string, color int) error {
	webhook = strings.TrimSpace(webhook)
	if !isDiscordWebhookURL(webhook) {
		return fmt.Errorf("must start with https://discord.com/api/webhooks/")
	}

	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"footer":      map[string]string{"text": "Clonarr " + app.Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}

	resp, err := app.SafeClient.Post(webhook, "application/json", bytes.NewReader(payload))
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
