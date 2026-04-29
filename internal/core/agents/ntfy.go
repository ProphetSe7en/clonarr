package agents

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
)

// ntfyProvider implements Provider for ntfy.sh push notifications.
//
// ntfy uses a simple POST-to-URL model: the topic is a URL path segment,
// metadata (title, priority, tags) is sent as HTTP headers, and the message
// body is plain text. Optional bearer-token auth covers private topics on
// self-hosted servers and ntfy.sh's authenticated tier.
//
// Severity → priority mapping mirrors Gotify: each level
// (info / warning / critical) can be independently enabled or disabled, and
// each can be assigned a custom priority value in ntfy's 1-5 range. When a
// severity is disabled, the message is silently dropped rather than sent at
// the lowest priority.
//
// Security: the bearer token, when present, is sent via the Authorization
// header — never in the URL — to keep it out of access logs and proxy
// records. Both ntfy.sh and self-hosted instances go through
// Runtime.NotifyClient (trusted endpoint, not SSRF-restricted) since the
// user explicitly configures the URL.
type ntfyProvider struct{}

// Compile-time check: ntfyProvider satisfies the Provider interface.
var _ Provider = ntfyProvider{}

func init() {
	registerProvider(ntfyProvider{})
}

// Type returns the provider registration key used in Agent.Type.
func (ntfyProvider) Type() string {
	return "ntfy"
}

// Async returns true because ntfy sends are dispatched in background workers.
func (ntfyProvider) Async() bool {
	return true
}

// MaskConfig hides the bearer token for API responses sent to the UI.
func (ntfyProvider) MaskConfig(cfg Config) Config {
	cfg.NtfyToken = maskSecret(cfg.NtfyToken, maskedToken)
	return cfg
}

// PreserveConfig keeps the existing token when masked placeholders are
// posted back from the UI without explicit user changes.
func (ntfyProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.NtfyToken = preserveIfMasked(strings.TrimSpace(incoming.NtfyToken), existing.NtfyToken, maskedToken)
	return incoming
}

// Validate checks the required URL and topic. Token is optional — public
// topics on ntfy.sh and self-hosted servers without auth need no token.
func (ntfyProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.NtfyURL) == "" {
		return fmt.Errorf("ntfy URL is required")
	}
	if strings.TrimSpace(agent.Config.NtfyTopic) == "" {
		return fmt.Errorf("ntfy topic is required")
	}
	return nil
}

// Test sends one verification message to the configured topic.
func (n ntfyProvider) Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.NtfyURL) == "" || strings.TrimSpace(cfg.NtfyTopic) == "" {
		return nil, fmt.Errorf("ntfy URL and topic are required")
	}
	if runtime.NotifyClient == nil {
		return nil, fmt.Errorf("ntfy client not configured")
	}

	res := TestResult{Label: "ntfy", Status: statusOK}
	resp, err := ntfyPost(ctx, runtime.NotifyClient, cfg, testTitle, testMessage("ntfy"), 3, "white_check_mark")
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach ntfy: %v", err)
		return []TestResult{res}, nil
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = httpError("ntfy", resp).Error()
	}

	return []TestResult{res}, nil
}

// Notify sends one outbound message at a severity-mapped priority.
// Returns nil silently when the resolved severity is disabled.
func (n ntfyProvider) Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error {
	cfg := agent.Config
	if strings.TrimSpace(cfg.NtfyURL) == "" || strings.TrimSpace(cfg.NtfyTopic) == "" {
		return nil
	}
	if runtime.NotifyClient == nil {
		return fmt.Errorf("ntfy client not configured")
	}

	severity := payload.severityOrDefault()
	priority, ok := n.priorityForSeverity(cfg, severity)
	if !ok {
		return nil
	}

	resp, err := ntfyPost(ctx, runtime.NotifyClient, cfg, payload.Title, payload.Message, priority, ntfyTagsForSeverity(severity))
	if err != nil {
		return err
	}
	defer drainAndClose(resp)

	if resp.StatusCode >= 400 {
		return httpError("ntfy", resp)
	}

	return nil
}

// ntfyPost sends a single message to the configured ntfy topic. Title,
// priority, and tags are HTTP headers. Bearer auth (when configured)
// goes through Authorization — never the URL — to avoid leaking the
// token through proxy and server access logs.
func ntfyPost(ctx context.Context, client HTTPPoster, cfg Config, title, message string, priority int, tags string) (*http.Response, error) {
	base := strings.TrimRight(cfg.NtfyURL, "/")
	topic := strings.TrimPrefix(strings.TrimSpace(cfg.NtfyTopic), "/")
	url := base + "/" + topic

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBufferString(message))
	if err != nil {
		return nil, fmt.Errorf("ntfy request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if title != "" {
		req.Header.Set("Title", title)
	}
	if priority >= 1 && priority <= 5 {
		req.Header.Set("Priority", fmt.Sprintf("%d", priority))
	}
	if tags != "" {
		req.Header.Set("Tags", tags)
	}
	if token := strings.TrimSpace(cfg.NtfyToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return client.Do(req)
}

// priorityForSeverity maps payload severity to ntfy priority (1-5).
// Returns (priority, true) when the severity channel is enabled, or
// (0, false) when disabled. Defaults: critical=5, warning=4, info=3.
// User-supplied values outside 1-5 are clamped to the default.
func (ntfyProvider) priorityForSeverity(cfg Config, severity Severity) (int, bool) {
	clamp := func(p, dflt int) int {
		if p < 1 || p > 5 {
			return dflt
		}
		return p
	}
	switch severity {
	case SeverityCritical:
		if !cfg.NtfyPriorityCritical {
			return 0, false
		}
		if cfg.NtfyCriticalValue != nil {
			return clamp(*cfg.NtfyCriticalValue, 5), true
		}
		return 5, true
	case SeverityWarning:
		if !cfg.NtfyPriorityWarning {
			return 0, false
		}
		if cfg.NtfyWarningValue != nil {
			return clamp(*cfg.NtfyWarningValue, 4), true
		}
		return 4, true
	default:
		if !cfg.NtfyPriorityInfo {
			return 0, false
		}
		if cfg.NtfyInfoValue != nil {
			return clamp(*cfg.NtfyInfoValue, 3), true
		}
		return 3, true
	}
}

// ntfyTagsForSeverity returns a comma-separated tag list that ntfy renders
// as emoji prefixed to the title. The names match ntfy's documented emoji
// shortcodes (https://docs.ntfy.sh/publish/#tags-emojis).
func ntfyTagsForSeverity(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "rotating_light"
	case SeverityWarning:
		return "warning"
	default:
		return "white_check_mark"
	}
}
