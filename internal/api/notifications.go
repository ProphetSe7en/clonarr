package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"clonarr/internal/core"
)

// maskAgentConfig returns a copy of NotificationConfig with credentials masked.
// Gotify URL is not a bearer credential and is returned as-is.
func maskAgentConfig(agentType string, nc core.NotificationConfig) core.NotificationConfig {
	switch agentType {
	case "discord":
		nc.DiscordWebhook = maskSecret(nc.DiscordWebhook, maskedDiscordWebhook)
		nc.DiscordWebhookUpdates = maskSecret(nc.DiscordWebhookUpdates, maskedDiscordWebhook)
	case "gotify":
		nc.GotifyToken = maskSecret(nc.GotifyToken, maskedToken)
	case "pushover":
		nc.PushoverUserKey = maskSecret(nc.PushoverUserKey, maskedToken)
		nc.PushoverAppToken = maskSecret(nc.PushoverAppToken, maskedToken)
	}
	return nc
}

// preserveAgentConfig applies preserveIfMasked to each credential field,
// keeping the stored value when the UI returns the masked placeholder.
func preserveAgentConfig(agentType string, incoming, existing core.NotificationConfig) core.NotificationConfig {
	switch agentType {
	case "discord":
		incoming.DiscordWebhook = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhook), existing.DiscordWebhook, maskedDiscordWebhook)
		incoming.DiscordWebhookUpdates = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhookUpdates), existing.DiscordWebhookUpdates, maskedDiscordWebhook)
	case "gotify":
		incoming.GotifyToken = preserveIfMasked(strings.TrimSpace(incoming.GotifyToken), existing.GotifyToken, maskedToken)
	case "pushover":
		incoming.PushoverUserKey = preserveIfMasked(strings.TrimSpace(incoming.PushoverUserKey), existing.PushoverUserKey, maskedToken)
		incoming.PushoverAppToken = preserveIfMasked(strings.TrimSpace(incoming.PushoverAppToken), existing.PushoverAppToken, maskedToken)
	}
	return incoming
}

// validateAgentConfig checks that required fields are present for the agent type.
func validateAgentConfig(agent core.NotificationAgent) error {
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch agent.Type {
	case "discord":
		if strings.TrimSpace(agent.Config.DiscordWebhook) == "" {
			return fmt.Errorf("discord webhook is required")
		}
		webhook := strings.TrimSpace(agent.Config.DiscordWebhook)
		if !strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
			!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
			return fmt.Errorf("discord webhook must start with https://discord.com/api/webhooks/")
		}
		// Validate the optional updates webhook only when it has been provided.
		if u := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); u != "" {
			if !strings.HasPrefix(u, "https://discord.com/api/webhooks/") &&
				!strings.HasPrefix(u, "https://discordapp.com/api/webhooks/") {
				return fmt.Errorf("discord updates webhook must start with https://discord.com/api/webhooks/")
			}
		}
	case "gotify":
		if strings.TrimSpace(agent.Config.GotifyURL) == "" || strings.TrimSpace(agent.Config.GotifyToken) == "" {
			return fmt.Errorf("gotify URL and token are required")
		}
	case "pushover":
		if strings.TrimSpace(agent.Config.PushoverUserKey) == "" || strings.TrimSpace(agent.Config.PushoverAppToken) == "" {
			return fmt.Errorf("pushover user key and app token are required")
		}
	default:
		return fmt.Errorf("unknown agent type: %q (expected discord | gotify | pushover)", agent.Type)
	}
	return nil
}

// handleListNotificationAgents returns all configured notification agents
// with credentials masked.
func (s *Server) handleListNotificationAgents(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	agents := cfg.AutoSync.NotificationAgents
	if agents == nil {
		agents = []core.NotificationAgent{}
	}
	for i, a := range agents {
		agents[i].Config = maskAgentConfig(a.Type, a.Config)
	}
	writeJSON(w, agents)
}

// handleCreateNotificationAgent adds a new notification agent.
func (s *Server) handleCreateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	agent, ok := decodeJSON[core.NotificationAgent](w, r, 8192)
	if !ok {
		return
	}
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	created, err := s.Core.Config.AddNotificationAgent(agent)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	created.Config = maskAgentConfig(created.Type, created.Config)
	writeJSON(w, created)
}

// handleUpdateNotificationAgent replaces a notification agent by ID.
func (s *Server) handleUpdateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agent, ok := decodeJSON[core.NotificationAgent](w, r, 8192)
	if !ok {
		return
	}
	existing, found := s.Core.Config.GetNotificationAgent(id)
	if !found {
		writeError(w, 404, "Notification agent not found")
		return
	}
	agent.Config = preserveAgentConfig(agent.Type, agent.Config, existing.Config)
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated, err := s.Core.Config.UpdateNotificationAgent(id, agent)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	updated.Config = maskAgentConfig(updated.Type, updated.Config)
	writeJSON(w, updated)
}

