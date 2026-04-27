package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type pushoverNotificationProvider struct{}

func init() {
	registerNotificationAgentProvider(pushoverNotificationProvider{})
}

func (pushoverNotificationProvider) Type() string {
	return "pushover"
}

func (pushoverNotificationProvider) Async() bool {
	return true
}

func (pushoverNotificationProvider) MaskConfig(nc NotificationConfig) NotificationConfig {
	nc.PushoverUserKey = maskSecret(nc.PushoverUserKey, maskedToken)
	nc.PushoverAppToken = maskSecret(nc.PushoverAppToken, maskedToken)
	return nc
}

func (pushoverNotificationProvider) PreserveConfig(incoming, existing NotificationConfig) NotificationConfig {
	incoming.PushoverUserKey = preserveIfMasked(strings.TrimSpace(incoming.PushoverUserKey), existing.PushoverUserKey, maskedToken)
	incoming.PushoverAppToken = preserveIfMasked(strings.TrimSpace(incoming.PushoverAppToken), existing.PushoverAppToken, maskedToken)
	return incoming
}

func (pushoverNotificationProvider) Validate(agent NotificationAgent) error {
	if strings.TrimSpace(agent.Config.PushoverUserKey) == "" || strings.TrimSpace(agent.Config.PushoverAppToken) == "" {
		return fmt.Errorf("pushover user key and app token are required")
	}
	return nil
}

func (pushoverNotificationProvider) Test(app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	nc := agent.Config
	if strings.TrimSpace(nc.PushoverUserKey) == "" || strings.TrimSpace(nc.PushoverAppToken) == "" {
		return nil, fmt.Errorf("User key and app token are required")
	}

	res := NotificationTestResult{Label: "Pushover", Status: notificationStatusOK}
	body, _ := json.Marshal(map[string]any{
		"token":    nc.PushoverAppToken,
		"user":     nc.PushoverUserKey,
		"title":    "Clonarr Test",
		"message":  "If you see this, Pushover is configured correctly!",
		"priority": 0,
	})

	resp, err := app.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		res.Status = notificationStatusError
		res.Error = fmt.Sprintf("Failed to reach Pushover: %v", err)
		return []NotificationTestResult{res}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		res.Status = notificationStatusError
		res.Error = fmt.Sprintf("Pushover returned %d", resp.StatusCode)
	}

	return []NotificationTestResult{res}, nil
}

func (pushoverNotificationProvider) Notify(app *App, agent NotificationAgent, payload NotificationPayload) error {
	nc := agent.Config
	if strings.TrimSpace(nc.PushoverUserKey) == "" || strings.TrimSpace(nc.PushoverAppToken) == "" {
		return nil
	}

	body, _ := json.Marshal(map[string]any{
		"token":    nc.PushoverAppToken,
		"user":     nc.PushoverUserKey,
		"title":    payload.Title,
		"message":  payload.Message,
		"priority": 0,
	})

	resp, err := app.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pushover returned %d", resp.StatusCode)
	}

	return nil
}
