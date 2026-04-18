package api

import (
	"clonarr/internal/core"

	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
)

// --- Auto-Sync handlers ---

// handleGetAutoSyncSettings returns the global auto-sync settings (without rules).
func (s *Server) handleGetAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	writeJSON(w, map[string]any{
		"enabled":                cfg.AutoSync.Enabled,
		"notifyOnSuccess":        cfg.AutoSync.NotifyOnSuccess,
		"notifyOnFailure":        cfg.AutoSync.NotifyOnFailure,
		"notifyOnRepoUpdate":     cfg.AutoSync.NotifyOnRepoUpdate,
		"discordEnabled":         cfg.AutoSync.DiscordEnabled,
		"discordWebhook":         cfg.AutoSync.DiscordWebhook,
		"discordWebhookUpdates":  cfg.AutoSync.DiscordWebhookUpdates,
		"gotifyEnabled":          cfg.AutoSync.GotifyEnabled,
		"gotifyUrl":              cfg.AutoSync.GotifyURL,
		"gotifyToken":            cfg.AutoSync.GotifyToken,
		"gotifyPriorityCritical": cfg.AutoSync.GotifyPriorityCritical,
		"gotifyPriorityWarning":  cfg.AutoSync.GotifyPriorityWarning,
		"gotifyPriorityInfo":     cfg.AutoSync.GotifyPriorityInfo,
		"gotifyCriticalValue":    cfg.AutoSync.GotifyCriticalValue,
		"gotifyWarningValue":     cfg.AutoSync.GotifyWarningValue,
		"gotifyInfoValue":        cfg.AutoSync.GotifyInfoValue,
		"pushoverEnabled":        cfg.AutoSync.PushoverEnabled,
		"pushoverUserKey":        cfg.AutoSync.PushoverUserKey,
		"pushoverAppToken":       cfg.AutoSync.PushoverAppToken,
	})
}

