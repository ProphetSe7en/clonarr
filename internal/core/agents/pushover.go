package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// pushoverProvider implements Provider for Pushover push notifications.
// Messages are sent via the Pushover API using the user's app token and
// user/group key. Severity is mapped to Pushover priorities:
//   - SeverityCritical → priority 1 (high — bypasses quiet hours, shown in red)
//   - SeverityWarning  → priority 0 (normal)
//   - SeverityInfo     → priority 0 (normal)
//
// Security: Pushover's API endpoint is a fixed third-party URL, so HTTP calls
// go through Runtime.SafeClient (SSRF-protected) to prevent credential leakage
// through DNS rebinding or other redirect attacks.
type pushoverProvider struct{}

// pushoverAPIURL is the Pushover message submission endpoint.
const pushoverAPIURL = "https://api.pushover.net/1/messages.json"

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
func (pushoverProvider) Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.PushoverUserKey) == "" || strings.TrimSpace(cfg.PushoverAppToken) == "" {
		return nil, fmt.Errorf("pushover user key and app token are required")
	}
	if runtime.SafeClient == nil {
		return nil, fmt.Errorf("pushover client not configured")
	}

	res := TestResult{Label: "Pushover", Status: statusOK}
	resp, err := pushoverPost(ctx, runtime.SafeClient, cfg, testTitle, testMessage("Pushover"), 0)
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach Pushover: %v", err)
		return []TestResult{res}, nil
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = httpError("pushover", resp).Error()
	}

	return []TestResult{res}, nil
}

// Notify sends one outbound Pushover message with severity-mapped priority.
// Returns nil (skip) when required credentials are missing, which can occur
// if the agent was disabled after dispatch was queued.
func (pushoverProvider) Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error {
	cfg := agent.Config
	if strings.TrimSpace(cfg.PushoverUserKey) == "" || strings.TrimSpace(cfg.PushoverAppToken) == "" {
		return nil
	}
	if runtime.SafeClient == nil {
		return fmt.Errorf("pushover client not configured")
	}

	resp, err := pushoverPost(ctx, runtime.SafeClient, cfg, payload.Title, payload.Message, pushoverPriority(payload.Severity))
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return httpError("pushover", resp)
	}

	return nil
}

// pushoverPost builds and sends a Pushover API message. This helper
// deduplicates the identical payload construction shared by Test and Notify.
func pushoverPost(ctx context.Context, client HTTPPoster, cfg Config, title, message string, priority int) (*http.Response, error) {
	body, err := json.Marshal(map[string]any{
		"token":    cfg.PushoverAppToken,
		"user":     cfg.PushoverUserKey,
		"title":    title,
		"message":  message,
		"priority": priority,
	})
	if err != nil {
		return nil, fmt.Errorf("pushover marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", pushoverAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("pushover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

// pushoverPriority maps Clonarr severity levels to Pushover priority integers.
// Pushover supports priorities -2 (lowest) through 2 (emergency).
//   - SeverityCritical → 1 (high priority: bypasses quiet hours, highlighted in red)
//   - All others       → 0 (normal priority)
func pushoverPriority(severity Severity) int {
	if severity == SeverityCritical {
		return 1
	}
	return 0
}
