package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// pushoverProvider implements Provider for Pushover push notifications.
// Messages are sent via the Pushover API (https://api.pushover.net/1/messages.json)
// using the user's app token and user/group key. All messages are sent with
// priority 0 (normal) — Pushover's priority system is not currently mapped to
// Clonarr's severity levels.
//
// Security: Pushover's API endpoint is a fixed third-party URL, so HTTP calls
// go through Runtime.SafeClient (SSRF-protected) to prevent credential leakage
// through DNS rebinding or other redirect attacks.
type pushoverProvider struct{}

// Compile-time check: pushoverProvider satisfies the Provider interface.
var _ Provider = pushoverProvider{}

func init() {
	registerProvider(pushoverProvider{})
}

// Type returns the provider registration key used in Agent.Type.
func (pushoverProvider) Type() string {
	return "pushover"
}

// Async returns true because Pushover sends are dispatched in background workers.
func (pushoverProvider) Async() bool {
	return true
}

// MaskConfig hides Pushover credentials for API responses.
func (pushoverProvider) MaskConfig(cfg Config) Config {
	cfg.PushoverUserKey = maskSecret(cfg.PushoverUserKey, maskedToken)
	cfg.PushoverAppToken = maskSecret(cfg.PushoverAppToken, maskedToken)
	return cfg
}

// PreserveConfig keeps existing credentials when masked placeholders are posted back.
func (pushoverProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.PushoverUserKey = preserveIfMasked(strings.TrimSpace(incoming.PushoverUserKey), existing.PushoverUserKey, maskedToken)
	incoming.PushoverAppToken = preserveIfMasked(strings.TrimSpace(incoming.PushoverAppToken), existing.PushoverAppToken, maskedToken)
	return incoming
}

// Validate checks required Pushover user key and app token fields.
func (pushoverProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.PushoverUserKey) == "" || strings.TrimSpace(agent.Config.PushoverAppToken) == "" {
		return fmt.Errorf("pushover user key and app token are required")
	}
	return nil
}

// Test sends one verification message to Pushover.
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

// Notify sends one outbound Pushover message with normal priority.
// Returns nil (skip) when required credentials are missing, which can occur
// if the agent was disabled after dispatch was queued.
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