// handleDeleteNotificationAgent removes a notification agent by ID.
func (s *Server) handleDeleteNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Core.Config.DeleteNotificationAgent(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// testResult captures the outcome of a single notification-channel probe.
type testResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status == "error"
}

// handleTestNotificationAgentInline tests agent credentials sent inline in the
// request body without requiring a saved agent ID. Used by the add-agent modal.
func (s *Server) handleTestNotificationAgentInline(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[core.NotificationAgent](w, r, 4096)
	if !ok {
		return
	}
	s.runNotificationAgentTest(w, req)
}

// handleTestNotificationAgent fires test messages for an existing saved agent.
func (s *Server) handleTestNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := s.Core.Config.GetNotificationAgent(id)
	if !ok {
		writeError(w, 404, "Notification agent not found")
		return
	}
	s.runNotificationAgentTest(w, existing)
}

// runNotificationAgentTest executes the test logic for any notification agent
// and writes the JSON response. Shared by both the inline and saved-agent test handlers.
func (s *Server) runNotificationAgentTest(w http.ResponseWriter, req core.NotificationAgent) {
	var results []testResult

	switch req.Type {
	case "discord":
		nc := req.Config
		if nc.DiscordWebhook != "" {
			res := testResult{Label: "Sync webhook", Status: "ok"}
			if err := s.testDiscordWebhook(nc.DiscordWebhook); err != nil {
				res.Status = "error"
				res.Error = err.Error()
			}
			results = append(results, res)
		}
		if nc.DiscordWebhookUpdates != "" && nc.DiscordWebhookUpdates != nc.DiscordWebhook {
			res := testResult{Label: "Updates webhook", Status: "ok"}
			if err := s.testDiscordWebhook(nc.DiscordWebhookUpdates); err != nil {
				res.Status = "error"
				res.Error = err.Error()
			}
			results = append(results, res)
		}
		if len(results) == 0 {
			writeError(w, 400, "At least one webhook URL is required")
			return
		}

	case "gotify":
		nc := req.Config
		if nc.GotifyURL == "" || nc.GotifyToken == "" {
			writeError(w, 400, "URL and token are required")
			return
		}
		res := testResult{Label: "Gotify", Status: "ok"}
		payload := map[string]any{
			"title":    "Clonarr Test",
			"message":  "If you see this, Gotify is configured correctly!",
			"priority": 5,
			"extras":   map[string]any{"client::display": map[string]string{"contentType": "text/markdown"}},
		}
		body, _ := json.Marshal(payload)
		gotifyURL := strings.TrimRight(nc.GotifyURL, "/") + "/message?token=" + url.QueryEscape(nc.GotifyToken)
		resp, err := s.Core.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
		if err != nil {
			res.Status = "error"
			res.Error = fmt.Sprintf("Failed to reach Gotify: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				res.Status = "error"
				res.Error = fmt.Sprintf("Gotify returned %d", resp.StatusCode)
			}
		}
		results = append(results, res)

	case "pushover":
		nc := req.Config
		if nc.PushoverUserKey == "" || nc.PushoverAppToken == "" {
			writeError(w, 400, "User key and app token are required")
			return
		}
		res := testResult{Label: "Pushover", Status: "ok"}
		payload := map[string]any{
			"token":    nc.PushoverAppToken,
			"user":     nc.PushoverUserKey,
			"title":    "Clonarr Test",
			"message":  "If you see this, Pushover is configured correctly!",
			"priority": 0,
		}
		body, _ := json.Marshal(payload)
		resp, err := s.Core.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
		if err != nil {
			res.Status = "error"
			res.Error = fmt.Sprintf("Failed to reach Pushover: %v", err)
		} else {
			resp.Body.Close()
			if resp.StatusCode >= 400 {
				res.Status = "error"
				res.Error = fmt.Sprintf("Pushover returned %d", resp.StatusCode)
			}
		}
		results = append(results, res)

	default:
		writeError(w, 400, "Unknown agent type: "+req.Type)
		return
	}

	writeJSON(w, map[string]any{"results": results})
}

// testDiscordWebhook sends a test embed to a Discord webhook and returns any error.
func (s *Server) testDiscordWebhook(webhook string) error {
	webhook = strings.TrimSpace(webhook)
	if !strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
		return fmt.Errorf("must start with https://discord.com/api/webhooks/")
	}
	embed := map[string]any{
		"title":       "Clonarr Test",
		"description": "If you see this, Discord is configured correctly!",
		"color":       0x58a6ff,
		"footer":      map[string]string{"text": "Clonarr " + s.Core.Version + " by ProphetSe7en"},
	}
	payload, _ := json.Marshal(map[string]any{"embeds": []any{embed}})
	resp, err := s.Core.SafeClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned %d", resp.StatusCode)
	}
	return nil
}
