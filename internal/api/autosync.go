package api

import (
	"encoding/json"
	"log"
	"net/http"

	"clonarr/internal/core"
)

// handleGetAutoSyncSettings returns the minimal auto-sync config (notification
// agents are served by handleListNotificationAgents).
func (s *Server) handleGetAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	writeJSON(w, map[string]any{
		"enabled": cfg.AutoSync.Enabled,
	})
}

// handleSaveAutoSyncSettings updates the top-level enabled flag. Notification
// agents are managed via /api/auto-sync/notification-agents.
func (s *Server) handleSaveAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Enabled bool `json:"enabled"`
	}](w, r, 4096)
	if !ok {
		return
	}
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		cfg.AutoSync.Enabled = req.Enabled
	}); err != nil {
		log.Printf("Error saving auto-sync settings: %v", err)
		writeError(w, 500, "Failed to save settings")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

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


