package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type gotifyNotificationProvider struct{}

func init() {
	registerNotificationAgentProvider(gotifyNotificationProvider{})
}

func (gotifyNotificationProvider) Type() string {
	return "gotify"
}

func (gotifyNotificationProvider) Async() bool {
	return true
}

func (gotifyNotificationProvider) MaskConfig(nc NotificationConfig) NotificationConfig {
	nc.GotifyToken = maskSecret(nc.GotifyToken, maskedToken)
	return nc
}

func (gotifyNotificationProvider) PreserveConfig(incoming, existing NotificationConfig) NotificationConfig {
	incoming.GotifyToken = preserveIfMasked(strings.TrimSpace(incoming.GotifyToken), existing.GotifyToken, maskedToken)
	return incoming
}

func (gotifyNotificationProvider) Validate(agent NotificationAgent) error {
	if strings.TrimSpace(agent.Config.GotifyURL) == "" || strings.TrimSpace(agent.Config.GotifyToken) == "" {
		return fmt.Errorf("gotify URL and token are required")
	}
	return nil
}

func (g gotifyNotificationProvider) Test(app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	nc := agent.Config
	if strings.TrimSpace(nc.GotifyURL) == "" || strings.TrimSpace(nc.GotifyToken) == "" {
		return nil, fmt.Errorf("URL and token are required")
	}

	res := NotificationTestResult{Label: "Gotify", Status: notificationStatusOK}
	payload := map[string]any{
		"title":    "Clonarr Test",
		"message":  "If you see this, Gotify is configured correctly!",
		"priority": 5,
		"extras":   map[string]any{"client::display": map[string]string{"contentType": "text/markdown"}},
	}
	body, _ := json.Marshal(payload)
	gotifyURL := strings.TrimRight(nc.GotifyURL, "/") + "/message?token=" + url.QueryEscape(nc.GotifyToken)
	resp, err := app.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		res.Status = notificationStatusError
		res.Error = fmt.Sprintf("Failed to reach Gotify: %v", err)
		return []NotificationTestResult{res}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		res.Status = notificationStatusError
		res.Error = fmt.Sprintf("Gotify returned %d", resp.StatusCode)
	}

	return []NotificationTestResult{res}, nil
}

func (g gotifyNotificationProvider) Notify(app *App, agent NotificationAgent, payload NotificationPayload) error {
	nc := agent.Config
	if strings.TrimSpace(nc.GotifyURL) == "" || strings.TrimSpace(nc.GotifyToken) == "" {
		return nil
	}

	priority, ok := g.priorityForSeverity(nc, payload.severityOrDefault())
	if !ok {
		return nil
	}

	msg := normalizeGotifyMarkdown(payload.Message)
	body, _ := json.Marshal(map[string]any{
		"title":    payload.Title,
		"message":  msg,
		"priority": priority,
		"extras": map[string]any{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	})

	gotifyURL := strings.TrimRight(nc.GotifyURL, "/") + "/message?token=" + url.QueryEscape(nc.GotifyToken)
	resp, err := app.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify returned %d", resp.StatusCode)
	}

	return nil
}

func (gotifyNotificationProvider) priorityForSeverity(nc NotificationConfig, severity NotificationSeverity) (int, bool) {
	switch severity {
	case NotificationSeverityCritical:
		if !nc.GotifyPriorityCritical {
			return 0, false
		}
		if nc.GotifyCriticalValue != nil {
			return *nc.GotifyCriticalValue, true
		}
		return 0, true
	case NotificationSeverityWarning:
		if !nc.GotifyPriorityWarning {
			return 0, false
		}
		if nc.GotifyWarningValue != nil {
			return *nc.GotifyWarningValue, true
		}
		return 0, true
	default:
		if !nc.GotifyPriorityInfo {
			return 0, false
		}
		if nc.GotifyInfoValue != nil {
			return *nc.GotifyInfoValue, true
		}
		return 0, true
	}
}

func normalizeGotifyMarkdown(message string) string {
	msg := message
	msg = strings.ReplaceAll(msg, "\n**", "\n\n**")
	msg = strings.ReplaceAll(msg, "\n- ", "\n\n- ")
	for strings.Contains(msg, "\n\n\n") {
		msg = strings.ReplaceAll(msg, "\n\n\n", "\n\n")
	}
	return msg
}
