package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// appriseProvider implements Provider for Apprise API meta-notifier servers
// (https://github.com/caronc/apprise-api). Apprise fans one notification out
// to many backends — Discord, Slack, MS Teams, email, etc. — through its
// 100+ supported URL schemes.
//
// This provider runs in stateless mode: the user configures one or more
// notification URLs in Clonarr, and each Test/Notify call ships those URLs
// along with the message body to the Apprise server. The Apprise server
// itself holds no per-agent state.
//
// Severity → Apprise message type mapping:
//
//	SeverityCritical → "failure"
//	SeverityWarning  → "warning"
//	SeverityInfo     → "info"
//
// Security: the optional bearer token for protected Apprise instances
// goes through the Authorization header. Apprise URLs themselves
// (discord://..., mailto://...) often carry their own credentials and
// are masked / preserved end-to-end. Calls go through Runtime.NotifyClient
// since the user configures the Apprise server URL explicitly.
type appriseProvider struct{}

// Compile-time check: appriseProvider satisfies the Provider interface.
var _ Provider = appriseProvider{}

func init() {
	registerProvider(appriseProvider{})
}

// Type returns the provider registration key used in Agent.Type.
func (appriseProvider) Type() string {
	return "apprise"
}

// Async returns true because Apprise sends are dispatched in background workers.
func (appriseProvider) Async() bool {
	return true
}

// MaskConfig hides sensitive fields for API responses to the UI:
//   - The bearer token, if set
//   - Each Apprise notification URL — these typically embed credentials
//     (e.g. `discord://webhook_id/token`, `mailto://user:pass@host`) so the
//     entire URL is treated as secret
func (appriseProvider) MaskConfig(cfg Config) Config {
	cfg.AppriseToken = maskSecret(cfg.AppriseToken, maskedToken)
	if len(cfg.AppriseURLs) > 0 {
		masked := make([]string, len(cfg.AppriseURLs))
		for i, u := range cfg.AppriseURLs {
			masked[i] = maskSecret(u, maskedToken)
		}
		cfg.AppriseURLs = masked
	}
	return cfg
}

// PreserveConfig restores the original token + URL list when the UI sends
// back masked placeholders. URLs are matched by index — if every incoming
// URL is the masked placeholder and the lengths match, the existing list
// is preserved as-is.
func (appriseProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.AppriseToken = preserveIfMasked(strings.TrimSpace(incoming.AppriseToken), existing.AppriseToken, maskedToken)
	if len(incoming.AppriseURLs) == len(existing.AppriseURLs) {
		allMasked := true
		for _, u := range incoming.AppriseURLs {
			if strings.TrimSpace(u) != maskedToken {
				allMasked = false
				break
			}
		}
		if allMasked {
			restored := make([]string, len(existing.AppriseURLs))
			copy(restored, existing.AppriseURLs)
			incoming.AppriseURLs = restored
		}
	}
	return incoming
}

// Validate checks the API URL and at least one notification URL.
func (appriseProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.AppriseURL) == "" {
		return fmt.Errorf("apprise URL is required")
	}
	if len(filterEmpty(agent.Config.AppriseURLs)) == 0 {
		return fmt.Errorf("at least one Apprise notification URL is required")
	}
	return nil
}

// Test sends one verification message via the Apprise API.
func (a appriseProvider) Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.AppriseURL) == "" {
		return nil, fmt.Errorf("apprise URL is required")
	}
	urls := filterEmpty(cfg.AppriseURLs)
	if len(urls) == 0 {
		return nil, fmt.Errorf("at least one Apprise notification URL is required")
	}
	if runtime.NotifyClient == nil {
		return nil, fmt.Errorf("apprise client not configured")
	}

	res := TestResult{Label: "Apprise", Status: statusOK}
	resp, err := apprisePost(ctx, runtime.NotifyClient, cfg, urls, testTitle, testMessage("Apprise"), "info")
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach Apprise: %v", err)
		return []TestResult{res}, nil
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = httpError("apprise", resp).Error()
	}

	return []TestResult{res}, nil
}

// Notify sends one outbound notification. Severity is mapped to Apprise's
// type field (info / warning / failure). Messages are sent as markdown so
// the same Payload.Message body works across Discord/Gotify/Slack/etc.
// downstream backends that respect markdown.
func (a appriseProvider) Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error {
	cfg := agent.Config
	if strings.TrimSpace(cfg.AppriseURL) == "" {
		return nil
	}
	urls := filterEmpty(cfg.AppriseURLs)
	if len(urls) == 0 {
		return nil
	}
	if runtime.NotifyClient == nil {
		return fmt.Errorf("apprise client not configured")
	}

	resp, err := apprisePost(ctx, runtime.NotifyClient, cfg, urls, payload.Title, payload.Message, appriseTypeForSeverity(payload.severityOrDefault()))
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return httpError("apprise", resp)
	}

	return nil
}

// apprisePost sends a single notification request to the Apprise API.
// URLs are joined with commas per Apprise's documented URL-list syntax.
// Bearer token (when configured) goes in the Authorization header so the
// secret never appears in URL query strings or proxy logs.
func apprisePost(ctx context.Context, client HTTPPoster, cfg Config, urls []string, title, message, msgType string) (*http.Response, error) {
	endpoint := strings.TrimRight(cfg.AppriseURL, "/") + "/notify"

	body, err := json.Marshal(map[string]any{
		"urls":   strings.Join(urls, ","),
		"title":  title,
		"body":   message,
		"type":   msgType,
		"format": "markdown",
	})
	if err != nil {
		return nil, fmt.Errorf("apprise marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("apprise request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(cfg.AppriseToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// appriseTypeForSeverity maps Clonarr severity to Apprise's notification
// type vocabulary. Apprise also defines "success" but Clonarr doesn't
// distinguish info/success in its severity model — both map to "info".
func appriseTypeForSeverity(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "failure"
	case SeverityWarning:
		return "warning"
	default:
		return "info"
	}
}

// filterEmpty drops zero-length entries (and trims whitespace) from a
// string slice. Used to clean up the AppriseURLs list — UI textarea splits
// on newlines and may produce trailing empty entries.
func filterEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
