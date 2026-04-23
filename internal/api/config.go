package api

import (
	"clonarr/internal/auth"
	"clonarr/internal/core"
	"clonarr/internal/netsec"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
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
	// Mask notification secrets embedded in NotificationAgents (PR #15 layout).
	for i, a := range cfg.AutoSync.NotificationAgents {
		cfg.AutoSync.NotificationAgents[i].Config = maskAgentConfig(a.Type, a.Config)
	}
	// Wrap config with version for frontend
	writeJSON(w, struct {
		core.Config
		Version string `json:"version"`
	}{cfg, s.Core.Version})
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	s.updateConfigMu.Lock()
	defer s.updateConfigMu.Unlock()

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	// Read body once so we can decode into both the main ConfigData and a
	// small side struct that picks up `confirm_password` (never persisted).
	bodyBytes, rerr := io.ReadAll(r.Body)
	if rerr != nil {
		writeError(w, 400, "Failed to read request body")
		return
	}
	var req struct {
		TrashRepo              *core.TrashRepo      `json:"trashRepo,omitempty"`
		PullInterval           *string              `json:"pullInterval,omitempty"`
		DevMode                *bool                `json:"devMode,omitempty"`
		TrashSchemaFields      *bool                `json:"trashSchemaFields,omitempty"`
		DebugLogging           *bool                `json:"debugLogging"`
		Prowlarr               *core.ProwlarrConfig `json:"prowlarr,omitempty"`
		Authentication         *string              `json:"authentication,omitempty"`
		AuthenticationRequired *string              `json:"authenticationRequired,omitempty"`
		TrustedProxies         *string              `json:"trustedProxies,omitempty"`
		TrustedNetworks        *string              `json:"trustedNetworks,omitempty"`
		SessionTTLDays         *int                 `json:"sessionTtlDays,omitempty"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	var confirm struct {
		ConfirmPassword string `json:"confirm_password"`
	}
	_ = json.Unmarshal(bodyBytes, &confirm)

	// ==== Auth-field validation (before touching disk) =====================
	if req.Authentication != nil {
		switch *req.Authentication {
		case "forms", "basic", "none":
			// ok
		default:
			writeError(w, 400, "authentication must be one of: forms, basic, none")
			return
		}
	}
	if req.AuthenticationRequired != nil {
		switch *req.AuthenticationRequired {
		case "enabled", "disabled_for_local_addresses":
			// ok
		default:
			writeError(w, 400, "authenticationRequired must be one of: enabled, disabled_for_local_addresses")
			return
		}
	}
	if req.SessionTTLDays != nil {
		if *req.SessionTTLDays <= 0 || *req.SessionTTLDays > 365 {
			writeError(w, 400, "sessionTtlDays must be an integer 1..365")
			return
		}
	}
	// Env-lock enforcement: when the field is pinned via TRUSTED_PROXIES /
	// TRUSTED_NETWORKS env var, reject any submission that tries to CHANGE
	// it. Submissions matching either the env-locked effective value or
	// the existing on-disk value are accepted — this prevents clients
	// that happen to include the field in their body (e.g. a partial PUT
	// that snapshots current state) from being bounced with 403.
	if req.TrustedProxies != nil {
		if s.AuthStore != nil && s.AuthStore.TrustedProxiesLocked() {
			effective := s.AuthStore.TrustedProxiesRaw()
			existing := s.Core.Config.Get().TrustedProxies
			if *req.TrustedProxies != effective && *req.TrustedProxies != existing {
				writeError(w, 403, "trustedProxies is locked by the TRUSTED_PROXIES environment variable. Edit the Unraid template / docker-compose file and restart the container to change it.")
				return
			}
		}
		if *req.TrustedProxies != "" {
			if _, perr := netsec.ParseTrustedProxies(*req.TrustedProxies); perr != nil {
				writeError(w, 400, fmt.Sprintf("trustedProxies: %v", perr))
				return
			}
		}
	}
	if req.TrustedNetworks != nil {
		if s.AuthStore != nil && s.AuthStore.TrustedNetworksLocked() {
			effective := s.AuthStore.TrustedNetworksRaw()
			existing := s.Core.Config.Get().TrustedNetworks
			if *req.TrustedNetworks != effective && *req.TrustedNetworks != existing {
				writeError(w, 403, "trustedNetworks is locked by the TRUSTED_NETWORKS environment variable. Edit the Unraid template / docker-compose file and restart the container to change it.")
				return
			}
		}
		if *req.TrustedNetworks != "" {
			if _, perr := netsec.ParseTrustedNetworks(*req.TrustedNetworks); perr != nil {
				writeError(w, 400, fmt.Sprintf("trustedNetworks: %v", perr))
				return
			}
		}
	}

	// Any save that sets authentication=none requires the current admin password.
	if req.Authentication != nil && *req.Authentication == "none" && s.AuthStore != nil {
		if confirm.ConfirmPassword == "" {
			writeError(w, 400, "Saving with authentication=none requires your current password in the confirm_password field of the request body.")
			return
		}
		if !s.AuthStore.VerifyPassword(s.AuthStore.Username(), confirm.ConfirmPassword) {
			writeError(w, 401, "Current password is incorrect. Authentication change aborted.")
			return
		}
	}

	pullChanged := false
	authChanged := false
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
		if req.TrashSchemaFields != nil {
			cfg.TrashSchemaFields = *req.TrashSchemaFields
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
		if req.Authentication != nil {
			cfg.Authentication = *req.Authentication
			authChanged = true
		}
		if req.AuthenticationRequired != nil {
			cfg.AuthenticationRequired = *req.AuthenticationRequired
			authChanged = true
		}
		if req.TrustedProxies != nil {
			cfg.TrustedProxies = *req.TrustedProxies
			authChanged = true
		}
		if req.TrustedNetworks != nil {
			cfg.TrustedNetworks = *req.TrustedNetworks
			authChanged = true
		}
		if req.SessionTTLDays != nil {
			cfg.SessionTTLDays = *req.SessionTTLDays
			authChanged = true
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

	// Live-reload auth config so the change takes effect immediately
	if authChanged && s.AuthStore != nil {
		cfg := s.Core.Config.Get()
		newAuthCfg := s.AuthStore.Config()
		if cfg.Authentication != "" {
			newAuthCfg.Mode = auth.AuthMode(cfg.Authentication)
		}
		if cfg.AuthenticationRequired != "" {
			newAuthCfg.Requirement = auth.Requirement(cfg.AuthenticationRequired)
		}
		if cfg.SessionTTLDays > 0 {
			newAuthCfg.SessionTTL = time.Duration(cfg.SessionTTLDays) * 24 * time.Hour
		}
		if !s.AuthStore.TrustedProxiesLocked() {
			if ips, perr := netsec.ParseTrustedProxies(cfg.TrustedProxies); perr == nil {
				newAuthCfg.TrustedProxies = ips
			}
		}
		if !s.AuthStore.TrustedNetworksLocked() {
			if nets, perr := netsec.ParseTrustedNetworks(cfg.TrustedNetworks); perr == nil {
				newAuthCfg.TrustedNetworks = nets
			}
		}
		if uerr := s.AuthStore.UpdateConfig(newAuthCfg); uerr != nil {
			log.Printf("auth live-reload failed: %v — will take effect on next restart", uerr)
			writeJSON(w, map[string]any{"status": "saved", "auth_reload_error": uerr.Error()})
			return
		}
	}

	writeJSON(w, map[string]string{"status": "saved"})
}