// handleSaveAutoSyncSettings updates global auto-sync settings.
func (s *Server) handleSaveAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Enabled                bool   `json:"enabled"`
		NotifyOnSuccess        bool   `json:"notifyOnSuccess"`
		NotifyOnFailure        bool   `json:"notifyOnFailure"`
		NotifyOnRepoUpdate     bool   `json:"notifyOnRepoUpdate"`
		DiscordEnabled         bool   `json:"discordEnabled"`
		DiscordWebhook         string `json:"discordWebhook"`
		DiscordWebhookUpdates  string `json:"discordWebhookUpdates"`
		GotifyEnabled          bool   `json:"gotifyEnabled"`
		GotifyURL              string `json:"gotifyUrl"`
		GotifyToken            string `json:"gotifyToken"`
		GotifyPriorityCritical bool   `json:"gotifyPriorityCritical"`
		GotifyPriorityWarning  bool   `json:"gotifyPriorityWarning"`
		GotifyPriorityInfo     bool   `json:"gotifyPriorityInfo"`
		GotifyCriticalValue    int    `json:"gotifyCriticalValue"`
		GotifyWarningValue     int    `json:"gotifyWarningValue"`
		GotifyInfoValue        int    `json:"gotifyInfoValue"`
		PushoverEnabled        bool   `json:"pushoverEnabled"`
		PushoverUserKey        string `json:"pushoverUserKey"`
		PushoverAppToken       string `json:"pushoverAppToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	webhook := strings.TrimSpace(req.DiscordWebhook)
	if webhook != "" &&
		!strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
		writeError(w, 400, "Discord webhook must start with https://discord.com/api/webhooks/")
		return
	}
	webhookUpdates := strings.TrimSpace(req.DiscordWebhookUpdates)
	if webhookUpdates != "" &&
		!strings.HasPrefix(webhookUpdates, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhookUpdates, "https://discordapp.com/api/webhooks/") {
		writeError(w, 400, "Discord updates webhook must start with https://discord.com/api/webhooks/")
		return
	}
	gotifyURL := strings.TrimSpace(req.GotifyURL)
	if gotifyURL != "" && !strings.HasPrefix(gotifyURL, "http://") && !strings.HasPrefix(gotifyURL, "https://") {
		writeError(w, 400, "Gotify URL must start with http:// or https://")
		return
	}

	if err := s.Core.Config.Update(func(cfg *core.Config) {
		cfg.AutoSync.Enabled = req.Enabled
		cfg.AutoSync.NotifyOnSuccess = req.NotifyOnSuccess
		cfg.AutoSync.NotifyOnFailure = req.NotifyOnFailure
		cfg.AutoSync.NotifyOnRepoUpdate = req.NotifyOnRepoUpdate
		de := req.DiscordEnabled
		cfg.AutoSync.DiscordEnabled = &de
		cfg.AutoSync.DiscordWebhook = webhook
		cfg.AutoSync.DiscordWebhookUpdates = webhookUpdates
		cfg.AutoSync.GotifyEnabled = req.GotifyEnabled
		cfg.AutoSync.GotifyURL = gotifyURL
		cfg.AutoSync.GotifyToken = strings.TrimSpace(req.GotifyToken)
		cfg.AutoSync.GotifyPriorityCritical = req.GotifyPriorityCritical
		cfg.AutoSync.GotifyPriorityWarning = req.GotifyPriorityWarning
		cfg.AutoSync.GotifyPriorityInfo = req.GotifyPriorityInfo
		cv, wv, iv := req.GotifyCriticalValue, req.GotifyWarningValue, req.GotifyInfoValue
		cfg.AutoSync.GotifyCriticalValue = &cv
		cfg.AutoSync.GotifyWarningValue = &wv
		cfg.AutoSync.GotifyInfoValue = &iv
		cfg.AutoSync.PushoverEnabled = req.PushoverEnabled
		cfg.AutoSync.PushoverUserKey = strings.TrimSpace(req.PushoverUserKey)
		cfg.AutoSync.PushoverAppToken = strings.TrimSpace(req.PushoverAppToken)
	}); err != nil {
		writeError(w, 500, "Failed to save settings")
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleTestGotify(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" || req.Token == "" {
		writeError(w, 400, "url and token required")
		return
	}
	payload := map[string]any{
		"title":    "Clonarr Test",
		"message":  "If you see this, Gotify is configured correctly!",
		"priority": 5,
		"extras": map[string]any{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	}
	body, _ := json.Marshal(payload)
	gotifyURL := strings.TrimRight(req.URL, "/") + "/message?token=" + url.QueryEscape(req.Token)
	resp, err := s.Core.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		writeError(w, 502, fmt.Sprintf("Failed to reach Gotify: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeError(w, resp.StatusCode, fmt.Sprintf("Gotify returned %d", resp.StatusCode))
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleTestDiscord(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Webhook string `json:"webhook"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Webhook == "" {
		writeError(w, 400, "webhook required")
		return
	}
	webhook := strings.TrimSpace(req.Webhook)
	if !strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
		writeError(w, 400, "Discord webhook must start with https://discord.com/api/webhooks/")
		return
	}
	embed := map[string]any{
		"title":       "Clonarr Test",
		"description": "If you see this, Discord is configured correctly!",
		"color":       0x58a6ff,
		"footer":      map[string]string{"text": "Clonarr " + s.Core.Version + " by ProphetSe7en"},
	}
	payload, _ := json.Marshal(map[string]any{"embeds": []any{embed}})
	resp, err := s.Core.NotifyClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		writeError(w, 502, fmt.Sprintf("Failed to reach Discord: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeError(w, resp.StatusCode, fmt.Sprintf("Discord returned %d", resp.StatusCode))
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleTestPushover(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		UserKey  string `json:"userKey"`
		AppToken string `json:"appToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserKey == "" || req.AppToken == "" {
		writeError(w, 400, "userKey and appToken required")
		return
	}
	payload := map[string]any{
		"token":    strings.TrimSpace(req.AppToken),
		"user":     strings.TrimSpace(req.UserKey),
		"title":    "Clonarr Test",
		"message":  "If you see this, Pushover is configured correctly!",
		"priority": 0,
	}
	body, _ := json.Marshal(payload)
	resp, err := s.Core.NotifyClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		writeError(w, 502, fmt.Sprintf("Failed to reach Pushover: %v", err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		writeError(w, resp.StatusCode, fmt.Sprintf("Pushover returned %d", resp.StatusCode))
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleListAutoSyncRules returns all auto-sync rules with instance names resolved.
func (s *Server) handleListAutoSyncRules(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()

	type ruleResponse struct {
		core.AutoSyncRule
		InstanceName string `json:"instanceName"`
		InstanceType string `json:"instanceType"`
	}

	rules := make([]ruleResponse, 0, len(cfg.AutoSync.Rules))
	for _, rule := range cfg.AutoSync.Rules {
		rr := ruleResponse{AutoSyncRule: rule}
		if inst, ok := s.Core.Config.GetInstance(rule.InstanceID); ok {
			rr.InstanceName = inst.Name
			rr.InstanceType = inst.Type
		}
		rules = append(rules, rr)
	}
	writeJSON(w, rules)
}

// handleCreateAutoSyncRule creates a new auto-sync rule.
func (s *Server) handleCreateAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rule core.AutoSyncRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	// Validate required fields
	if rule.InstanceID == "" {
		writeError(w, 400, "instanceId is required")
		return
	}
	if _, ok := s.Core.Config.GetInstance(rule.InstanceID); !ok {
		writeError(w, 400, "Instance not found")
		return
	}
	if rule.ProfileSource != "trash" && rule.ProfileSource != "imported" {
		writeError(w, 400, "profileSource must be 'trash' or 'imported'")
		return
	}
	if rule.ProfileSource == "trash" && rule.TrashProfileID == "" {
		writeError(w, 400, "trashProfileId is required for trash profiles")
		return
	}
	if rule.ProfileSource == "imported" && rule.ImportedProfileID == "" {
		writeError(w, 400, "importedProfileId is required for imported profiles")
		return
	}

	rule.ID = core.GenerateID()

	// Check for duplicate inside Update callback to avoid TOCTOU race
	var duplicate bool
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		for _, existing := range cfg.AutoSync.Rules {
			if existing.InstanceID == rule.InstanceID && existing.ArrProfileID == rule.ArrProfileID {
				duplicate = true
				return
			}
		}
		cfg.AutoSync.Rules = append(cfg.AutoSync.Rules, rule)
	}); err != nil {
		log.Printf("Failed to save auto-sync rule: %v", err)
		writeError(w, 500, "Failed to save rule")
		return
	}
	if duplicate {
		writeError(w, 409, "Auto-sync rule already exists for this profile and instance")
		return
	}

	writeJSON(w, rule)
}

// handleUpdateAutoSyncRule updates an existing auto-sync rule.
func (s *Server) handleUpdateAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rule core.AutoSyncRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	rule.ID = id

	found := false
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == id {
				rule.LastSyncCommit = cfg.AutoSync.Rules[i].LastSyncCommit
				rule.LastSyncTime = cfg.AutoSync.Rules[i].LastSyncTime
				// Frontend controls lastSyncError — passes current value or empty to clear
				cfg.AutoSync.Rules[i] = rule
				found = true
				return
			}
		}
	}); err != nil {
		log.Printf("Failed to update auto-sync rule: %v", err)
		writeError(w, 500, "Failed to save rule")
		return
	}

	if !found {
		writeError(w, 404, "Rule not found")
		return
	}

	writeJSON(w, rule)
}

// handleDeleteAutoSyncRule deletes an auto-sync rule.
func (s *Server) handleDeleteAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	found := false
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == id {
				cfg.AutoSync.Rules = append(cfg.AutoSync.Rules[:i], cfg.AutoSync.Rules[i+1:]...)
				found = true
				return
			}
		}
	}); err != nil {
		log.Printf("Failed to delete auto-sync rule: %v", err)
		writeError(w, 500, "Failed to delete rule")
		return
	}

	if !found {
		writeError(w, 404, "Rule not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


