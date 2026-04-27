package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type gotifyProvider struct{}

var _ Provider = gotifyProvider{}

func init() {
	registerProvider(gotifyProvider{})
}

func (gotifyProvider) Type() string {
	return "gotify"
}

func (gotifyProvider) Async() bool {
	return true
}

func (gotifyProvider) MaskConfig(cfg Config) Config {
	cfg.GotifyToken = maskSecret(cfg.GotifyToken, maskedToken)
	return cfg
}

func (gotifyProvider) PreserveConfig(incoming, existing Config) Config {
	incoming.GotifyToken = preserveIfMasked(strings.TrimSpace(incoming.GotifyToken), existing.GotifyToken, maskedToken)
	return incoming
}

func (gotifyProvider) Validate(agent Agent) error {
	if strings.TrimSpace(agent.Config.GotifyURL) == "" || strings.TrimSpace(agent.Config.GotifyToken) == "" {
		return fmt.Errorf("gotify URL and token are required")
	}
	return nil
}

func (g gotifyProvider) Test(runtime Runtime, agent Agent) ([]TestResult, error) {
	cfg := agent.Config
	if strings.TrimSpace(cfg.GotifyURL) == "" || strings.TrimSpace(cfg.GotifyToken) == "" {
		return nil, fmt.Errorf("URL and token are required")
	}
	if runtime.NotifyClient == nil {
		return nil, fmt.Errorf("gotify client not configured")
	}

	res := TestResult{Label: "Gotify", Status: statusOK}
	payload := map[string]any{
		"title":    "Clonarr Test",
		"message":  "If you see this, Gotify is configured correctly!",
		"priority": 5,
		"extras":   map[string]any{"client::display": map[string]string{"contentType": "text/markdown"}},
	}
	body, _ := json.Marshal(payload)
	gotifyURL := strings.TrimRight(cfg.GotifyURL, "/") + "/message?token=" + url.QueryEscape(cfg.GotifyToken)
	resp, err := runtime.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		res.Status = statusError
		res.Error = fmt.Sprintf("Failed to reach Gotify: %v", err)
		return []TestResult{res}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		res.Status = statusError
		res.Error = fmt.Sprintf("Gotify returned %d", resp.StatusCode)
	}

	return []TestResult{res}, nil
}

func (g gotifyProvider) Notify(runtime Runtime, agent Agent, payload Payload) error {
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

	gotifyURL := strings.TrimRight(cfg.GotifyURL, "/") + "/message?token=" + url.QueryEscape(cfg.GotifyToken)
	resp, err := runtime.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify returned %d", resp.StatusCode)
	}

	return nil
}

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

func normalizeGotifyMarkdown(message string) string {
	msg := message
	msg = strings.ReplaceAll(msg, "\n**", "\n\n**")
	msg = strings.ReplaceAll(msg, "\n- ", "\n\n- ")
	for strings.Contains(msg, "\n\n\n") {
		msg = strings.ReplaceAll(msg, "\n\n\n", "\n\n")
	}
	return msg
}
