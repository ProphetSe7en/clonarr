package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// gotifyProvider implements Provider for Gotify self-hosted push notifications.
// Messages are sent via Gotify's /message endpoint as JSON with markdown extras.
//
// Gotify supports severity-based priority mapping: each severity level
// (info, warning, critical) can be independently enabled/disabled and assigned
// a custom integer priority value. When a severity is disabled, the message
// is silently dropped rather than sent with priority 0.
//
// Security: Gotify is typically self-hosted on a trusted LAN, so HTTP calls
// go through Runtime.NotifyClient (standard, non-SSRF-restricted).
type gotifyProvider struct{}

// Compile-time check: gotifyProvider satisfies the Provider interface.
var _ Provider = gotifyProvider{}

func init() {
	registerProvider(gotifyProvider{})
}

// Type returns the provider registration key used in Agent.Type.
func (gotifyProvider) Type() string {
	return "gotify"
}

// Async returns true because Gotify sends are dispatched in background workers.
func (gotifyProvider) Async() bool {
	return true
}

// MaskConfig hides the Gotify bearer token for API responses.
func (gotifyProvider) MaskConfig(cfg Config) Config {
	cfg.GotifyToken = maskSecret(cfg.GotifyToken, maskedToken)
	return cfg
}

// PreserveConfig keeps existing token when masked placeholders are posted back.
func (gotifyProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.GotifyToken = preserveIfMasked(strings.TrimSpace(incoming.GotifyToken), existing.GotifyToken, maskedToken)
	return incoming
}

// Validate checks required Gotify URL and token fields.
func (gotifyProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.GotifyURL) == "" || strings.TrimSpace(agent.Config.GotifyToken) == "" {
		return fmt.Errorf("gotify URL and token are required")
	}
	return nil
}

// Test sends one verification message to the configured Gotify endpoint.
func (g gotifyProvider) Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.GotifyURL) == "" || strings.TrimSpace(cfg.GotifyToken) == "" {
		return nil, fmt.Errorf("gotify URL and token are required")
	}
	if runtime.NotifyClient == nil {
		return nil, fmt.Errorf("gotify client not configured")
	}

	res := TestResult{Label: "Gotify", Status: statusOK}
	payload := map[string]any{
		"title":    testTitle,
		"message":  testMessage("Gotify"),
		"priority": 5,
		"extras":   map[string]any{"client::display": map[string]string{"contentType": "text/markdown"}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to encode Gotify request: %v", err)
		return []TestResult{res}, nil
	}

	resp, err := gotifyPost(ctx, runtime.NotifyClient, cfg, body)
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach Gotify: %v", err)
		return []TestResult{res}, nil
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = httpError("gotify", resp).Error()
	}

	return []TestResult{res}, nil
}

// Notify sends one outbound Gotify message.
// The message priority is resolved from the payload severity using the agent's
// per-severity enable flags and custom priority values. If the resolved
// severity is disabled (e.g. info notifications turned off), Notify returns
// nil without sending. Message bodies are normalized for Gotify's markdown
// renderer via normalizeGotifyMarkdown.
func (g gotifyProvider) Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error {
	cfg := agent.Config
	if strings.TrimSpace(cfg.GotifyURL) == "" || strings.TrimSpace(cfg.GotifyToken) == "" {
		return nil
	}
	if runtime.NotifyClient == nil {
		return fmt.Errorf("gotify client not configured")
	}

	priority, ok := g.priorityForSeverity(cfg, payload.severityOrDefault())
	if !ok {
		return nil
	}

	msg := normalizeGotifyMarkdown(payload.Message)
	body, err := json.Marshal(map[string]any{
		"title":    payload.Title,
		"message":  msg,
		"priority": priority,
		"extras": map[string]any{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("gotify marshal request body: %w", err)
	}

	resp, err := gotifyPost(ctx, runtime.NotifyClient, cfg, body)
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return httpError("gotify", resp)
	}

	return nil
}

// gotifyPost sends a JSON body to the Gotify /message endpoint, authenticating
// via the X-Gotify-Key header instead of a query-string token. This keeps the
// application token out of server access logs and reverse-proxy logs.
func gotifyPost(ctx context.Context, client HTTPPoster, cfg Config, body []byte) (*http.Response, error) {
	gotifyURL := strings.TrimRight(cfg.GotifyURL, "/") + "/message"
	req, err := http.NewRequestWithContext(ctx, "POST", gotifyURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", cfg.GotifyToken)
	return client.Do(req)
}

// priorityForSeverity maps a payload severity to the user-configured Gotify
// priority integer. Returns (priority, true) when the severity channel is
// enabled, or (0, false) when the user has disabled that severity level.
// When enabled but no custom value is set (*int is nil), the default priority
// is 0 (Gotify's lowest).
func (gotifyProvider) priorityForSeverity(cfg Config, severity Severity) (int, bool) {
	switch severity {
	case SeverityCritical:
		if !cfg.GotifyPriorityCritical {
			return 0, false
		}
		if cfg.GotifyCriticalValue != nil {
			return *cfg.GotifyCriticalValue, true
		}
		return 0, true
	case SeverityWarning:
		if !cfg.GotifyPriorityWarning {
			return 0, false
		}
		if cfg.GotifyWarningValue != nil {
			return *cfg.GotifyWarningValue, true
		}
		return 0, true
	default:
		if !cfg.GotifyPriorityInfo {
			return 0, false
		}
		if cfg.GotifyInfoValue != nil {
			return *cfg.GotifyInfoValue, true
		}
		return 0, true
	}
}

// normalizeGotifyMarkdown adjusts line breaks for cleaner rendering in Gotify's
// markdown client display. Gotify's renderer collapses single newlines, so this
// function doubles newlines before bold headers and list items, then collapses
// any resulting triple-newlines back to doubles.
func normalizeGotifyMarkdown(message string) string {
	msg := message
	msg = strings.ReplaceAll(msg, "\n**", "\n\n**")
	msg = strings.ReplaceAll(msg, "\n- ", "\n\n- ")
	for strings.Contains(msg, "\n\n\n") {
		msg = strings.ReplaceAll(msg, "\n\n\n", "\n\n")
	}
	return msg
}
