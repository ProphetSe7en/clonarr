package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type pushoverProvider struct{}

var _ Provider = pushoverProvider{}

func init() {
	registerProvider(pushoverProvider{})
}

func (pushoverProvider) Type() string {
	return "pushover"
}

func (pushoverProvider) Async() bool {
	return true
}

func (pushoverProvider) MaskConfig(cfg Config) Config {
	cfg.PushoverUserKey = maskSecret(cfg.PushoverUserKey, maskedToken)
	cfg.PushoverAppToken = maskSecret(cfg.PushoverAppToken, maskedToken)
	return cfg
}

func (pushoverProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.PushoverUserKey = preserveIfMasked(strings.TrimSpace(incoming.PushoverUserKey), existing.PushoverUserKey, maskedToken)
	incoming.PushoverAppToken = preserveIfMasked(strings.TrimSpace(incoming.PushoverAppToken), existing.PushoverAppToken, maskedToken)
	return incoming
}

func (pushoverProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.PushoverUserKey) == "" || strings.TrimSpace(agent.Config.PushoverAppToken) == "" {
		return fmt.Errorf("pushover user key and app token are required")
	}
	return nil
}

func (pushoverProvider) Test(runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.PushoverUserKey) == "" || strings.TrimSpace(cfg.PushoverAppToken) == "" {
		return nil, fmt.Errorf("User key and app token are required")
	}
	if runtime.SafeClient == nil {
		return nil, fmt.Errorf("pushover client not configured")
	}

	res := TestResult{Label: "Pushover", Status: statusOK}
	body, _ := json.Marshal(map[string]any{
		"token":    cfg.PushoverAppToken,
		"user":     cfg.PushoverUserKey,
		"title":    "Clonarr Test",
		"message":  "If you see this, Pushover is configured correctly!",
		"priority": 0,
	})

	resp, err := runtime.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach Pushover: %v", err)
		return []TestResult{res}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = fmt.Sprintf("Pushover returned %d", resp.StatusCode)
	}

	return []TestResult{res}, nil
}

func (pushoverProvider) Notify(runtime Runtime, agent Agent, payload Payload) error {
	cfg := agent.Config
	if strings.TrimSpace(cfg.PushoverUserKey) == "" || strings.TrimSpace(cfg.PushoverAppToken) == "" {
		return nil
	}
	if runtime.SafeClient == nil {
		return fmt.Errorf("pushover client not configured")
	}

	body, _ := json.Marshal(map[string]any{
		"token":    cfg.PushoverAppToken,
		"user":     cfg.PushoverUserKey,
		"title":    payload.Title,
		"message":  payload.Message,
		"priority": 0,
	})

	resp, err := runtime.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pushover returned %d", resp.StatusCode)
	}

	return nil
}
