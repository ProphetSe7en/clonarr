package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
)

// --- core.Config ---

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get() // deep copy from ConfigStore
	// Mask API keys in the copy (M11: safe because Get() returns deep copy)
	for i := range cfg.Instances {
		if cfg.Instances[i].APIKey != "" {
			cfg.Instances[i].APIKey = maskKey(cfg.Instances[i].APIKey)
		}
	}
	// Mask Prowlarr API key
	if cfg.Prowlarr.APIKey != "" {
		cfg.Prowlarr.APIKey = maskKey(cfg.Prowlarr.APIKey)
	}
	// Wrap config with version for frontend
	writeJSON(w, struct {
		core.Config
		Version string `json:"version"`
	}{cfg, s.Core.Version})
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var req struct {
		TrashRepo    *core.TrashRepo      `json:"trashRepo,omitempty"`
		PullInterval *string              `json:"pullInterval,omitempty"`
		DevMode      *bool                `json:"devMode,omitempty"`
		DebugLogging *bool                `json:"debugLogging"`
		Prowlarr     *core.ProwlarrConfig `json:"prowlarr,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	pullChanged := false
	err := s.Core.Config.Update(func(cfg *core.Config) {
		if req.TrashRepo != nil {
			if req.TrashRepo.URL != "" {
				cfg.TrashRepo.URL = req.TrashRepo.URL
			}
			if req.TrashRepo.Branch != "" {
				cfg.TrashRepo.Branch = req.TrashRepo.Branch
			}
		}
		if req.PullInterval != nil {
			cfg.PullInterval = *req.PullInterval
			pullChanged = true
		}
		if req.DevMode != nil {
			cfg.DevMode = *req.DevMode
		}
		if req.DebugLogging != nil {
			cfg.DebugLogging = *req.DebugLogging
			s.Core.DebugLog.SetEnabled(*req.DebugLogging)
		}
		if req.Prowlarr != nil {
			// Preserve existing API key if masked
			if isMasked(req.Prowlarr.APIKey) {
				req.Prowlarr.APIKey = cfg.Prowlarr.APIKey
			}
			cfg.Prowlarr = *req.Prowlarr
		}
	})
	if err != nil {
		writeError(w, 500, "Failed to save config")
		return
	}

	// Notify pull goroutine of schedule change
	if pullChanged {
		cfg := s.Core.Config.Get()
		select {
		case s.Core.PullUpdateCh <- cfg.PullInterval:
		default:
		}
	}

	writeJSON(w, map[string]string{"status": "saved"})
}
