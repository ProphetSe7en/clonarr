package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clonarr/auth"
	"clonarr/netsec"
)

// Fallback for Radarr/Sonarr's parse API returning empty ReleaseGroup when the
// trailing group token is numeric only (e.g. "…Atmos-126811"). Narrow enough to
// leave alphanumeric groups (handled correctly by Arr) and codec tokens alone:
// matches only a trailing dash followed by pure digits at end of string.
var numericReleaseGroupRE = regexp.MustCompile(`-(\d+)$`)

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	// No-store prevents shared browser caches + reverse-proxy caches from
	// retaining /api/* responses. Even with masking, a 4+4 API-key reveal
	// or config blob shouldn't live on a kiosk browser after logout.
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

// ==== Credential masking =====================================================
// Discord webhook URLs and Gotify/Pushover tokens are bearer credentials —
// whoever holds them can post messages as Clonarr. They must not appear in
// plaintext in responses so a compromised session, local-bypass peer, or
// leaked API call log cannot exfiltrate them. Masking pattern:
//   - GET responses return a placeholder form
//   - SAVE handlers detect the placeholder on input and preserve the stored
//     value instead of overwriting with the placeholder (so "Save" without
//     edits is a no-op for secrets).

const (
	maskedDiscordWebhook = "https://discord.com/api/webhooks/[MASKED]/[MASKED]"
	maskedToken          = "••••••••••••••••" // 16 bullets — looks different from any real token
)

// maskSecret returns the placeholder if s is non-empty; otherwise empty.
// Empty stays empty so the UI can distinguish "not set" from "set but hidden".
func maskSecret(s, placeholder string) string {
	if s == "" {
		return ""
	}
	return placeholder
}

// preserveIfMasked returns existing when incoming equals the placeholder —
// i.e. the UI returned the masked value unchanged. Otherwise returns incoming
// (including empty string, which represents explicit deletion).
func preserveIfMasked(incoming, existing, placeholder string) string {
	if incoming == placeholder {
		return existing
	}
	return incoming
}

// maskAgentConfig returns a copy of NotificationConfig with credentials masked.
// Gotify URL is not a bearer credential and is returned as-is.
func maskAgentConfig(agentType string, nc NotificationConfig) NotificationConfig {
	switch agentType {
	case "discord":
		nc.DiscordWebhook = maskSecret(nc.DiscordWebhook, maskedDiscordWebhook)
		nc.DiscordWebhookUpdates = maskSecret(nc.DiscordWebhookUpdates, maskedDiscordWebhook)
	case "gotify":
		// GotifyURL is intentionally not masked — not a bearer credential
		nc.GotifyToken = maskSecret(nc.GotifyToken, maskedToken)
	case "pushover":
		nc.PushoverUserKey = maskSecret(nc.PushoverUserKey, maskedToken)
		nc.PushoverAppToken = maskSecret(nc.PushoverAppToken, maskedToken)
	}
	return nc
}

// preserveAgentConfig applies preserveIfMasked to each credential field,
// keeping the stored value when the UI returns the masked placeholder.
func preserveAgentConfig(agentType string, incoming, existing NotificationConfig) NotificationConfig {
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

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON reads and decodes JSON from the request body into T, enforcing a size limit.
// Returns the decoded value and true on success, or writes an error response and returns false.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, 400, "Invalid JSON")
		return v, false
	}
	return v, true
}

// requireInstance looks up an instance by the "id" path parameter.
// Returns the instance and true on success, or writes an error response and returns false.
func (app *App) requireInstance(w http.ResponseWriter, r *http.Request) (Instance, bool) {
	id := r.PathValue("id")
	inst, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
	}
	return inst, ok
}

// --- Config ---

func (app *App) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get() // deep copy from configStore
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
	// Mask notification secrets embedded in NotificationAgents.
	// Credentials must not appear in plaintext in the bulk config response.
	for i, a := range cfg.AutoSync.NotificationAgents {
		cfg.AutoSync.NotificationAgents[i].Config = maskAgentConfig(a.Type, a.Config)
	}
	// Wrap config with version for frontend
	writeJSON(w, struct {
		Config
		Version string `json:"version"`
	}{cfg, Version})
}

func (app *App) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	// Read body once so we can decode into both the main ConfigData and a
	// small side struct that picks up `confirm_password` (never persisted,
	// stays out of reverse-proxy access logs unlike an HTTP header would).
	bodyBytes, rerr := io.ReadAll(r.Body)
	if rerr != nil {
		writeError(w, 400, "Failed to read request body")
		return
	}
	var req struct {
		TrashRepo              *TrashRepo      `json:"trashRepo,omitempty"`
		PullInterval           *string         `json:"pullInterval,omitempty"`
		DevMode                *bool           `json:"devMode,omitempty"`
		DebugLogging           *bool           `json:"debugLogging"`
		Prowlarr               *ProwlarrConfig `json:"prowlarr,omitempty"`
		Authentication         *string         `json:"authentication,omitempty"`
		AuthenticationRequired *string         `json:"authenticationRequired,omitempty"`
		TrustedProxies         *string         `json:"trustedProxies,omitempty"`
		TrustedNetworks        *string         `json:"trustedNetworks,omitempty"`
		SessionTTLDays         *int            `json:"sessionTtlDays,omitempty"`
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
	if req.TrustedProxies != nil {
		if app.authStore != nil && app.authStore.TrustedProxiesLocked() {
			writeError(w, 403, "trustedProxies is locked by the TRUSTED_PROXIES environment variable. Edit the Unraid template / docker-compose file and restart the container to change it.")
			return
		}
		if *req.TrustedProxies != "" {
			if _, perr := netsec.ParseTrustedProxies(*req.TrustedProxies); perr != nil {
				writeError(w, 400, fmt.Sprintf("trustedProxies: %v", perr))
				return
			}
		}
	}
	if req.TrustedNetworks != nil {
		if app.authStore != nil && app.authStore.TrustedNetworksLocked() {
			writeError(w, 403, "trustedNetworks is locked by the TRUSTED_NETWORKS environment variable. Edit the Unraid template / docker-compose file and restart the container to change it.")
			return
		}
		if *req.TrustedNetworks != "" {
			if _, perr := netsec.ParseTrustedNetworks(*req.TrustedNetworks); perr != nil {
				writeError(w, 400, fmt.Sprintf("trustedNetworks: %v", perr))
				return
			}
		}
	}

	// Any save that sets authentication=none requires the current admin
	// password. Local-bypass peers can otherwise disable auth or modify
	// security-config fields without ever proving they know the password.
	if req.Authentication != nil && *req.Authentication == "none" && app.authStore != nil {
		if confirm.ConfirmPassword == "" {
			writeError(w, 400, "Saving with authentication=none requires your current password in the confirm_password field of the request body.")
			return
		}
		if !app.authStore.VerifyPassword(app.authStore.Username(), confirm.ConfirmPassword) {
			writeError(w, 401, "Current password is incorrect. Authentication change aborted.")
			return
		}
	}

	pullChanged := false
	authChanged := false
	err := app.config.Update(func(cfg *Config) {
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
			app.debugLog.SetEnabled(*req.DebugLogging)
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
		cfg := app.config.Get()
		select {
		case app.pullUpdateCh <- cfg.PullInterval:
		default:
		}
	}

	// Live-reload auth config so the change takes effect immediately
	// (no container restart needed).
	if authChanged && app.authStore != nil {
		cfg := app.config.Get()
		newAuthCfg := app.authStore.Config()
		if cfg.Authentication != "" {
			newAuthCfg.Mode = auth.AuthMode(cfg.Authentication)
		}
		if cfg.AuthenticationRequired != "" {
			newAuthCfg.Requirement = auth.Requirement(cfg.AuthenticationRequired)
		}
		if cfg.SessionTTLDays > 0 {
			newAuthCfg.SessionTTL = time.Duration(cfg.SessionTTLDays) * 24 * time.Hour
		}
		// Skip the parse-and-overwrite when env-locked: cfg.TrustedNetworks
		// on disk is typically empty when env provides the value, and
		// ParseTrustedNetworks("") returns (nil, nil) — which would clobber
		// the env-derived slice on any unrelated save (SessionTTL, etc).
		// UpdateConfig has a defense-in-depth guard too (C2 fix), but
		// guarding here keeps the intent explicit at the call site.
		if !app.authStore.TrustedProxiesLocked() {
			if ips, perr := netsec.ParseTrustedProxies(cfg.TrustedProxies); perr == nil {
				newAuthCfg.TrustedProxies = ips
			}
		}
		if !app.authStore.TrustedNetworksLocked() {
			if nets, perr := netsec.ParseTrustedNetworks(cfg.TrustedNetworks); perr == nil {
				newAuthCfg.TrustedNetworks = nets
			}
		}
		if uerr := app.authStore.UpdateConfig(newAuthCfg); uerr != nil {
			log.Printf("auth live-reload failed: %v — will take effect on next restart", uerr)
			writeJSON(w, map[string]any{"status": "saved", "auth_reload_error": uerr.Error()})
			return
		}
	}

	writeJSON(w, map[string]string{"status": "saved"})
}

// --- Instances ---

func (app *App) handleListInstances(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	instances := cfg.Instances
	if instances == nil {
		instances = []Instance{}
	}
	// Mask API keys (safe: Get() returns deep copy)
	for i := range instances {
		if instances[i].APIKey != "" {
			instances[i].APIKey = maskKey(instances[i].APIKey)
		}
	}
	writeJSON(w, instances)
}

func (app *App) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var inst Instance
	if err := json.NewDecoder(r.Body).Decode(&inst); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	if inst.Name == "" || inst.URL == "" || inst.APIKey == "" {
		writeError(w, 400, "name, url, and apiKey are required")
		return
	}
	if inst.Type != "radarr" && inst.Type != "sonarr" {
		writeError(w, 400, "type must be 'radarr' or 'sonarr'")
		return
	}

	created, err := app.config.AddInstance(inst)
	if err != nil {
		writeError(w, 500, "Failed to save instance")
		return
	}

	// M2: mask API key in response
	created.APIKey = maskKey(created.APIKey)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, created)
}

func (app *App) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var inst Instance
	if err := json.NewDecoder(r.Body).Decode(&inst); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	if inst.Name == "" || inst.URL == "" {
		writeError(w, 400, "name and url are required")
		return
	}
	if inst.Type != "radarr" && inst.Type != "sonarr" {
		writeError(w, 400, "type must be 'radarr' or 'sonarr'")
		return
	}

	// M9: If API key is empty or masked, keep the existing one
	if inst.APIKey == "" || isMasked(inst.APIKey) {
		existing, ok := app.config.GetInstance(id)
		if ok {
			inst.APIKey = existing.APIKey
		}
	}

	updated, err := app.config.UpdateInstance(id, inst)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}

	// M1: mask API key in response
	updated.APIKey = maskKey(updated.APIKey)
	writeJSON(w, updated)
}

func (app *App) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	if err := app.config.DeleteInstance(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// --- Instance Test ---

func (app *App) handleTestInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	inst, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	status, err := client.TestConnection()
	if err != nil {
		errMsg := err.Error()
		if isConnectionError(err) {
			errMsg = inst.Name + " is not reachable — check that the instance is running and the URL is correct"
		}
		writeJSON(w, map[string]any{
			"connected": false,
			"error":     errMsg,
		})
		return
	}

	writeJSON(w, map[string]any{
		"connected": true,
		"appName":   status.AppName,
		"version":   status.Version,
	})
}

// isBlockedHost returns true if the hostname resolves to a non-routable or metadata address.
func isBlockedHost(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := u.Hostname()
	if host == "" {
		return true
	}
	// Resolve hostname to IPs
	ips, err := net.LookupHost(host)
	if err != nil {
		return false // allow — DNS failure will surface as connection error
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
		// Block cloud metadata (169.254.169.254)
		if ip.Equal(net.ParseIP("169.254.169.254")) {
			return true
		}
	}
	return false
}

// handleTestConnection tests connectivity without requiring a saved instance.
func (app *App) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.URL == "" || req.APIKey == "" {
		writeError(w, 400, "url and apiKey are required")
		return
	}
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		req.URL = "http://" + req.URL
	}
	if isBlockedHost(req.URL) {
		writeError(w, 400, "URL resolves to a blocked address")
		return
	}

	client := NewArrClient(req.URL, req.APIKey, app.httpClient)
	status, err := client.TestConnection()
	if err != nil {
		errMsg := err.Error()
		if isConnectionError(err) {
			errMsg = "Instance is not reachable — check that the URL is correct and the instance is running"
		}
		writeJSON(w, map[string]any{
			"connected": false,
			"error":     errMsg,
		})
		return
	}

	writeJSON(w, map[string]any{
		"connected": true,
		"appName":   status.AppName,
		"version":   status.Version,
	})
}

// --- Instance Data (profiles, CFs from Arr) ---

func (app *App) handleInstanceProfiles(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to connect to instance")
		return
	}
	writeJSON(w, profiles)
}

// handleQualityDefinitions returns quality names from an Arr instance for the quality builder.
func (app *App) handleQualityDefinitions(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}
	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		writeError(w, 502, "Failed to fetch quality definitions: "+err.Error())
		return
	}
	// Return just quality names in order
	type QDef struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	result := make([]QDef, 0, len(defs))
	for _, d := range defs {
		result = append(result, QDef{ID: d.Quality.ID, Name: d.Quality.Name})
	}
	writeJSON(w, result)
}

// handleRenameProfile renames a quality profile in an Arr instance and updates local sync history.
func (app *App) handleRenameProfile(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}
	profileID, err := strconv.Atoi(r.PathValue("profileId"))
	if err != nil || profileID <= 0 {
		writeError(w, 400, "Invalid profile ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, 400, "Name is required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, 400, "Name is required")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to list profiles: "+err.Error())
		return
	}
	var target *ArrQualityProfile
	for i := range profiles {
		if profiles[i].ID == profileID {
			target = &profiles[i]
			break
		}
	}
	if target == nil {
		writeError(w, 404, "Profile not found in Arr")
		return
	}

	// Check for duplicate name
	for _, p := range profiles {
		if p.ID != profileID && strings.EqualFold(p.Name, req.Name) {
			writeError(w, 409, fmt.Sprintf("Profile %q already exists (ID %d)", p.Name, p.ID))
			return
		}
	}

	oldName := target.Name
	target.Name = req.Name
	if err := client.UpdateProfile(target); err != nil {
		writeError(w, 502, "Failed to rename profile: "+err.Error())
		return
	}

	// Update local sync history and auto-sync rules
	app.config.Update(func(cfg *Config) {
		for i := range cfg.SyncHistory {
			if cfg.SyncHistory[i].InstanceID == inst.ID && cfg.SyncHistory[i].ArrProfileID == profileID {
				cfg.SyncHistory[i].ArrProfileName = req.Name
			}
		}
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].InstanceID == inst.ID && cfg.AutoSync.Rules[i].ArrProfileID == profileID {
				// Rule doesn't store arrProfileName, but keep for future
			}
		}
	})

	log.Printf("Renamed profile %d on %s: %q → %q", profileID, inst.Name, oldName, req.Name)
	writeJSON(w, map[string]any{"ok": true, "oldName": oldName, "newName": req.Name})
}

// handleInstanceProfileExport fetches a profile from an Arr instance,
// converts it to an ImportedProfile (same as the import system), and saves it.
func (app *App) handleInstanceProfileExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profileIdStr := r.PathValue("profileId")
	profileId, err := strconv.Atoi(profileIdStr)
	if err != nil {
		writeError(w, 400, "Invalid profile ID")
		return
	}

	inst, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}

	var profile *ArrQualityProfile
	for i := range profiles {
		if profiles[i].ID == profileId {
			profile = &profiles[i]
			break
		}
	}
	if profile == nil {
		writeError(w, 404, "Profile not found in instance")
		return
	}

	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch custom formats: "+err.Error())
		return
	}

	// Build Arr CF ID → name map
	arrCFNames := make(map[int]string, len(arrCFs))
	for _, cf := range arrCFs {
		arrCFNames[cf.ID] = cf.Name
	}

	// Build CF name → id map from TRaSH data + the user's custom CFs. Custom CFs created
	// in Clonarr (ccf.ID is a "custom:<hex>" UUID, not a TRaSH hex) still need to resolve
	// here so imports surface them in the Builder's Custom group. TRaSH wins on name
	// collision — matches how the sync engine prefers TRaSH for known names.
	ad := app.trash.GetAppData(inst.Type)
	customCFs := app.customCFs.List(inst.Type)
	customCFByID := make(map[string]CustomCF, len(customCFs))
	nameToTrashID := make(map[string]string)
	if ad != nil {
		for trashID, cf := range ad.CustomFormats {
			nameToTrashID[cf.Name] = trashID
		}
	}
	for _, ccf := range customCFs {
		customCFByID[ccf.ID] = ccf
		if _, exists := nameToTrashID[ccf.Name]; !exists {
			nameToTrashID[ccf.Name] = ccf.ID
		}
	}

	// Consult Clonarr's sync history for this instance + profile. Arr lists ALL CFs on
	// every profile (score=0 by default), so a naive import can't distinguish "unused
	// Arr default" from "user's intentional score-0 extra". When this profile was previously
	// synced via Clonarr, SyncedCFs + ScoreOverrides tell us exactly which CFs are tracked
	// — we allow score=0 through for those. Mirrors the same enrichment handleCompareProfile
	// added in the Compare flow (commit 082963c). Arr CF IDs (not trash_ids) are used so
	// the skip check stays a cheap map lookup in the FormatItems loop.
	knownArrIDs := make(map[int]bool)
	if ad != nil || len(customCFByID) > 0 {
		arrByName := make(map[string]int, len(arrCFs))
		for i := range arrCFs {
			arrByName[strings.ToLower(arrCFs[i].Name)] = arrCFs[i].ID
		}
		resolveArrID := func(id string) {
			var name string
			if ad != nil {
				if tcf, ok := ad.CustomFormats[id]; ok {
					name = tcf.Name
				}
			}
			if name == "" {
				if ccf, ok := customCFByID[id]; ok {
					name = ccf.Name
				}
			}
			if name == "" {
				return
			}
			if arrID, ok := arrByName[strings.ToLower(name)]; ok {
				knownArrIDs[arrID] = true
			}
		}
		for _, sh := range app.config.GetSyncHistory(inst.ID) {
			if sh.ArrProfileID != profileId {
				continue
			}
			for _, id := range sh.SyncedCFs {
				resolveArrID(id)
			}
			for id := range sh.ScoreOverrides {
				resolveArrID(id)
			}
			break
		}
	}

	// Map profile formatItems: Arr CF ID → trash_id + score.
	// Non-zero scores always flow through. Zero scores flow through only when sync history
	// marks the CF as intentionally tracked for this profile — otherwise we'd flood the
	// Builder with every Arr default.
	formatItems := make(map[string]int)
	formatComments := make(map[string]string)
	unmapped := []string{}
	for _, fi := range profile.FormatItems {
		if fi.Score == 0 && !knownArrIDs[fi.Format] {
			continue
		}
		cfName := fi.Name
		if cfName == "" {
			cfName = arrCFNames[fi.Format]
		}
		if cfName == "" {
			continue
		}
		if trashID, ok := nameToTrashID[cfName]; ok {
			formatItems[trashID] = fi.Score
			formatComments[trashID] = cfName
		} else {
			unmapped = append(unmapped, cfName)
		}
	}

	// Resolve formatGroups from TRaSH CF group data (same logic as import)
	var formatGroups map[string]string
	if ad != nil {
		formatGroups = make(map[string]string)
		for _, g := range ad.CFGroups {
			for _, cf := range g.CustomFormats {
				if _, ok := formatItems[cf.TrashID]; ok {
					formatGroups[cf.TrashID] = g.Name
				}
			}
		}
	}

	// Map quality items (Radarr API returns lowest priority first, reverse for TRaSH format)
	var qualities []QualityItem
	for _, qi := range profile.Items {
		q := QualityItem{Allowed: qi.Allowed}
		if qi.Quality != nil {
			q.Name = qi.Quality.Name
		} else if qi.Name != "" {
			q.Name = qi.Name
			for _, sub := range qi.Items {
				if sub.Quality != nil {
					q.Items = append(q.Items, sub.Quality.Name)
				}
			}
		}
		qualities = append(qualities, q)
	}
	// Reverse to match TRaSH format (highest priority / allowed first)
	for i, j := 0, len(qualities)-1; i < j; i, j = i+1, j-1 {
		qualities[i], qualities[j] = qualities[j], qualities[i]
	}

	// Resolve cutoff name — check both quality groups (by group ID) and individual qualities
	cutoffName := ""
	for _, qi := range profile.Items {
		if qi.Quality != nil && qi.Quality.ID == profile.Cutoff {
			cutoffName = qi.Quality.Name
			break
		}
		// Quality group: ID is the group ID, Name is the group name (e.g. "Bluray|WEB-2160p")
		if qi.Quality == nil && qi.ID == profile.Cutoff && qi.Name != "" {
			cutoffName = qi.Name
			break
		}
		for _, sub := range qi.Items {
			if sub.Quality != nil && sub.Quality.ID == profile.Cutoff {
				cutoffName = qi.Name // Use group name as cutoff
				break
			}
		}
		if cutoffName != "" {
			break
		}
	}

	// Language
	lang := "Original"
	if profile.Language != nil && profile.Language.Name != "" {
		lang = profile.Language.Name
	}

	// Create ImportedProfile — same struct as the import system
	imported := ImportedProfile{
		ID:                    generateID(),
		Name:                  profile.Name,
		AppType:               inst.Type,
		Source:                 "instance",
		UpgradeAllowed:        profile.UpgradeAllowed,
		Cutoff:                cutoffName,
		CutoffScore:           profile.CutoffFormatScore,
		MinFormatScore:        profile.MinFormatScore,
		MinUpgradeFormatScore: profile.MinUpgradeFormatScore,
		Language:              lang,
		FormatItems:           formatItems,
		FormatComments:        formatComments,
		FormatGroups:          formatGroups,
		Qualities:             qualities,
		ImportedAt:            time.Now().UTC().Format(time.RFC3339),
	}

	// Return data without saving — user decides to save via Create Profile button
	writeJSON(w, map[string]any{
		"profile":  imported,
		"unmapped": unmapped,
	})
}

func (app *App) handleInstanceLanguages(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	languages, err := client.ListLanguages()
	if err != nil {
		writeError(w, 502, "Failed to fetch languages")
		return
	}
	writeJSON(w, languages)
}

// handleInstanceBackup creates a backup of selected profiles + CFs, or CFs only.
// POST body: { "profileIds": [1,2], "extraCfIds": [5,10], "cfIds": [3,4] }
// Mode 1 (profiles): profileIds selects profiles; CFs with score≠0 auto-included; extraCfIds adds score=0 CFs.
// Mode 2 (CFs only): cfIds selects specific CFs without profiles.
func (app *App) handleInstanceBackup(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	req, ok := decodeJSON[struct {
		ProfileIDs []int `json:"profileIds"`
		ExtraCFIDs []int `json:"extraCfIds"`
		CFIDs      []int `json:"cfIds"`
	}](w, r, 1<<20)
	if !ok {
		return
	}
	if len(req.ProfileIDs) == 0 && len(req.CFIDs) == 0 {
		writeError(w, 400, "No profiles or custom formats selected")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	allCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch custom formats: "+err.Error())
		return
	}

	var selectedProfiles []ArrQualityProfile
	var selectedCFs []ArrCF

	if len(req.CFIDs) > 0 && len(req.ProfileIDs) == 0 {
		// CF-only backup mode
		cfIDSet := make(map[int]bool, len(req.CFIDs))
		for _, id := range req.CFIDs {
			cfIDSet[id] = true
		}
		for _, cf := range allCFs {
			if cfIDSet[cf.ID] {
				selectedCFs = append(selectedCFs, cf)
			}
		}
	} else {
		// Profile backup mode
		profiles, err := client.ListProfiles()
		if err != nil {
			writeError(w, 502, "Failed to fetch profiles: "+err.Error())
			return
		}

		selectedIDs := make(map[int]bool, len(req.ProfileIDs))
		for _, pid := range req.ProfileIDs {
			selectedIDs[pid] = true
		}
		for _, p := range profiles {
			if selectedIDs[p.ID] {
				selectedProfiles = append(selectedProfiles, p)
			}
		}

		// Collect CF IDs: auto-include any CF with score ≠ 0 in selected profiles
		includeCFIDs := make(map[int]bool)
		for _, p := range selectedProfiles {
			for _, fi := range p.FormatItems {
				if fi.Score != 0 {
					includeCFIDs[fi.Format] = true
				}
			}
		}
		for _, cfid := range req.ExtraCFIDs {
			includeCFIDs[cfid] = true
		}
		for _, cf := range allCFs {
			if includeCFIDs[cf.ID] {
				selectedCFs = append(selectedCFs, cf)
			}
		}
	}

	backup := map[string]any{
		"_clonarrBackup": true,
		"version":        2,
		"instanceName":   inst.Name,
		"instanceType":   inst.Type,
		"exportedAt":     time.Now().UTC().Format(time.RFC3339),
		"profiles":       selectedProfiles,
		"customFormats":  selectedCFs,
	}
	writeJSON(w, backup)
}

// handleInstanceRestore applies a backup to an instance.
// POST body: the backup JSON with profiles + customFormats arrays.
// dryRun query param: if "true", returns a preview without applying.
func (app *App) handleInstanceRestore(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	dryRun := r.URL.Query().Get("dryRun") == "true"

	backup, ok := decodeJSON[struct {
		Profiles      []ArrQualityProfile `json:"profiles"`
		CustomFormats []ArrCF             `json:"customFormats"`
	}](w, r, 10<<20)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	// Fetch current state from instance
	existingCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch custom formats: "+err.Error())
		return
	}
	existingProfiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}

	// Build name→ID maps for matching (case-insensitive)
	existingCFByName := make(map[string]*ArrCF, len(existingCFs))
	for i := range existingCFs {
		existingCFByName[strings.ToLower(existingCFs[i].Name)] = &existingCFs[i]
	}
	existingProfileByName := make(map[string]*ArrQualityProfile, len(existingProfiles))
	for i := range existingProfiles {
		existingProfileByName[strings.ToLower(existingProfiles[i].Name)] = &existingProfiles[i]
	}

	type RestoreAction struct {
		Type   string `json:"type"`   // "cf" or "profile"
		Action string `json:"action"` // "create" or "update"
		Name   string `json:"name"`
		Error  string `json:"error,omitempty"`
	}
	var actions []RestoreAction

	// Old CF ID → new CF ID mapping (for remapping profile formatItems)
	cfIDMap := make(map[int]int)

	// Step 1: Restore Custom Formats
	for _, cf := range backup.CustomFormats {
		oldID := cf.ID
		existing := existingCFByName[strings.ToLower(cf.Name)]
		action := "create"
		if existing != nil {
			action = "update"
		}

		if dryRun {
			actions = append(actions, RestoreAction{Type: "cf", Action: action, Name: cf.Name})
			if existing != nil {
				cfIDMap[oldID] = existing.ID
			} else {
				cfIDMap[oldID] = -1 // placeholder for new CFs
			}
			continue
		}

		cf.ID = 0 // Clear for create
		if existing != nil {
			// Update existing CF
			result, err := client.UpdateCustomFormat(existing.ID, &cf)
			if err != nil {
				actions = append(actions, RestoreAction{Type: "cf", Action: action, Name: cf.Name, Error: err.Error()})
				continue
			}
			cfIDMap[oldID] = result.ID
		} else {
			// Create new CF
			result, err := client.CreateCustomFormat(&cf)
			if err != nil {
				actions = append(actions, RestoreAction{Type: "cf", Action: action, Name: cf.Name, Error: err.Error()})
				continue
			}
			cfIDMap[oldID] = result.ID
		}
		actions = append(actions, RestoreAction{Type: "cf", Action: action, Name: cf.Name})
	}

	// Step 2: Restore Profiles (remap CF IDs in formatItems)
	for _, profile := range backup.Profiles {
		existing := existingProfileByName[strings.ToLower(profile.Name)]
		action := "create"
		if existing != nil {
			action = "update"
		}

		// Remap formatItems CF IDs from backup IDs to current instance IDs
		if !dryRun {
			// Build name→backupCF map for fallback matching
			backupCFByID := make(map[int]string)
			for _, cf := range backup.CustomFormats {
				backupCFByID[cf.ID] = cf.Name
			}
			// Remap using cfIDMap, or match by name against current instance
			for i := range profile.FormatItems {
				fi := &profile.FormatItems[i]
				if newID, ok := cfIDMap[fi.Format]; ok {
					fi.Format = newID
				} else if cfName := backupCFByID[fi.Format]; cfName != "" {
					if ecf := existingCFByName[strings.ToLower(cfName)]; ecf != nil {
						fi.Format = ecf.ID
					}
				}
			}
		}

		if dryRun {
			actions = append(actions, RestoreAction{Type: "profile", Action: action, Name: profile.Name})
			continue
		}

		if existing != nil {
			// Update: merge formatItems scores into existing profile
			profile.ID = existing.ID
			// Keep existing quality items structure (don't overwrite allowed qualities)
			profile.Items = existing.Items
			if err := client.UpdateProfile(&profile); err != nil {
				actions = append(actions, RestoreAction{Type: "profile", Action: action, Name: profile.Name, Error: err.Error()})
				continue
			}
		} else {
			// Create new profile
			profile.ID = 0
			if _, err := client.CreateProfile(&profile); err != nil {
				actions = append(actions, RestoreAction{Type: "profile", Action: action, Name: profile.Name, Error: err.Error()})
				continue
			}
		}
		actions = append(actions, RestoreAction{Type: "profile", Action: action, Name: profile.Name})
	}

	writeJSON(w, map[string]any{"actions": actions, "dryRun": dryRun})
}

func (app *App) handleInstanceCFs(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	cfs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to connect to instance")
		return
	}
	writeJSON(w, cfs)
}

func (app *App) handleInstanceQualitySizes(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		writeError(w, 502, "Failed to fetch quality sizes: "+err.Error())
		return
	}
	writeJSON(w, defs)
}

func (app *App) handleGetInstanceNaming(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	naming, err := client.GetNaming()
	if err != nil {
		writeError(w, 502, "Failed to fetch naming config: "+err.Error())
		return
	}
	writeJSON(w, naming)
}

func (app *App) handleApplyNaming(w http.ResponseWriter, r *http.Request) {
	inst, ok := app.requireInstance(w, r)
	if !ok {
		return
	}

	req, ok := decodeJSON[struct {
		Preset  string `json:"preset"`           // convenience: e.g. "plex-tmdb" — resolves from TRaSH data
		Folder  string `json:"folder"`
		File    string `json:"file"`
		Season  string `json:"season,omitempty"`
		Series  string `json:"series,omitempty"`
		Daily   string `json:"daily,omitempty"`
		Anime   string `json:"anime,omitempty"`
		Special string `json:"special,omitempty"`
	}](w, r, 1<<20)
	if !ok {
		return
	}

	// If preset is given, resolve naming formats from TRaSH data
	if req.Preset != "" {
		ad := app.trash.GetAppData(inst.Type)
		if ad == nil {
			writeError(w, 400, "No TRaSH data available for "+inst.Type)
			return
		}
		if inst.Type == "radarr" {
			if v, ok := ad.Naming.Folder[req.Preset]; ok {
				req.Folder = v
			} else if req.Folder == "" {
				req.Folder = ad.Naming.Folder["default"]
			}
			if v, ok := ad.Naming.File[req.Preset]; ok {
				req.File = v
			}
		} else {
			// Sonarr
			if v, ok := ad.Naming.Series[req.Preset]; ok {
				req.Series = v
			} else if req.Series == "" {
				req.Series = ad.Naming.Series["default"]
			}
			if v, ok := ad.Naming.Season["default"]; ok && req.Season == "" {
				req.Season = v
			}
			if ep := ad.Naming.Episodes["standard"]; ep != nil {
				if v, ok := ep[req.Preset]; ok {
					req.File = v
				} else if req.File == "" {
					req.File = ep["default"]
				}
			}
			if ep := ad.Naming.Episodes["daily"]; ep != nil {
				if v, ok := ep[req.Preset]; ok {
					req.Daily = v
				} else if req.Daily == "" {
					req.Daily = ep["default"]
				}
			}
			if ep := ad.Naming.Episodes["anime"]; ep != nil {
				if v, ok := ep[req.Preset]; ok {
					req.Anime = v
				} else if req.Anime == "" {
					req.Anime = ep["default"]
				}
			}
		}
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	// Fetch current config first (we need id and other fields)
	current, err := client.GetNaming()
	if err != nil {
		writeError(w, 502, "Failed to fetch current naming: "+err.Error())
		return
	}

	// Apply the requested changes based on instance type
	if inst.Type == "radarr" {
		current["renameMovies"] = true
		current["replaceIllegalCharacters"] = true
		if req.File != "" {
			current["standardMovieFormat"] = req.File
		}
		if req.Folder != "" {
			current["movieFolderFormat"] = req.Folder
		}
	} else {
		// Sonarr
		current["renameEpisodes"] = true
		current["replaceIllegalCharacters"] = true
		if req.File != "" {
			current["standardEpisodeFormat"] = req.File
		}
		if req.Season != "" {
			current["seasonFolderFormat"] = req.Season
		}
		if req.Series != "" {
			current["seriesFolderFormat"] = req.Series
		}
		if req.Daily != "" {
			current["dailyEpisodeFormat"] = req.Daily
		}
		if req.Anime != "" {
			current["animeEpisodeFormat"] = req.Anime
		}
		if req.Special != "" {
			current["specialsFolderFormat"] = req.Special
		}
	}

	result, err := client.UpdateNaming(current)
	if err != nil {
		writeError(w, 502, "Failed to apply naming: "+err.Error())
		return
	}
	writeJSON(w, result)
}

func (app *App) handleSyncQualitySizes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	req, ok2 := decodeJSON[struct {
		Definitions []ArrQualityDefinition `json:"definitions"`
		Type        string                 `json:"type"` // convenience: "movie", "anime", etc — builds definitions from TRaSH data
	}](w, r, 1<<20)
	if !ok2 {
		return
	}

	// If type is provided, build definitions by comparing TRaSH data with instance
	if req.Type != "" && len(req.Definitions) == 0 {
		defs, err := app.buildQualitySizeDefs(inst, req.Type)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		if len(defs) == 0 {
			writeJSON(w, map[string]any{"status": "synced", "count": 0, "message": "all values match"})
			return
		}
		req.Definitions = defs
	}

	if len(req.Definitions) == 0 {
		writeError(w, 400, "No definitions to sync — provide 'definitions' array or 'type' string")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	if err := client.UpdateQualityDefinitions(req.Definitions); err != nil {
		writeError(w, 502, "Sync failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "synced", "count": len(req.Definitions)})
}

// buildQualitySizeDefs compares TRaSH quality sizes with instance and returns
// definitions that need updating (same logic as autoSyncQualitySizes).
func (app *App) buildQualitySizeDefs(inst Instance, qsType string) ([]ArrQualityDefinition, error) {
	ad := app.trash.GetAppData(inst.Type)
	if ad == nil {
		return nil, fmt.Errorf("no TRaSH data for %s", inst.Type)
	}
	var trashQS *TrashQualitySize
	for _, qs := range ad.QualitySizes {
		if qs.Type == qsType {
			trashQS = qs
			break
		}
	}
	if trashQS == nil {
		return nil, fmt.Errorf("no TRaSH quality sizes for type %q", qsType)
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch definitions: %w", err)
	}

	defByName := make(map[string]*ArrQualityDefinition)
	for i := range defs {
		defByName[defs[i].Quality.Name] = &defs[i]
		defByName[defs[i].Title] = &defs[i]
	}

	cfg := app.config.Get()
	overrides := cfg.QualitySizeOverrides[inst.ID]

	var updated []ArrQualityDefinition
	for _, qs := range trashQS.Qualities {
		if overrides != nil {
			if _, isCustom := overrides[qs.Quality]; isCustom {
				continue
			}
		}
		def := defByName[qs.Quality]
		if def == nil {
			continue
		}
		if math.Abs(floatVal(def.MinSize)-qs.Min) >= 0.05 ||
			math.Abs(floatVal(def.PreferredSize)-qs.Preferred) >= 0.05 ||
			math.Abs(floatVal(def.MaxSize)-qs.Max) >= 0.05 {
			updated = append(updated, ArrQualityDefinition{
				ID:            def.ID,
				Quality:       def.Quality,
				Title:         def.Title,
				MinSize:       floatPtr(qs.Min),
				PreferredSize: floatPtr(qs.Preferred),
				MaxSize:       floatPtr(qs.Max),
			})
		}
	}
	return updated, nil
}

func (app *App) handleGetQSOverrides(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := app.config.Get()
	overrides := map[string]QSOverride{}
	if cfg.QualitySizeOverrides != nil {
		if inst, ok := cfg.QualitySizeOverrides[id]; ok {
			overrides = inst
		}
	}
	writeJSON(w, overrides)
}

func (app *App) handleSaveQSOverrides(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var overrides map[string]QSOverride
	if err := json.NewDecoder(r.Body).Decode(&overrides); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	err := app.config.Update(func(cfg *Config) {
		if cfg.QualitySizeOverrides == nil {
			cfg.QualitySizeOverrides = make(map[string]map[string]QSOverride)
		}
		if len(overrides) == 0 {
			delete(cfg.QualitySizeOverrides, id)
		} else {
			cfg.QualitySizeOverrides[id] = overrides
		}
	})
	if err != nil {
		writeError(w, 500, "Failed to save overrides")
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

func (app *App) handleGetQSAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := app.config.Get()
	as := QSAutoSync{}
	if cfg.QualitySizeAutoSync != nil {
		if v, ok := cfg.QualitySizeAutoSync[id]; ok {
			as = v
		}
	}
	writeJSON(w, as)
}

func (app *App) handleSaveQSAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := app.config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var as QSAutoSync
	if err := json.NewDecoder(r.Body).Decode(&as); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	err := app.config.Update(func(cfg *Config) {
		if cfg.QualitySizeAutoSync == nil {
			cfg.QualitySizeAutoSync = make(map[string]QSAutoSync)
		}
		if !as.Enabled {
			delete(cfg.QualitySizeAutoSync, id)
		} else {
			cfg.QualitySizeAutoSync[id] = as
		}
	})
	if err != nil {
		writeError(w, 500, "Failed to save auto-sync settings")
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

// autoSyncQualitySizes runs after a TRaSH pull. For each instance with auto-sync
// enabled, it syncs quality sizes in Sync mode (skipping Custom overrides).
func (app *App) autoSyncQualitySizes() {
	cfg := app.config.Get()
	if len(cfg.QualitySizeAutoSync) == 0 {
		return
	}

	for instID, as := range cfg.QualitySizeAutoSync {
		if !as.Enabled || as.Type == "" {
			continue
		}
		inst, ok := app.config.GetInstance(instID)
		if !ok {
			continue
		}

		// Get TRaSH quality sizes for this type
		ad := app.trash.GetAppData(inst.Type)
		if ad == nil {
			continue
		}
		var trashQS *TrashQualitySize
		for _, qs := range ad.QualitySizes {
			if qs.Type == as.Type {
				trashQS = qs
				break
			}
		}
		if trashQS == nil {
			continue
		}

		// Get current instance quality definitions
		client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
		defs, err := client.ListQualityDefinitions()
		if err != nil {
			log.Printf("Auto-sync QS [%s]: failed to fetch definitions: %v", inst.Name, err)
			app.debugLog.Logf(LogAutoSync, "QS [%s]: failed to fetch definitions: %v", inst.Name, err)
			continue
		}

		// Build lookup
		defByName := make(map[string]*ArrQualityDefinition)
		for i := range defs {
			defByName[defs[i].Quality.Name] = &defs[i]
			defByName[defs[i].Title] = &defs[i]
		}

		// Get overrides (Custom qualities to skip)
		overrides := cfg.QualitySizeOverrides[instID]

		var updated []ArrQualityDefinition
		for _, qs := range trashQS.Qualities {
			// Skip Custom-mode qualities
			if overrides != nil {
				if _, isCustom := overrides[qs.Quality]; isCustom {
					continue
				}
			}

			def := defByName[qs.Quality]
			if def == nil {
				continue
			}

			if math.Abs(floatVal(def.MinSize)-qs.Min) >= 0.05 ||
				math.Abs(floatVal(def.PreferredSize)-qs.Preferred) >= 0.05 ||
				math.Abs(floatVal(def.MaxSize)-qs.Max) >= 0.05 {
				updated = append(updated, ArrQualityDefinition{
					ID:            def.ID,
					Quality:       def.Quality,
					Title:         def.Title,
					MinSize:       floatPtr(qs.Min),
					PreferredSize: floatPtr(qs.Preferred),
					MaxSize:       floatPtr(qs.Max),
				})
			}
		}

		if len(updated) == 0 {
			log.Printf("Auto-sync QS [%s/%s]: all values match", inst.Name, as.Type)
			continue
		}

		if err := client.UpdateQualityDefinitions(updated); err != nil {
			log.Printf("Auto-sync QS [%s/%s]: sync failed: %v", inst.Name, as.Type, err)
			app.debugLog.Logf(LogError, "QS [%s/%s]: sync failed: %v", inst.Name, as.Type, err)
		} else {
			log.Printf("Auto-sync QS [%s/%s]: synced %d qualities", inst.Name, as.Type, len(updated))
			app.debugLog.Logf(LogAutoSync, "QS [%s/%s]: synced %d qualities", inst.Name, as.Type, len(updated))
		}
	}
}

// --- TRaSH ---

func (app *App) handleTrashStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, app.trash.Status())
}

func (app *App) handleTrashPull(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	safeGo("manual-trash-pull", func() {
		prevCommit := app.trash.CurrentCommit()
		if err := app.trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("TRaSH pull failed: %v", err)
			app.debugLog.Logf(LogError, "TRaSH pull failed: %v", err)
			app.trash.SetPullError(err.Error())
		} else {
			newCommit := app.trash.CurrentCommit()
			if prevCommit != "" && newCommit != prevCommit {
				app.notifyRepoUpdate(prevCommit, newCommit)
			}
			app.debugLog.Logf(LogAutoSync, "TRaSH pull completed — running auto-sync")
			app.autoSyncQualitySizes()
			app.autoSyncAfterPull()
		}
	})
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "pulling"})
}

func (app *App) handleTrashCFs(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	cfs := make([]*TrashCF, 0, len(ad.CustomFormats))
	for _, cf := range ad.CustomFormats {
		cfs = append(cfs, cf)
	}
	writeJSON(w, cfs)
}

// handleTrashScoreContexts returns the distinct trash_scores context keys
// actually used in TRaSH-Guides CFs for the given app. Keeps the Custom Format
// editor's context dropdown in sync with upstream without hardcoding.
func (app *App) handleTrashScoreContexts(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []string{"default"})
		return
	}

	seen := map[string]struct{}{"default": {}}
	for _, cf := range ad.CustomFormats {
		for k := range cf.TrashScores {
			seen[k] = struct{}{}
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	// Stable ordering: "default" first, then alphabetical.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "default" {
			return true
		}
		if keys[j] == "default" {
			return false
		}
		return keys[i] < keys[j]
	})
	writeJSON(w, keys)
}

func (app *App) handleTrashCFGroups(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	groups := ad.CFGroups
	if groups == nil {
		groups = []*TrashCFGroup{}
	}
	writeJSON(w, groups)
}

func (app *App) handleTrashConflicts(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := app.trash.GetAppData(appType)
	if ad == nil || ad.Conflicts == nil {
		writeJSON(w, ConflictsData{CustomFormats: [][]ConflictEntry{}})
		return
	}
	writeJSON(w, ad.Conflicts)
}

func (app *App) handleTrashProfiles(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	type ProfileListItem struct {
		TrashID          string `json:"trashId"`
		Name             string `json:"name"`
		TrashScoreSet    string `json:"trashScoreSet,omitempty"`
		TrashDescription string `json:"trashDescription,omitempty"`
		TrashURL         string `json:"trashUrl,omitempty"`
		Group            int    `json:"group"`
		GroupName        string `json:"groupName"`
		CFCount          int    `json:"cfCount"`
	}

	groupNames := make(map[string]string) // trash_id → group name
	for _, pg := range ad.ProfileGroups {
		for _, tid := range pg.Profiles {
			groupNames[tid] = pg.Name
		}
	}

	var items []ProfileListItem
	for _, p := range ad.Profiles {
		gn := groupNames[p.TrashID]
		if gn == "" {
			gn = "Other"
		}
		items = append(items, ProfileListItem{
			TrashID:          p.TrashID,
			Name:             p.Name,
			TrashScoreSet:    p.TrashScoreSet,
			TrashDescription: p.TrashDescription,
			TrashURL:         p.TrashURL,
			Group:            p.Group,
			GroupName:        gn,
			CFCount:          len(p.FormatItems),
		})
	}
	writeJSON(w, items)
}

// --- Arr Comparison Types ---

// ArrCFState describes the state of a single CF in an Arr instance.
type ArrCFState struct {
	Exists       bool   `json:"exists"`
	ArrID        int    `json:"arrId,omitempty"`
	ArrName      string `json:"arrName,omitempty"`
	TrashName    string `json:"trashName"`
	Description  string `json:"description,omitempty"`
	CurrentScore int    `json:"currentScore"`
	DesiredScore int    `json:"desiredScore"`
	ScoreMatch   bool   `json:"scoreMatch"`
}

// ProfileComparison compares a specific Arr profile against a TRaSH profile.
type ProfileComparison struct {
	ArrProfileID       int                      `json:"arrProfileId"`
	ArrProfileName     string                   `json:"arrProfileName"`
	TrashProfileID     string                   `json:"trashProfileId"`
	TrashProfileName   string                   `json:"trashProfileName"`
	CFStates           map[string]ArrCFState    `json:"cfStates"`          // keyed by trash_id
	ExtraCFs           []ArrProfileFormatItem   `json:"extraCFs"`          // CFs in Arr profile not in TRaSH profile
	// NEW: TRaSH group-based comparison
	FormatItems        []CompareFormatItem      `json:"formatItems"`
	Groups             []CompareGroup           `json:"groups"`
	// Profile settings and quality comparison
	SettingsDiffs      []SettingDiff            `json:"settingsDiffs,omitempty"`
	QualityDiffs       []QualityItemDiff        `json:"qualityDiffs,omitempty"`
	// LEGACY (keep for now)
	OptionalCategories []CompareCategory        `json:"optionalCategories"` // optional CF groups categorized
	Summary            ComparisonSummary        `json:"summary"`
	Error              string                   `json:"error,omitempty"`
}

// ComparisonSummary holds counts for the comparison.
type ComparisonSummary struct {
	Missing       int `json:"missing"`       // CFs in TRaSH but not in Arr instance
	WrongScore    int `json:"wrongScore"`    // CFs exist but score differs
	Matching      int `json:"matching"`      // CFs exist with correct score
	Extra         int `json:"extra"`         // CFs in Arr profile not in TRaSH profile
	SettingsDiffs int `json:"settingsDiffs"` // profile settings that differ
	QualityDiffs  int `json:"qualityDiffs"`  // quality items that differ
}

// SettingDiff describes a profile setting that differs between Arr and TRaSH.
type SettingDiff struct {
	Name    string `json:"name"`
	Current string `json:"current"`
	Desired string `json:"desired"`
	Match   bool   `json:"match"`
}

// QualityItemDiff describes a quality item that differs between Arr and TRaSH.
type QualityItemDiff struct {
	Name           string `json:"name"`
	CurrentAllowed bool   `json:"currentAllowed"`
	DesiredAllowed bool   `json:"desiredAllowed"`
	Match          bool   `json:"match"`
}

// CompareFormatItem is a formatItem CF with comparison status.
// ScoreOverride is set when the sync rule has a per-CF score override; UI colors the Current
// cell differently and shows a tooltip so users know the diff is intentional.
type CompareFormatItem struct {
	TrashID       string `json:"trashId"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"` // TRaSH description for hover tooltip
	Exists        bool   `json:"exists"`
	ArrID         int    `json:"arrId,omitempty"`
	CurrentScore  int    `json:"currentScore"`
	DesiredScore  int    `json:"desiredScore"`
	ScoreMatch    bool   `json:"scoreMatch"`
	ScoreOverride *int   `json:"scoreOverride,omitempty"` // nil if no override from sync rule
}

// CompareGroup is a TRaSH CF group with comparison status per CF.
type CompareGroup struct {
	Name             string      `json:"name"`
	ShortName        string      `json:"shortName"`
	Category         string      `json:"category"`
	TrashDescription string      `json:"trashDescription"`
	DefaultEnabled   bool        `json:"defaultEnabled"`
	Exclusive        bool        `json:"exclusive"`
	CFs              []CompareCF `json:"cfs"`
	// Summary counts
	Total     int `json:"total"`
	Present   int `json:"present"`
	Matching  int `json:"matching"`
	WrongScore int `json:"wrongScore"`
	Missing   int `json:"missing"`
}

// CompareCF is a single CF within a group with comparison status.
type CompareCF struct {
	TrashID       string `json:"trashId"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Required      bool   `json:"required"`
	Default       bool   `json:"default"`
	Exists        bool   `json:"exists"`
	InUse         bool   `json:"inUse"` // non-zero score in Arr (user actively chose this)
	ArrID         int    `json:"arrId,omitempty"`
	CurrentScore  int    `json:"currentScore"`
	DesiredScore  int    `json:"desiredScore"`
	ScoreMatch    bool   `json:"scoreMatch"`
	ScoreOverride *int   `json:"scoreOverride,omitempty"` // nil if no override from sync rule
}

// CompareCategory groups optional CF groups under a category (Streaming Services, Optional, etc.)
type CompareCategory struct {
	Category string                `json:"category"`
	Groups   []OptionalGroupState `json:"groups"`
}

// OptionalGroupState describes the state of an optional CF group in the Arr instance.
type OptionalGroupState struct {
	Name        string              `json:"name"`
	ShortName   string              `json:"shortName"`
	Description string              `json:"description,omitempty"`
	Exclusive   bool                `json:"exclusive,omitempty"` // pick one (Golden Rule)
	CFs         []OptionalCFState   `json:"cfs"`
	Present     int                 `json:"present"`     // how many CFs exist in Arr
	Total       int                 `json:"total"`       // total CFs in group
	WrongScore  int                 `json:"wrongScore"`  // present but wrong score
}

// OptionalCFState describes one CF within an optional/required group.
type OptionalCFState struct {
	TrashID      string `json:"trashId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Exists       bool   `json:"exists"`
	ArrID        int    `json:"arrId,omitempty"`
	Default      bool   `json:"default"`      // TRaSH recommended
	CurrentScore int    `json:"currentScore"`
	DesiredScore int    `json:"desiredScore"`
	ScoreMatch   bool   `json:"scoreMatch"`
}

// buildProfileComparison compares a specific Arr profile against a TRaSH profile.
// Uses TRaSH CF groups consistently: ungrouped formatItems in FormatItems, grouped CFs in Groups.
// scoreOverrides (trash_id → score) carry the user's desired-score-per-CF from a prior Clonarr
// sync. When a CF has an override, the Compare uses that value as the desired score instead of
// TRaSH's default — so CFs deliberately set to 0 (or any custom score) don't show as "wrong".
func buildProfileComparison(inst Instance, ad *AppData, trashProfileID string, arrProfileID int, syncedCFs []string, scoreOverrides map[string]int, httpClient *http.Client) *ProfileComparison {
	comp := &ProfileComparison{
		ArrProfileID:   arrProfileID,
		TrashProfileID: trashProfileID,
		CFStates:       make(map[string]ArrCFState),
		FormatItems:    []CompareFormatItem{},
		Groups:         []CompareGroup{},
	}

	// Find TRaSH profile
	var trashProfile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == trashProfileID {
			trashProfile = p
			break
		}
	}
	if trashProfile == nil {
		comp.Error = "TRaSH profile not found"
		return comp
	}
	comp.TrashProfileName = trashProfile.Name

	client := NewArrClient(inst.URL, inst.APIKey, httpClient)

	// Fetch existing CFs from instance
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		comp.Error = "Failed to fetch CFs: " + err.Error()
		return comp
	}

	// Build name → ArrCF map (case-insensitive keys for matching)
	arrByName := make(map[string]*ArrCF)
	for i := range arrCFs {
		arrByName[strings.ToLower(arrCFs[i].Name)] = &arrCFs[i]
	}

	// Fetch the specific Arr profile to get scores
	arrProfiles, err := client.ListProfiles()
	if err != nil {
		comp.Error = "Failed to fetch profiles: " + err.Error()
		return comp
	}

	var arrProfile *ArrQualityProfile
	for i := range arrProfiles {
		if arrProfiles[i].ID == arrProfileID {
			arrProfile = &arrProfiles[i]
			break
		}
	}
	if arrProfile == nil {
		comp.Error = "Arr profile not found"
		return comp
	}
	comp.ArrProfileName = arrProfile.Name

	// Build score map from this specific profile
	arrScores := make(map[int]int)
	for _, fi := range arrProfile.FormatItems {
		arrScores[fi.Format] = fi.Score
	}

	// Build set of CFs that were synced via Clonarr (from sync history)
	// This tells us which score-0 CFs were deliberately synced vs just existing in Arr
	syncedCFSet := make(map[string]bool)
	for _, tid := range syncedCFs {
		syncedCFSet[tid] = true
	}

	// Get TRaSH group data via ProfileDetailData
	detailData := ProfileDetailData(ad, trashProfileID)

	// Track all Arr CF IDs matched by TRaSH CFs (for extra CF detection)
	trackedArrIDs := make(map[int]bool)

	// Compare formatItems (ungrouped CFs not in any TRaSH group)
	if detailData != nil {
		for _, fi := range detailData.FormatItemNames {
			desiredScore := fi.Score
			var description string
			if tcf, ok := ad.CustomFormats[fi.TrashID]; ok {
				description = tcf.Description
			}
			cfi := CompareFormatItem{
				TrashID:      fi.TrashID,
				Name:         fi.Name,
				Description:  description,
				DesiredScore: desiredScore,
			}
			if arrCF, ok := arrByName[strings.ToLower(fi.Name)]; ok {
				cfi.Exists = true
				cfi.ArrID = arrCF.ID
				cfi.CurrentScore = arrScores[arrCF.ID]
				cfi.ScoreMatch = cfi.CurrentScore == cfi.DesiredScore
				if ov, hasOv := scoreOverrides[fi.TrashID]; hasOv {
					ovCopy := ov
					cfi.ScoreOverride = &ovCopy
				}
				trackedArrIDs[arrCF.ID] = true
				if cfi.ScoreMatch {
					comp.Summary.Matching++
				} else {
					comp.Summary.WrongScore++
				}
			} else {
				comp.Summary.Missing++
			}
			comp.FormatItems = append(comp.FormatItems, cfi)
			// Populate cfStates for backward compat with single-CF sync
			comp.CFStates[fi.TrashID] = ArrCFState{
				Exists:       cfi.Exists,
				ArrID:        cfi.ArrID,
				TrashName:    fi.Name,
				CurrentScore: cfi.CurrentScore,
				DesiredScore: desiredScore,
				ScoreMatch:   cfi.ScoreMatch,
			}
			if cfi.Exists {
				comp.CFStates[fi.TrashID] = ArrCFState{
					Exists:       true,
					ArrID:        cfi.ArrID,
					ArrName:      fi.Name,
					TrashName:    fi.Name,
					CurrentScore: cfi.CurrentScore,
					DesiredScore: desiredScore,
					ScoreMatch:   cfi.ScoreMatch,
				}
			}
		}

		// Compare groups — simple verification model:
		// 1. Required CFs in default groups: must exist with correct score
		// 2. Optional/non-default CFs: only verify if user has them (non-zero score in Arr)
		// 3. Exclusive groups: verify one is chosen with correct score, don't try to add both
		for _, group := range detailData.Groups {
			cg := CompareGroup{
				Name:             group.Name,
				ShortName:        group.ShortName,
				Category:         group.Category,
				TrashDescription: group.TrashDescription,
				DefaultEnabled:   group.DefaultEnabled,
				Exclusive:        group.Exclusive,
				Total:            len(group.CFs),
				CFs:              []CompareCF{},
			}
			for _, cf := range group.CFs {
				var desiredScore int
				if cf.HasScore {
					desiredScore = cf.Score
				}
				ccf := CompareCF{
					TrashID:      cf.TrashID,
					Name:         cf.Name,
					Description:  cf.Description,
					Required:     cf.Required,
					Default:      cf.Default,
					DesiredScore: desiredScore,
				}
				if arrCF, ok := arrByName[strings.ToLower(cf.Name)]; ok {
					ccf.Exists = true
					ccf.ArrID = arrCF.ID
					ccf.CurrentScore = arrScores[arrCF.ID]
					if ov, hasOv := scoreOverrides[cf.TrashID]; hasOv {
						ovCopy := ov
						ccf.ScoreOverride = &ovCopy
					}
					trackedArrIDs[arrCF.ID] = true
					// CF is "in use" if:
					// - it has non-zero score (user actively scored it), OR
					// - it's required in a default group (expected to be there), OR
					// - it was synced via Clonarr (in sync history, even with score 0)
					ccf.InUse = ccf.CurrentScore != 0 || (group.DefaultEnabled && cf.Required) || syncedCFSet[cf.TrashID]

					if ccf.InUse {
						// CF is actively scored by user — verify score matches TRaSH
						ccf.ScoreMatch = ccf.CurrentScore == desiredScore
						cg.Present++
						if ccf.ScoreMatch {
							cg.Matching++
						} else {
							cg.WrongScore++
						}
					} else {
						// CF exists with score 0 — user hasn't actively chosen it
						ccf.ScoreMatch = true // not an error
					}
				} else {
					// CF doesn't exist in Arr
					if group.DefaultEnabled && cf.Required && !group.Exclusive {
						// Required CF in default group is genuinely missing
						// (exclusive groups counted separately — only need ONE)
						cg.Missing++
					}
					// Optional/non-default CFs not in Arr = fine, user didn't want them
				}
				cg.CFs = append(cg.CFs, ccf)
				// Populate cfStates for syncSingleCF backward compat
				comp.CFStates[cf.TrashID] = ArrCFState{
					Exists:       ccf.Exists,
					ArrID:        ccf.ArrID,
					TrashName:    cf.Name,
					Description:  cf.Description,
					CurrentScore: ccf.CurrentScore,
					DesiredScore: desiredScore,
					ScoreMatch:   ccf.ScoreMatch,
				}
			}
			// Exclusive groups: if required AND default-enabled AND no CF is in use,
			// count as 1 missing (need to pick one, not all)
			if cg.Exclusive && cg.DefaultEnabled && cg.Present == 0 {
				hasRequired := false
				for _, c := range cg.CFs {
					if c.Required {
						hasRequired = true
						break
					}
				}
				if hasRequired {
					cg.Missing = 1
				}
			}
			comp.Groups = append(comp.Groups, cg)
			// Global summary: always count present/matching/wrong from groups
			// Missing only from default+required
			comp.Summary.Matching += cg.Matching
			comp.Summary.WrongScore += cg.WrongScore
			comp.Summary.Missing += cg.Missing
		}
	}

	// LEGACY: keep OptionalCategories populated for old frontend code
	optionalArrIDs := make(map[int]bool)
	requiredGroups, optionalGroups := ProfileCFGroups(ad, trashProfileID)
	catMap := make(map[string][]OptionalGroupState)
	for _, groups := range [][]ProfileCFGroup{requiredGroups, optionalGroups} {
		for _, pg := range groups {
			category, shortName := parseCategoryPrefix(pg.Name)
			descLower := strings.ToLower(pg.TrashDescription)
			exclusive := strings.Contains(descLower, "only score or enable one") ||
				strings.Contains(descLower, "only enable one")
			gs := OptionalGroupState{
				Name:        pg.Name,
				ShortName:   shortName,
				Description: pg.TrashDescription,
				Exclusive:   exclusive,
				Total:       len(pg.CFs),
			}
			for _, cfEntry := range pg.CFs {
				var desiredScore int
				if cfEntry.HasScore {
					desiredScore = cfEntry.Score
				}
				var cfDesc string
				if tcf, ok := ad.CustomFormats[cfEntry.TrashID]; ok {
					cfDesc = tcf.Description
				}
				ocs := OptionalCFState{
					TrashID:      cfEntry.TrashID,
					Name:         cfEntry.Name,
					Description:  cfDesc,
					Default:      cfEntry.Default,
					DesiredScore: desiredScore,
				}
				if arrCF, ok := arrByName[strings.ToLower(cfEntry.Name)]; ok {
					ocs.Exists = true
					ocs.ArrID = arrCF.ID
					ocs.CurrentScore = arrScores[arrCF.ID]
					ocs.ScoreMatch = ocs.CurrentScore == desiredScore
					gs.Present++
					if !ocs.ScoreMatch {
						gs.WrongScore++
					}
					optionalArrIDs[arrCF.ID] = true
				}
				gs.CFs = append(gs.CFs, ocs)
			}
			catMap[category] = append(catMap[category], gs)
		}
	}
	var catNames []string
	for cat := range catMap {
		catNames = append(catNames, cat)
	}
	sort.Slice(catNames, func(i, j int) bool {
		return getCategoryOrder(catNames[i]) < getCategoryOrder(catNames[j])
	})
	for _, cat := range catNames {
		comp.OptionalCategories = append(comp.OptionalCategories, CompareCategory{
			Category: cat,
			Groups:   catMap[cat],
		})
	}

	// Build set of Arr CF IDs known to Clonarr via prior sync (as intentional extras or score
	// overrides). Sync history stores trashIDs that Clonarr pushed — including any Extra CFs
	// added via the profile-detail "Add Extra CFs" override. Without this, a score=0 extra
	// would be hidden from the Compare (since score=0 usually means "unused in Arr profile").
	// By matching these trashIDs back to Arr CF IDs, we recognize them as deliberate extras
	// and show them in the diff — user can see exactly what's non-standard on the profile.
	currentTrashIDs := make(map[string]bool)
	if detailData != nil {
		for _, fi := range detailData.FormatItemNames {
			currentTrashIDs[fi.TrashID] = true
		}
		for _, group := range detailData.Groups {
			for _, cf := range group.CFs {
				currentTrashIDs[cf.TrashID] = true
			}
		}
	}
	knownExtraArrIDs := make(map[int]bool)
	collectExtraID := func(tid string) {
		if currentTrashIDs[tid] {
			return
		}
		tcf, ok := ad.CustomFormats[tid]
		if !ok {
			return
		}
		if arrCF, ok := arrByName[strings.ToLower(tcf.Name)]; ok {
			knownExtraArrIDs[arrCF.ID] = true
		}
	}
	for tid := range syncedCFSet {
		collectExtraID(tid)
	}
	for tid := range scoreOverrides {
		collectExtraID(tid)
	}

	// Extra CFs: in Arr profile, not tracked by any TRaSH CF. Non-zero score → always included.
	// Zero score → only included if Clonarr's sync history knows about it (intentional extra).
	for _, fi := range arrProfile.FormatItems {
		if fi.Score == 0 && !knownExtraArrIDs[fi.Format] {
			continue
		}
		if trackedArrIDs[fi.Format] {
			continue
		}
		if optionalArrIDs[fi.Format] {
			continue // accounted for by legacy optional group
		}
		comp.ExtraCFs = append(comp.ExtraCFs, fi)
		comp.Summary.Extra++
	}

	// Compare profile settings
	addSetting := func(name, current, desired string) {
		match := current == desired
		comp.SettingsDiffs = append(comp.SettingsDiffs, SettingDiff{
			Name: name, Current: current, Desired: desired, Match: match,
		})
		if !match {
			comp.Summary.SettingsDiffs++
		}
	}
	addSetting("Upgrade Allowed", fmt.Sprintf("%v", arrProfile.UpgradeAllowed), fmt.Sprintf("%v", trashProfile.UpgradeAllowed))
	addSetting("Min Format Score", fmt.Sprintf("%d", arrProfile.MinFormatScore), fmt.Sprintf("%d", trashProfile.MinFormatScore))
	addSetting("Cutoff Format Score", fmt.Sprintf("%d", arrProfile.CutoffFormatScore), fmt.Sprintf("%d", trashProfile.CutoffFormatScore))
	addSetting("Min Upgrade Format Score", fmt.Sprintf("%d", arrProfile.MinUpgradeFormatScore), fmt.Sprintf("%d", trashProfile.MinUpgradeFormatScore))
	// Language (Radarr only — Sonarr profiles don't have a language field)
	if inst.Type == "radarr" {
		arrLang := "Unknown"
		if arrProfile.Language != nil {
			arrLang = arrProfile.Language.Name
		}
		addSetting("Language", arrLang, trashProfile.Language)
	}
	// Cutoff: TRaSH stores as quality name, Arr stores as ID — resolve for display
	arrCutoffName := "Unknown"
	for _, item := range arrProfile.Items {
		id := 0
		name := item.Name
		if item.Quality != nil {
			id = item.Quality.ID
			if name == "" {
				name = item.Quality.Name
			}
		} else {
			id = item.ID
		}
		if id == arrProfile.Cutoff {
			arrCutoffName = name
			break
		}
	}
	addSetting("Cutoff", arrCutoffName, trashProfile.Cutoff)

	// Compare quality items: TRaSH items vs Arr items
	// Build Arr quality state: name → allowed
	arrQuality := make(map[string]bool)
	var collectArrQualities func(items []ArrQualityItem)
	collectArrQualities = func(items []ArrQualityItem) {
		for _, item := range items {
			name := item.Name
			if name == "" && item.Quality != nil {
				name = item.Quality.Name
			}
			if name != "" {
				arrQuality[strings.ToLower(name)] = item.Allowed
			}
			if len(item.Items) > 0 {
				collectArrQualities(item.Items)
			}
		}
	}
	collectArrQualities(arrProfile.Items)

	// Compare against TRaSH quality items
	for _, tItem := range trashProfile.Items {
		name := tItem.Name
		desiredAllowed := tItem.Allowed
		currentAllowed, exists := arrQuality[strings.ToLower(name)]
		if !exists {
			// Quality item not found in Arr — skip (may be a group name that maps differently)
			continue
		}
		match := currentAllowed == desiredAllowed
		comp.QualityDiffs = append(comp.QualityDiffs, QualityItemDiff{
			Name:           name,
			CurrentAllowed: currentAllowed,
			DesiredAllowed: desiredAllowed,
			Match:          match,
		})
		if !match {
			comp.Summary.QualityDiffs++
		}
	}

	return comp
}

// handleCompareProfile handles GET /api/instances/{id}/compare?arrProfileId=X&trashProfileId=Y
func (app *App) handleCompareProfile(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := app.config.GetInstance(instID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	arrProfileIDStr := r.URL.Query().Get("arrProfileId")
	trashProfileID := r.URL.Query().Get("trashProfileId")
	if arrProfileIDStr == "" || trashProfileID == "" {
		writeError(w, 400, "arrProfileId and trashProfileId are required")
		return
	}

	arrProfileID, err := strconv.Atoi(arrProfileIDStr)
	if err != nil {
		writeError(w, 400, "arrProfileId must be a number")
		return
	}

	ad := app.trash.GetAppData(inst.Type)
	if ad == nil {
		writeError(w, 404, "No TRaSH data available")
		return
	}

	// Get synced CFs + score overrides from sync history. SyncedCFs distinguishes deliberately
	// synced score-0 CFs from Arr defaults; ScoreOverrides lets the Compare see user-chosen
	// desired scores (e.g. intentional 0 to disable a CF) so they don't show as "wrong score".
	var lastSyncedCFs []string
	var lastScoreOverrides map[string]int
	history := app.config.GetSyncHistory(inst.ID)
	for _, sh := range history {
		if sh.ArrProfileID == arrProfileID {
			lastSyncedCFs = sh.SyncedCFs
			lastScoreOverrides = sh.ScoreOverrides
			break
		}
	}
	comp := buildProfileComparison(inst, ad, trashProfileID, arrProfileID, lastSyncedCFs, lastScoreOverrides, app.httpClient)

	if comp.Error == "" {
		app.debugLog.Logf(LogCompare, "%q vs %q on %s | %d matching, %d wrong, %d missing, %d extra",
			comp.ArrProfileName, comp.TrashProfileName, inst.Name,
			comp.Summary.Matching, comp.Summary.WrongScore, comp.Summary.Missing, comp.Summary.Extra)
	} else {
		app.debugLog.Logf(LogError, "Compare failed on %s: %s", inst.Name, comp.Error)
	}

	writeJSON(w, comp)
}

// handleRemoveProfileCFs handles POST /api/instances/{id}/profile-cfs/remove
// Removes specified CF scores from an Arr quality profile (sets score to 0).
func (app *App) handleRemoveProfileCFs(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := app.config.GetInstance(instID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	var req struct {
		ArrProfileID int   `json:"arrProfileId"`
		CFIDs        []int `json:"cfIds"` // Arr CF IDs to remove (set score to 0)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if req.ArrProfileID == 0 || len(req.CFIDs) == 0 {
		writeError(w, 400, "arrProfileId and cfIds are required")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}

	var profile *ArrQualityProfile
	for i := range profiles {
		if profiles[i].ID == req.ArrProfileID {
			profile = &profiles[i]
			break
		}
	}
	if profile == nil {
		writeError(w, 404, "Profile not found")
		return
	}

	removeSet := make(map[int]bool, len(req.CFIDs))
	for _, id := range req.CFIDs {
		removeSet[id] = true
	}

	// Set score to 0 for removed CFs (Arr requires all CFs in FormatItems)
	removed := 0
	for i := range profile.FormatItems {
		if removeSet[profile.FormatItems[i].Format] {
			profile.FormatItems[i].Score = 0
			removed++
		}
	}

	if err := client.UpdateProfile(profile); err != nil {
		writeError(w, 502, "Failed to update profile: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"removed": removed})
}

// handleSyncSingleCF handles POST /api/instances/{id}/profile-cfs/sync-one
// Syncs a single CF: creates it if missing, then sets the correct score in the profile.
func (app *App) handleSyncSingleCF(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := app.config.GetInstance(instID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	var req struct {
		ArrProfileID int    `json:"arrProfileId"`
		TrashID      string `json:"trashId"`
		Score        int    `json:"score"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if req.ArrProfileID == 0 || req.TrashID == "" {
		writeError(w, 400, "arrProfileId and trashId are required")
		return
	}

	ad := app.trash.GetAppData(inst.Type)
	if ad == nil {
		writeError(w, 404, "No TRaSH data available")
		return
	}
	trashCF := ad.CustomFormats[req.TrashID]
	if trashCF == nil {
		writeError(w, 404, "CF not found in TRaSH data")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	// Check if CF already exists in instance
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch CFs: "+err.Error())
		return
	}
	var arrCFID int
	for _, cf := range arrCFs {
		if strings.EqualFold(cf.Name, trashCF.Name) {
			arrCFID = cf.ID
			break
		}
	}

	action := "updated"
	if arrCFID == 0 {
		// Create the CF
		newCF := trashCFToArr(trashCF)
		created, err := client.CreateCustomFormat(newCF)
		if err != nil {
			writeError(w, 502, "Failed to create CF: "+err.Error())
			return
		}
		arrCFID = created.ID
		action = "created"
	} else {
		// Update existing CF specs to match TRaSH (handles name case differences and spec changes)
		updatedCF := trashCFToArr(trashCF)
		if _, err := client.UpdateCustomFormat(arrCFID, updatedCF); err != nil {
			writeError(w, 502, "Failed to update CF: "+err.Error())
			return
		}
	}

	// Set score in profile
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}
	var profile *ArrQualityProfile
	for i := range profiles {
		if profiles[i].ID == req.ArrProfileID {
			profile = &profiles[i]
			break
		}
	}
	if profile == nil {
		writeError(w, 404, "Profile not found")
		return
	}

	// Update or add score in FormatItems
	found := false
	for i := range profile.FormatItems {
		if profile.FormatItems[i].Format == arrCFID {
			profile.FormatItems[i].Score = req.Score
			found = true
			break
		}
	}
	if !found {
		profile.FormatItems = append(profile.FormatItems, ArrProfileFormatItem{
			Format: arrCFID,
			Name:   trashCF.Name,
			Score:  req.Score,
		})
	}

	if err := client.UpdateProfile(profile); err != nil {
		writeError(w, 502, "Failed to update profile: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"action": action, "name": trashCF.Name, "score": req.Score})

	tidShort := req.TrashID
	if len(tidShort) > 8 { tidShort = tidShort[:8] }
	app.debugLog.Logf(LogSync, "Single CF sync: %q (%s) score=%d on %s | action=%s",
		trashCF.Name, tidShort, req.Score, inst.Name, action)
}

func (app *App) handleTrashProfileDetail(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	trashID := r.PathValue("id")

	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	// Single snapshot for entire request (C1: safe after lock release)
	snap := app.trash.Snapshot()
	ad := SnapshotAppData(snap, appType)
	if ad == nil {
		writeError(w, 404, "No TRaSH data")
		return
	}

	var profile *TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == trashID {
			profile = p
			break
		}
	}
	if profile == nil {
		writeError(w, 404, "Profile not found")
		return
	}

	// Resolve formatItems CFs for sync engine (still needed for sync)
	resolvedCFs, scoreCtx := ResolveProfileCFs(ad, trashID)

	// New: TRaSH group-based detail data for sync view
	detailData := ProfileDetailData(ad, trashID)

	// Legacy: category-based groups (kept for backward compat until frontend fully migrated)
	cfCategories := ProfileCFCategories(ad, trashID)

	// Build response
	resp := map[string]any{
		"profile":            profile,
		"scoreCtx":           scoreCtx,
		"coreCFs":            resolvedCFs,
		"totalCoreCFs":       len(resolvedCFs),
		"cfCategories":       cfCategories,
		"formatItemNames":    detailData.FormatItemNames,
		"trashGroups":        detailData.Groups,
		"formatItemsOrder":   profile.FormatItemsOrder,
	}

	writeJSON(w, resp)
}

func (app *App) handleTrashQualitySizes(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	sizes := ad.QualitySizes
	if sizes == nil {
		sizes = []*TrashQualitySize{}
	}
	writeJSON(w, sizes)
}

func (app *App) handleTrashNaming(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := app.trash.GetAppData(appType)
	if ad == nil || ad.Naming == nil {
		writeJSON(w, map[string]any{})
		return
	}
	writeJSON(w, ad.Naming)
}

// --- Import ---

func (app *App) handleImportProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	var req struct {
		YAML     string `json:"yaml"`
		Name     string `json:"name"`    // optional override name
		AppType  string `json:"appType"` // for TRaSH JSON detection context (from active tab)
		Includes []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"includes"` // optional include files for Recyclarr configs
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.YAML)
	if content == "" {
		writeError(w, 400, "content is required")
		return
	}

	// Resolve Recyclarr includes: merge include file contents into the main YAML
	if len(req.Includes) > 0 {
		includeMap := make(map[string]string)
		for _, inc := range req.Includes {
			includeMap[strings.ToLower(inc.Name)] = inc.Content
		}
		content = mergeRecyclarrIncludes(content, includeMap)
	}

	// Build TRaSH data map for group resolution + CF name lookup
	snap := app.trash.Snapshot()
	trashData := map[string]*AppData{
		"radarr": SnapshotAppData(snap, "radarr"),
		"sonarr": SnapshotAppData(snap, "sonarr"),
	}

	// Auto-detect format: try JSON first, fall back to YAML
	var profiles []ImportedProfile
	if strings.HasPrefix(content, "{") {
		appType := req.AppType
		ad := trashData[appType] // may be nil for "auto" — parseProfileJSON handles it
		p, err := parseProfileJSON([]byte(content), appType, ad, trashData)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		profiles = []ImportedProfile{*p}
	} else {
		var err error
		profiles, err = parseRecyclarrYAML([]byte(content), trashData, req.AppType)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range profiles {
		profiles[i].ImportedAt = now
		// Allow name override for single-profile imports
		if req.Name != "" && len(profiles) == 1 {
			profiles[i].Name = req.Name
		}
	}

	// Resolve CF names from TRaSH data
	for i := range profiles {
		ad := trashData[profiles[i].AppType]
		if ad == nil {
			continue
		}
		// Replace comment-based names with actual TRaSH CF names where possible
		for tid := range profiles[i].FormatItems {
			if cf, ok := ad.CustomFormats[tid]; ok {
				if profiles[i].FormatComments == nil {
					profiles[i].FormatComments = make(map[string]string)
				}
				profiles[i].FormatComments[tid] = cf.Name
			}
		}
	}

	added, skipped, err := app.profiles.Add(profiles)
	if err != nil {
		writeError(w, 500, "failed to save: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"imported": added,
		"skipped":  skipped,
		"profiles": profiles,
	})
}


func (app *App) handleGetImportedProfiles(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	profiles := app.profiles.List(appType)
	if profiles == nil {
		profiles = []ImportedProfile{}
	}
	writeJSON(w, profiles)
}

func (app *App) handleImportedProfileDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profile, ok := app.profiles.Get(id)
	if !ok {
		writeError(w, 404, "profile not found")
		return
	}

	snap := app.trash.Snapshot()
	ad := SnapshotAppData(snap, profile.AppType)

	// If profile has a trashProfileId and TRaSH data is available, use the standard path
	if profile.TrashProfileID != "" && ad != nil {
		detailData := ProfileDetailData(ad, profile.TrashProfileID)
		if detailData != nil {
			writeJSON(w, map[string]any{
				"profile": map[string]any{
					"name":                  profile.Name,
					"upgradeAllowed":        profile.UpgradeAllowed,
					"cutoff":                profile.Cutoff,
					"minFormatScore":        profile.MinFormatScore,
					"cutoffFormatScore":     profile.CutoffScore,
					"minUpgradeFormatScore": profile.MinUpgradeFormatScore,
					"language":              profile.Language,
					"scoreSet":              profile.ScoreSet,
					"items":                 profile.Qualities,
				},
				"trashGroups":    detailData.Groups,
				"formatItemNames": detailData.FormatItemNames,
				"totalCoreCFs":   len(profile.FormatItems),
				"imported":       true,
				"importedRaw":    profile,
			})
			return
		}
	}

	// No trashProfileId or TRaSH lookup failed — build from imported data + TRaSH groups
	var detailData *ProfileDetailResult
	if ad != nil {
		detailData = ImportedProfileDetailData(ad, &profile)
	}

	// Build complete quality items list: imported items + all missing items from
	// TRaSH as disabled. This ensures the quality override editor shows all
	// available quality levels, not just the ones from the YAML.
	allItems := make([]QualityItem, len(profile.Qualities))
	copy(allItems, profile.Qualities)
	if ad != nil && len(ad.Profiles) > 0 {
		// Use first TRaSH profile's items as the complete list
		baseItems := ad.Profiles[0].Items
		importedNames := make(map[string]bool)
		for _, q := range profile.Qualities {
			importedNames[q.Name] = true
			// Also track sub-items for groups
			for _, sub := range q.Items {
				importedNames[sub] = true
			}
		}
		// Append missing items as disabled
		for _, ti := range baseItems {
			if !importedNames[ti.Name] {
				allItems = append(allItems, QualityItem{
					Name:    ti.Name,
					Allowed: false,
					Items:   ti.Items,
				})
			}
		}
	}

	resp := map[string]any{
		"profile": map[string]any{
			"name":                  profile.Name,
			"upgradeAllowed":        profile.UpgradeAllowed,
			"cutoff":                profile.Cutoff,
			"minFormatScore":        profile.MinFormatScore,
			"cutoffFormatScore":     profile.CutoffScore,
			"minUpgradeFormatScore": profile.MinUpgradeFormatScore,
			"language":              profile.Language,
			"scoreSet":              profile.ScoreSet,
			"items":                 allItems,
		},
		"totalCoreCFs": len(profile.FormatItems),
		"imported":     true,
		"importedRaw":  profile,
	}
	if detailData != nil {
		resp["trashGroups"] = detailData.Groups
		resp["formatItemNames"] = detailData.FormatItemNames
	}
	writeJSON(w, resp)
}

func (app *App) handleUpdateImportedProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profile, ok := app.profiles.Get(id)
	if !ok {
		writeError(w, 404, "profile not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		VariantGoldenRule string `json:"variantGoldenRule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	if req.VariantGoldenRule != "" {
		v := strings.ToUpper(req.VariantGoldenRule)
		if v != "HD" && v != "UHD" {
			writeError(w, 400, "variantGoldenRule must be 'HD' or 'UHD'")
			return
		}
		profile.VariantGoldenRule = v
	}

	if err := app.profiles.Update(profile); err != nil {
		writeError(w, 500, "failed to update: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
}

func (app *App) handleDeleteImportedProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := app.profiles.Delete(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// --- Custom Profiles ---

func (app *App) handleQualityPresets(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []QualityPreset{})
		return
	}
	writeJSON(w, QualityPresets(ad))
}

func (app *App) handleAllCFsCategorized(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := app.trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, CFPickerData{})
		return
	}
	customCFs := app.customCFs.List(appType)
	writeJSON(w, AllCFsCategorized(ad, customCFs))
}

func (app *App) handleCreateCustomProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var p ImportedProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if p.Name == "" {
		writeError(w, 400, "name is required")
		return
	}
	if p.AppType != "radarr" && p.AppType != "sonarr" {
		writeError(w, 400, "appType must be 'radarr' or 'sonarr'")
		return
	}

	p.ID = generateID()
	p.Source = "custom"
	p.ImportedAt = time.Now().UTC().Format(time.RFC3339)

	if _, _, err := app.profiles.Add([]ImportedProfile{p}); err != nil {
		writeError(w, 500, "Failed to save: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, p)
}

func (app *App) handleUpdateCustomProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := app.profiles.Get(id)
	if !ok {
		writeError(w, 404, "Profile not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var p ImportedProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	p.ID = id
	if p.Source == "" {
		p.Source = existing.Source
	}
	if p.ImportedAt == "" {
		p.ImportedAt = existing.ImportedAt
	}

	if err := app.profiles.Update(p); err != nil {
		writeError(w, 500, "Failed to save: "+err.Error())
		return
	}
	writeJSON(w, p)
}

// --- Sync ---

// C5: Per-instance sync mutex prevents parallel applies creating duplicates
var syncMutexes sync.Map // instance ID → *sync.Mutex

func getSyncMutex(instanceID string) *sync.Mutex {
	v, _ := syncMutexes.LoadOrStore(instanceID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (app *App) handleDryRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	inst, ok := app.config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	ad := app.trash.GetAppData(inst.Type)
	var imported *ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := app.profiles.Get(req.ImportedProfileID)
		if !ok {
			writeError(w, 404, "Imported profile not found")
			return
		}
		imported = &p
	}
	customCFs := app.customCFs.List(inst.Type)
	lastSyncedCFs := app.getLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, app.httpClient)
	if err != nil {
		log.Printf("Dry-run error for %s: %v", inst.Name, err)
		writeError(w, 400, err.Error())
		return
	}

	behavior := ResolveSyncBehavior(req.Behavior)
	app.debugLog.Logf(LogSync, "Dry-run: %q → %s | %d selected CFs | overrides: %s | behavior: %s/%s/%s",
		plan.ProfileName, inst.Name, len(req.SelectedCFs),
		overrideSummary(req.Overrides), behavior.AddMode, behavior.RemoveMode, behavior.ResetMode)
	app.debugLog.Logf(LogSync, "Dry-run result: %d create, %d update, %d unchanged | %d scores to set, %d to zero",
		plan.Summary.CFsToCreate, plan.Summary.CFsToUpdate, plan.Summary.CFsUnchanged,
		plan.Summary.ScoresToSet, plan.Summary.ScoresToZero)

	writeJSON(w, plan)
}

func (app *App) handleApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	inst, ok := app.config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	// C5: Only one sync per instance at a time
	mu := getSyncMutex(inst.ID)
	if !mu.TryLock() {
		writeError(w, 409, "Sync already in progress for this instance")
		return
	}
	defer mu.Unlock()

	// Single snapshot for both plan + execute (C2: prevents data drift between steps)
	ad := app.trash.GetAppData(inst.Type)
	var imported *ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := app.profiles.Get(req.ImportedProfileID)
		if !ok {
			writeError(w, 404, "Imported profile not found")
			return
		}
		imported = &p
	}
	customCFs := app.customCFs.List(inst.Type)
	lastSyncedCFs := app.getLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	behavior := ResolveSyncBehavior(req.Behavior)
	plan, err := BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, app.httpClient)
	if err != nil {
		log.Printf("Apply plan error for %s: %v", inst.Name, err)
		app.debugLog.Logf(LogError, "Apply plan error for %s: %v", inst.Name, err)
		writeError(w, 500, "Failed to build sync plan")
		return
	}

	result, err := ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, behavior, app.httpClient)
	if err != nil {
		log.Printf("Apply exec error for %s: %v", inst.Name, err)
		app.debugLog.Logf(LogError, "Apply exec error for %s: %v", inst.Name, err)
		writeError(w, 500, "Failed to execute sync")
		return
	}

	app.debugLog.Logf(LogSync, "Apply: %q → %s | arrProfileId=%d | mode=%s | %d created, %d updated, %d scores | %d errors",
		plan.ProfileName, inst.Name, req.ArrProfileID, func() string { if req.ArrProfileID == 0 { return "create" }; return "update" }(),
		result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, len(result.Errors))
	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			app.debugLog.Logf(LogError, "Apply error: %s", e)
		}
	}

	// Record sync history
	allCFIDs := make([]string, 0)
	for _, a := range plan.CFActions {
		allCFIDs = append(allCFIDs, a.TrashID)
	}
	// Build selectedCFs map from request (for resync restore)
	selectedCFMap := make(map[string]bool, len(req.SelectedCFs))
	for _, id := range req.SelectedCFs {
		selectedCFMap[id] = true
	}
	// Build change details. Start with the sync result's human-readable strings
	// (score changes, CF creates/updates, quality/settings changes), then enrich
	// with CF set diff (CFs added to or removed from the sync set) by comparing
	// allCFIDs against the previous entry's SyncedCFs. This catches group-level
	// changes (e.g. disabling "Streaming Services General" drops 18 CFs) that
	// the score engine doesn't report when the CFs had score=0.
	cfSetDetails := []string{}
	prevEntry := app.config.GetLatestSyncEntry(inst.ID, req.ArrProfileID)
	if prevEntry != nil {
		prevSet := make(map[string]bool, len(prevEntry.SyncedCFs))
		for _, id := range prevEntry.SyncedCFs {
			prevSet[id] = true
		}
		newSet := make(map[string]bool, len(allCFIDs))
		for _, id := range allCFIDs {
			newSet[id] = true
		}
		resolveName := func(tid string) string {
			if ad != nil {
				if cf, ok := ad.CustomFormats[tid]; ok {
					return cf.Name
				}
			}
			for _, a := range plan.CFActions {
				if a.TrashID == tid {
					return a.Name
				}
			}
			return tid[:min(len(tid), 12)]
		}
		for _, tid := range allCFIDs {
			if !prevSet[tid] {
				cfSetDetails = append(cfSetDetails, "Added: "+resolveName(tid))
			}
		}
		for _, tid := range prevEntry.SyncedCFs {
			if !newSet[tid] {
				cfSetDetails = append(cfSetDetails, "Removed: "+resolveName(tid))
			}
		}
	}
	// Merge: cfSetDetails (from set diff) + result.CFDetails (creates/updates)
	allCFDetails := append(cfSetDetails, result.CFDetails...)
	var changes *SyncChanges
	if len(allCFDetails) > 0 || len(result.ScoreDetails) > 0 ||
		len(result.QualityDetails) > 0 || len(result.SettingsDetails) > 0 {
		changes = &SyncChanges{
			CFDetails:       allCFDetails,
			ScoreDetails:    result.ScoreDetails,
			QualityDetails:  result.QualityDetails,
			SettingsDetails: result.SettingsDetails,
		}
	}

	entry := SyncHistoryEntry{
		InstanceID:        inst.ID,
		InstanceType:      inst.Type,
		ProfileTrashID:    req.ProfileTrashID,
		ImportedProfileID: req.ImportedProfileID,
		ProfileName:       plan.ProfileName,
		ArrProfileID:   req.ArrProfileID,
		ArrProfileName: plan.ArrProfileName,
		SyncedCFs:      allCFIDs,
		SelectedCFs:    selectedCFMap,
		ScoreOverrides:   req.ScoreOverrides,
		QualityOverrides: req.QualityOverrides,
		QualityStructure: req.QualityStructure,
		Overrides:      req.Overrides,
		Behavior:       req.Behavior,
		CFsCreated:     result.CFsCreated,
		CFsUpdated:     result.CFsUpdated,
		ScoresUpdated:  result.ScoresUpdated,
		LastSync:       time.Now().Format(time.RFC3339),
		Changes:        changes,
	}
	// Use newly created profile info when available
	if result.ProfileCreated {
		entry.ArrProfileID = result.ArrProfileID
		entry.ArrProfileName = result.ArrProfileName
		// Update auto-sync rule that has arrProfileId=0 (was waiting for profile creation)
		app.config.Update(func(cfg *Config) {
			for i := range cfg.AutoSync.Rules {
				r := &cfg.AutoSync.Rules[i]
				if r.ArrProfileID == 0 && r.InstanceID == req.InstanceID &&
					((r.TrashProfileID != "" && r.TrashProfileID == req.ProfileTrashID) ||
						(r.ImportedProfileID != "" && r.ImportedProfileID == req.ImportedProfileID)) {
					log.Printf("Sync: updating auto-sync rule %s with new Arr profile ID %d", r.ID, result.ArrProfileID)
					app.debugLog.Logf(LogSync, "Auto-sync rule %s updated with new Arr profile ID %d", r.ID, result.ArrProfileID)
					r.ArrProfileID = result.ArrProfileID
					return
				}
			}
		})
	}
	if err := app.config.UpsertSyncHistory(entry); err != nil {
		log.Printf("Failed to save sync history: %v", err)
		app.debugLog.Logf(LogError, "Failed to save sync history: %v", err)
	}

	// Ensure an auto-sync rule exists for this profile (disabled by default)
	// If a rule exists but source type changed (builder↔TRaSH), update it to match
	arrID := req.ArrProfileID
	if result.ProfileCreated {
		arrID = result.ArrProfileID
	}
	newSource := "trash"
	if req.ImportedProfileID != "" {
		newSource = "imported"
	}
	app.config.Update(func(cfg *Config) {
		for i, r := range cfg.AutoSync.Rules {
			if r.InstanceID == req.InstanceID && r.ArrProfileID == arrID {
				// Rule exists — update source type and selections if they changed
				if r.ProfileSource != newSource || r.TrashProfileID != req.ProfileTrashID || r.ImportedProfileID != req.ImportedProfileID {
					app.debugLog.Logf(LogSync, "Auto-sync rule %s: updating source %s→%s for Arr profile %d", r.ID, r.ProfileSource, newSource, arrID)
					cfg.AutoSync.Rules[i].ProfileSource = newSource
					cfg.AutoSync.Rules[i].TrashProfileID = req.ProfileTrashID
					cfg.AutoSync.Rules[i].ImportedProfileID = req.ImportedProfileID
					cfg.AutoSync.Rules[i].SelectedCFs = req.SelectedCFs
					cfg.AutoSync.Rules[i].ScoreOverrides = req.ScoreOverrides
					cfg.AutoSync.Rules[i].QualityOverrides = req.QualityOverrides
					cfg.AutoSync.Rules[i].QualityStructure = req.QualityStructure
					cfg.AutoSync.Rules[i].Behavior = req.Behavior
					cfg.AutoSync.Rules[i].Overrides = req.Overrides
				}
				return
			}
		}
		cfg.AutoSync.Rules = append(cfg.AutoSync.Rules, AutoSyncRule{
			ID:                generateID(),
			Enabled:           false,
			InstanceID:        req.InstanceID,
			ProfileSource:     newSource,
			TrashProfileID:    req.ProfileTrashID,
			ImportedProfileID: req.ImportedProfileID,
			ArrProfileID:      arrID,
			SelectedCFs:       req.SelectedCFs,
			ScoreOverrides:    req.ScoreOverrides,
			QualityOverrides:  req.QualityOverrides,
			QualityStructure:  req.QualityStructure,
			Behavior:          req.Behavior,
			Overrides:         req.Overrides,
		})
	})

	writeJSON(w, result)
}

// --- Sync History ---

// getLastSyncedCFs returns the CF snapshot from the previous sync for "add_new" mode.
// Returns nil if not needed (behavior is not "add_new") or no history exists.
func (app *App) getLastSyncedCFs(instanceID string, arrProfileID int, behavior *SyncBehavior) []string {
	b := ResolveSyncBehavior(behavior)
	if b.AddMode != "add_new" {
		return nil
	}
	history := app.config.GetSyncHistory(instanceID)
	for _, h := range history {
		if h.ArrProfileID == arrProfileID {
			return h.SyncedCFs
		}
	}
	return nil // no history = first sync, all CFs are "new"
}

func (app *App) handleSyncHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}
	// Clean up stale entries for this instance before returning (only if instance is reachable)
	inst, ok := app.config.GetInstance(id)
	if ok {
		client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			app.debugLog.Logf(LogAutoSync, "Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
		} else if len(profiles) == 0 {
			log.Printf("Cleanup: skipping %s — returned 0 profiles (instance may still be starting)", inst.Name)
			app.debugLog.Logf(LogAutoSync, "Cleanup: skipping %s — 0 profiles returned, likely still starting", inst.Name)
		} else {
			validIDs := make(map[int]bool)
			for _, p := range profiles {
				validIDs[p.ID] = true
			}
			var events []CleanupEvent
			app.config.Update(func(cfg *Config) {
				cleanedHistory := make([]SyncHistoryEntry, 0, len(cfg.SyncHistory))
				for _, h := range cfg.SyncHistory {
					if h.InstanceID == id && !validIDs[h.ArrProfileID] {
						log.Printf("Cleanup: removing stale sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
						app.debugLog.Logf(LogAutoSync, "Cleanup: removing stale sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
						events = append(events, CleanupEvent{
							ProfileName:  h.ProfileName,
							InstanceName: inst.Name,
							ArrProfileID: h.ArrProfileID,
							Timestamp:    time.Now().Format(time.RFC3339),
						})
						continue
					}
					cleanedHistory = append(cleanedHistory, h)
				}
				cleanedRules := make([]AutoSyncRule, 0, len(cfg.AutoSync.Rules))
				for _, r := range cfg.AutoSync.Rules {
					if r.InstanceID == id && !validIDs[r.ArrProfileID] && r.ArrProfileID != 0 {
						log.Printf("Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, inst.Name)
						app.debugLog.Logf(LogAutoSync, "Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, inst.Name)
						continue
					}
					cleanedRules = append(cleanedRules, r)
				}
				if len(events) > 0 || len(cleanedRules) < len(cfg.AutoSync.Rules) {
					cfg.SyncHistory = cleanedHistory
					cfg.AutoSync.Rules = cleanedRules
				}
			})
			if len(events) > 0 {
				app.cleanupMu.Lock()
				app.cleanupEvents = append(app.cleanupEvents, events...)
				if len(app.cleanupEvents) > 50 {
					trimmed := make([]CleanupEvent, 50)
					copy(trimmed, app.cleanupEvents[len(app.cleanupEvents)-50:])
					app.cleanupEvents = trimmed
				}
				app.cleanupMu.Unlock()
				app.notifyCleanup(events)
			}
		}
	}
	entries := app.config.GetSyncHistory(id)
	if entries == nil {
		entries = []SyncHistoryEntry{}
	}
	writeJSON(w, entries)
}

func (app *App) handleProfileChangeHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	arrProfileIDStr := r.PathValue("arrProfileId")
	arrProfileID, err := strconv.Atoi(arrProfileIDStr)
	if err != nil || id == "" {
		writeError(w, 400, "Invalid instance or profile ID")
		return
	}
	entries := app.config.GetProfileChangeHistory(id, arrProfileID)
	if entries == nil {
		entries = []SyncHistoryEntry{}
	}
	writeJSON(w, entries)
}

func (app *App) handleDeleteSyncHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	arrProfileIDStr := r.PathValue("arrProfileId")
	if id == "" || arrProfileIDStr == "" {
		writeError(w, 400, "Missing instance ID or Arr profile ID")
		return
	}
	arrProfileID, err := strconv.Atoi(arrProfileIDStr)
	if err != nil {
		writeError(w, 400, "arrProfileId must be a number")
		return
	}
	if err := app.config.DeleteSyncHistory(id, arrProfileID); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleCleanupEvents returns and clears pending cleanup events.
func (app *App) handleCleanupEvents(w http.ResponseWriter, r *http.Request) {
	app.cleanupMu.Lock()
	events := app.cleanupEvents
	app.cleanupEvents = nil
	app.cleanupMu.Unlock()
	if events == nil {
		events = []CleanupEvent{}
	}
	writeJSON(w, events)
}

// handleAutoSyncEvents returns and clears pending auto-sync events for frontend toast.
func (app *App) handleAutoSyncEvents(w http.ResponseWriter, r *http.Request) {
	app.autoSyncMu.Lock()
	events := app.autoSyncEvents
	app.autoSyncEvents = nil
	app.autoSyncMu.Unlock()
	if events == nil {
		events = []AutoSyncEvent{}
	}
	writeJSON(w, events)
}

// --- Helpers ---

const maskSentinel = "********"

func maskKey(key string) string {
	if len(key) <= 8 {
		return maskSentinel
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// isMasked detects if a key was produced by maskKey.
func isMasked(key string) bool {
	if key == "" || key == maskSentinel {
		return true
	}
	// maskKey produces: 4 chars + N asterisks + 4 chars (len >= 9)
	if len(key) < 9 {
		return false
	}
	mid := key[4 : len(key)-4]
	for _, c := range mid {
		if c != '*' {
			return false
		}
	}
	return len(mid) > 0
}

// =============================================================================
// CLEANUP HANDLERS
// =============================================================================

// CleanupScanResult is the dry-run response for cleanup operations.
type CleanupScanResult struct {
	Action      string        `json:"action"`
	InstanceID  string        `json:"instanceId"`
	Instance    string        `json:"instance"`
	Items       []CleanupItem `json:"items"`
	TotalCount  int           `json:"totalCount"`
	AffectCount int           `json:"affectCount"`
}

type CleanupItem struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Detail   string `json:"detail,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
}

// handleCleanupScan performs a dry-run scan for the requested cleanup action.
// POST /api/instances/{id}/cleanup/scan
func (app *App) handleCleanupScan(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := app.config.GetInstance(instanceID)
	if !ok {
		writeError(w, http.StatusNotFound, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	var req struct {
		Action string   `json:"action"`
		Keep   []string `json:"keep"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	switch req.Action {
	case "duplicates":
		result, err := scanDuplicateCFs(client, inst)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, result)

	case "delete-cfs-keep-scores":
		result, err := scanAllCFs(client, inst, "delete-cfs-keep-scores", req.Keep)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, result)

	case "delete-cfs-and-scores":
		result, err := scanAllCFs(client, inst, "delete-cfs-and-scores", req.Keep)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, result)

	case "reset-unsynced-scores":
		result, err := scanUnsyncedScores(app, client, inst)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, result)

	case "orphaned-scores":
		result, err := scanOrphanedScores(client, inst)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, result)

	default:
		writeError(w, http.StatusBadRequest, "Unknown action: "+req.Action)
	}
}

// handleCleanupApply executes the cleanup action.
// POST /api/instances/{id}/cleanup/apply
func (app *App) handleCleanupApply(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := app.config.GetInstance(instanceID)
	if !ok {
		writeError(w, http.StatusNotFound, "Instance not found")
		return
	}

	// Prevent concurrent cleanup/sync on the same instance
	mu := getSyncMutex(inst.ID)
	if !mu.TryLock() {
		writeError(w, 409, "Sync or cleanup already in progress for this instance")
		return
	}
	defer mu.Unlock()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	var req struct {
		Action string `json:"action"`
		IDs    []int  `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}

	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)

	switch req.Action {
	case "duplicates":
		count, err := applyDeleteCFs(client, req.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"deleted": count})

	case "delete-cfs-keep-scores":
		count, err := applyDeleteCFs(client, req.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"deleted": count})

	case "delete-cfs-and-scores":
		// Delete CFs first, then reset scores only for the deleted CFs.
		// This order is safer: if CF deletion fails partway through, orphaned
		// scores are harmless and easy to clean up.
		deletedIDs := make(map[int]bool, len(req.IDs))
		for _, id := range req.IDs {
			deletedIDs[id] = true
		}
		count, err := applyDeleteCFs(client, req.IDs)
		if err != nil {
			writeJSON(w, map[string]any{"deleted": count, "scoresReset": 0, "error": "CF deletion failed: " + err.Error()})
			return
		}
		profiles, err := client.ListProfiles()
		if err != nil {
			writeJSON(w, map[string]any{"deleted": count, "scoresReset": 0, "error": "CFs deleted but failed to list profiles for score reset: " + err.Error()})
			return
		}
		resetCount := 0
		for i := range profiles {
			changed := false
			for j := range profiles[i].FormatItems {
				if profiles[i].FormatItems[j].Score != 0 && deletedIDs[profiles[i].FormatItems[j].Format] {
					profiles[i].FormatItems[j].Score = 0
					changed = true
					resetCount++
				}
			}
			if changed {
				if err := client.UpdateProfile(&profiles[i]); err != nil {
					log.Printf("CLEANUP: Failed to reset scores on profile %s: %v", profiles[i].Name, err)
				}
			}
		}
		writeJSON(w, map[string]any{"deleted": count, "scoresReset": resetCount})

	case "reset-unsynced-scores":
		count, err := applyResetScores(client, req.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"scoresReset": count})

	case "orphaned-scores":
		count, err := applyResetScores(client, req.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"scoresReset": count})

	default:
		writeError(w, http.StatusBadRequest, "Unknown action: "+req.Action)
	}
}

// --- Scan helpers ---

func scanDuplicateCFs(client *ArrClient, inst Instance) (*CleanupScanResult, error) {
	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}

	// Group by normalized spec fingerprint
	type cfEntry struct {
		id   int
		name string
	}
	groups := make(map[string][]cfEntry)
	for _, cf := range cfs {
		// Build fingerprint from sorted spec names + values
		var parts []string
		for _, spec := range cf.Specifications {
			val := extractFieldValue(spec.Fields)
			parts = append(parts, spec.Name+":"+spec.Implementation+":"+stringify(val))
		}
		sort.Strings(parts)
		key := strings.Join(parts, "|")
		groups[key] = append(groups[key], cfEntry{id: cf.ID, name: cf.Name})
	}

	var items []CleanupItem
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		// Keep the first, flag the rest as duplicates
		for i := 1; i < len(group); i++ {
			items = append(items, CleanupItem{
				ID:     group[i].id,
				Name:   group[i].name,
				Detail: "Duplicate of " + group[0].name,
			})
		}
	}

	return &CleanupScanResult{
		Action:      "duplicates",
		InstanceID:  inst.ID,
		Instance:    inst.Name,
		TotalCount:  len(cfs),
		AffectCount: len(items),
		Items:       items,
	}, nil
}

func scanAllCFs(client *ArrClient, inst Instance, action string, keep []string) (*CleanupScanResult, error) {
	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}

	// Build case-insensitive keep set
	keepSet := make(map[string]bool, len(keep))
	for _, name := range keep {
		keepSet[strings.ToLower(strings.TrimSpace(name))] = true
	}

	items := make([]CleanupItem, 0, len(cfs))
	for _, cf := range cfs {
		if keepSet[strings.ToLower(cf.Name)] {
			continue
		}
		items = append(items, CleanupItem{
			ID:   cf.ID,
			Name: cf.Name,
		})
	}

	return &CleanupScanResult{
		Action:      action,
		InstanceID:  inst.ID,
		Instance:    inst.Name,
		TotalCount:  len(cfs),
		AffectCount: len(items),
		Items:       items,
	}, nil
}

func scanUnsyncedScores(app *App, client *ArrClient, inst Instance) (*CleanupScanResult, error) {
	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}
	profiles, err := client.ListProfiles()
	if err != nil {
		return nil, err
	}

	// Build set of CF names that are in any synced profile
	syncedCFNames := make(map[string]bool)
	importedProfiles := app.profiles.List(inst.Type)
	for _, ip := range importedProfiles {
		for trashID := range ip.FormatItems {
			if comment, ok := ip.FormatComments[trashID]; ok {
				syncedCFNames[strings.ToLower(comment)] = true
			}
		}
	}
	// Also check TRaSH profiles and all synced CFs from sync history
	cfg := app.config.Get()
	ad := app.trash.GetAppData(inst.Type)
	for _, sh := range cfg.SyncHistory {
		if sh.InstanceID == inst.ID {
			// Resolve standard TRaSH profile CFs
			if ad != nil {
				resolved, _ := ResolveProfileCFs(ad, sh.ProfileTrashID)
				for _, rcf := range resolved {
					syncedCFNames[strings.ToLower(rcf.Name)] = true
				}
			}
			// Include ALL CFs from sync history (covers extra CFs, custom CFs, score overrides)
			if ad != nil {
				for _, trashID := range sh.SyncedCFs {
					if cf, ok := ad.CustomFormats[trashID]; ok {
						syncedCFNames[strings.ToLower(cf.Name)] = true
					}
				}
			}
		}
	}
	// Include custom CFs synced to this instance
	customCFs := app.customCFs.List(inst.Type)
	for _, ccf := range customCFs {
		syncedCFNames[strings.ToLower(ccf.Name)] = true
	}

	// Build CF name→ID map
	cfNameToID := make(map[string]int)
	for _, cf := range cfs {
		cfNameToID[cf.Name] = cf.ID
	}

	// Find CFs with non-zero scores that aren't in any synced profile
	// Collect all profiles where each CF has a non-zero score
	type cfScoreInfo struct {
		name    string
		details []string
	}
	cfScores := make(map[int]*cfScoreInfo)
	for _, profile := range profiles {
		for _, fi := range profile.FormatItems {
			if fi.Score == 0 {
				continue
			}
			if info, ok := cfScores[fi.Format]; ok {
				info.details = append(info.details, strconv.Itoa(fi.Score)+" on "+profile.Name)
			} else {
				var cfName string
				for _, cf := range cfs {
					if cf.ID == fi.Format {
						cfName = cf.Name
						break
					}
				}
				if cfName == "" || syncedCFNames[strings.ToLower(cfName)] {
					continue
				}
				cfScores[fi.Format] = &cfScoreInfo{
					name:    cfName,
					details: []string{strconv.Itoa(fi.Score) + " on " + profile.Name},
				}
			}
		}
	}
	var items []CleanupItem
	for cfID, info := range cfScores {
		items = append(items, CleanupItem{
			ID:     cfID,
			Name:   info.name,
			Detail: "Score " + strings.Join(info.details, ", "),
		})
	}

	return &CleanupScanResult{
		Action:      "reset-unsynced-scores",
		InstanceID:  inst.ID,
		Instance:    inst.Name,
		TotalCount:  len(cfs),
		AffectCount: len(items),
		Items:       items,
	}, nil
}

func scanOrphanedScores(client *ArrClient, inst Instance) (*CleanupScanResult, error) {
	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}
	profiles, err := client.ListProfiles()
	if err != nil {
		return nil, err
	}

	// Build set of existing CF IDs
	cfIDs := make(map[int]bool)
	for _, cf := range cfs {
		cfIDs[cf.ID] = true
	}

	// Find profile format items referencing non-existent CFs
	var items []CleanupItem
	seen := make(map[int]bool)
	for _, profile := range profiles {
		for _, fi := range profile.FormatItems {
			if cfIDs[fi.Format] || seen[fi.Format] {
				continue
			}
			seen[fi.Format] = true
			items = append(items, CleanupItem{
				ID:     fi.Format,
				Name:   "CF #" + strconv.Itoa(fi.Format),
				Detail: "Referenced in " + profile.Name + " but CF no longer exists",
			})
		}
	}

	return &CleanupScanResult{
		Action:      "orphaned-scores",
		InstanceID:  inst.ID,
		Instance:    inst.Name,
		TotalCount:  len(cfs),
		AffectCount: len(items),
		Items:       items,
	}, nil
}

// --- Apply helpers ---

func applyDeleteCFs(client *ArrClient, ids []int) (int, error) {
	deleted := 0
	for _, id := range ids {
		if err := client.DeleteCustomFormat(id); err != nil {
			log.Printf("CLEANUP: Failed to delete CF %d: %v", id, err)
			continue
		}
		deleted++
	}
	return deleted, nil
}

func applyResetScores(client *ArrClient, cfIDs []int) (int, error) {
	profiles, err := client.ListProfiles()
	if err != nil {
		return 0, err
	}

	resetSet := make(map[int]bool)
	for _, id := range cfIDs {
		resetSet[id] = true
	}

	resetCount := 0
	for i := range profiles {
		changed := false
		for j := range profiles[i].FormatItems {
			if resetSet[profiles[i].FormatItems[j].Format] && profiles[i].FormatItems[j].Score != 0 {
				profiles[i].FormatItems[j].Score = 0
				changed = true
				resetCount++
			}
		}
		if changed {
			if err := client.UpdateProfile(&profiles[i]); err != nil {
				log.Printf("CLEANUP: Failed to update profile %s: %v", profiles[i].Name, err)
			}
		}
	}

	return resetCount, nil
}

// handleGetCleanupKeep returns the saved keep list for an instance.
func (app *App) handleGetCleanupKeep(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	cfg := app.config.Get()
	keep := cfg.CleanupKeep[instanceID]
	if keep == nil {
		keep = []string{}
	}
	writeJSON(w, keep)
}

// handleSaveCleanupKeep saves the keep list for an instance.
func (app *App) handleSaveCleanupKeep(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	var keep []string
	if err := json.NewDecoder(r.Body).Decode(&keep); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request")
		return
	}
	// Trim whitespace from names
	cleaned := make([]string, 0, len(keep))
	for _, name := range keep {
		name = strings.TrimSpace(name)
		if name != "" {
			cleaned = append(cleaned, name)
		}
	}
	if err := app.config.Update(func(cfg *Config) {
		if cfg.CleanupKeep == nil {
			cfg.CleanupKeep = make(map[string][]string)
		}
		if len(cleaned) == 0 {
			delete(cfg.CleanupKeep, instanceID)
		} else {
			cfg.CleanupKeep[instanceID] = cleaned
		}
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// --- Auto-Sync handlers ---

// handleGetAutoSyncSettings returns the global auto-sync enabled flag.
// Notification agents are managed via /api/auto-sync/notification-agents.
func (app *App) handleGetAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	writeJSON(w, map[string]any{
		"enabled": cfg.AutoSync.Enabled,
	})
}

// handleSaveAutoSyncSettings updates the global auto-sync enabled flag.
func (app *App) handleSaveAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if err := app.config.Update(func(cfg *Config) {
		cfg.AutoSync.Enabled = req.Enabled
	}); err != nil {
		writeError(w, 500, "Failed to save settings")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// --- Notification Agent handlers ---------------------------------------------

// handleListNotificationAgents returns all configured notification agents
// with credentials masked.
func (app *App) handleListNotificationAgents(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	agents := cfg.AutoSync.NotificationAgents
	if agents == nil {
		agents = []NotificationAgent{}
	}
	// Mask credentials before returning
	for i, a := range agents {
		agents[i].Config = maskAgentConfig(a.Type, a.Config)
	}
	writeJSON(w, agents)
}

// handleCreateNotificationAgent adds a new notification agent.
func (app *App) handleCreateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var agent NotificationAgent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	created, err := app.config.AddNotificationAgent(agent)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	created.Config = maskAgentConfig(created.Type, created.Config)
	writeJSON(w, created)
}

// handleUpdateNotificationAgent replaces a notification agent by ID.
func (app *App) handleUpdateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var agent NotificationAgent
	if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	// Resolve masked credentials against stored values
	existing, ok := app.config.GetNotificationAgent(id)
	if !ok {
		writeError(w, 404, "Notification agent not found")
		return
	}
	agent.Config = preserveAgentConfig(agent.Type, agent.Config, existing.Config)
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated, err := app.config.UpdateNotificationAgent(id, agent)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	updated.Config = maskAgentConfig(updated.Type, updated.Config)
	writeJSON(w, updated)
}

// handleDeleteNotificationAgent removes a notification agent by ID.
func (app *App) handleDeleteNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := app.config.DeleteNotificationAgent(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// testResult holds the outcome of testing one endpoint within an agent.
type testResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status == "error"
}

// handleTestNotificationAgentInline tests agent credentials sent inline in the
// request body without requiring a saved agent ID. Used by the add-agent modal.
func (app *App) handleTestNotificationAgentInline(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req NotificationAgent
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	app.runNotificationAgentTest(w, req)
}

// handleTestNotificationAgent fires test messages for an existing saved agent.
// Uses the stored agent config directly — no body parsing needed — so tests
// always use the real (unmasked) credentials regardless of enabled state.
func (app *App) handleTestNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := app.config.GetNotificationAgent(id)
	if !ok {
		writeError(w, 404, "Notification agent not found")
		return
	}
	app.runNotificationAgentTest(w, existing)
}

// runNotificationAgentTest executes the test logic for any notification agent
// and writes the JSON response. Shared by both the inline and saved-agent test handlers.
func (app *App) runNotificationAgentTest(w http.ResponseWriter, req NotificationAgent) {
	var results []testResult

	switch req.Type {
	case "discord":
		nc := req.Config
		// Test sync webhook
		if nc.DiscordWebhook != "" {
			res := testResult{Label: "Sync webhook", Status: "ok"}
			if err := app.testDiscordWebhook(nc.DiscordWebhook); err != nil {
				res.Status = "error"
				res.Error = err.Error()
			}
			results = append(results, res)
		}
		// Test updates webhook (only if set and different from sync)
		if nc.DiscordWebhookUpdates != "" && nc.DiscordWebhookUpdates != nc.DiscordWebhook {
			res := testResult{Label: "Updates webhook", Status: "ok"}
			if err := app.testDiscordWebhook(nc.DiscordWebhookUpdates); err != nil {
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
		resp, err := app.notifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
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
		resp, err := app.safeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
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
func (app *App) testDiscordWebhook(webhook string) error {
	webhook = strings.TrimSpace(webhook)
	if !strings.HasPrefix(webhook, "https://discord.com/api/webhooks/") &&
		!strings.HasPrefix(webhook, "https://discordapp.com/api/webhooks/") {
		return fmt.Errorf("must start with https://discord.com/api/webhooks/")
	}
	embed := map[string]any{
		"title":       "Clonarr Test",
		"description": "If you see this, Discord is configured correctly!",
		"color":       0x58a6ff,
		"footer":      map[string]string{"text": "Clonarr " + Version + " by ProphetSe7en"},
	}
	payload, _ := json.Marshal(map[string]any{"embeds": []any{embed}})
	resp, err := app.safeClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to reach Discord: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Discord returned %d", resp.StatusCode)
	}
	return nil
}

// validateAgentConfig checks that required fields are present for the agent type.
func validateAgentConfig(agent NotificationAgent) error {
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch agent.Type {
	case "discord":
		wh := strings.TrimSpace(agent.Config.DiscordWebhook)
		if wh == "" {
			return fmt.Errorf("sync webhook is required")
		}
		if !strings.HasPrefix(wh, "https://discord.com/api/webhooks/") &&
			!strings.HasPrefix(wh, "https://discordapp.com/api/webhooks/") {
			return fmt.Errorf("Discord webhook must start with https://discord.com/api/webhooks/")
		}
		if wu := strings.TrimSpace(agent.Config.DiscordWebhookUpdates); wu != "" {
			if !strings.HasPrefix(wu, "https://discord.com/api/webhooks/") &&
				!strings.HasPrefix(wu, "https://discordapp.com/api/webhooks/") {
				return fmt.Errorf("Discord updates webhook must start with https://discord.com/api/webhooks/")
			}
		}
	case "gotify":
		u := strings.TrimSpace(agent.Config.GotifyURL)
		if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("Gotify URL must start with http:// or https://")
		}
	case "pushover":
		// No URL validation needed
	default:
		return fmt.Errorf("unknown agent type: %s", agent.Type)
	}
	return nil
}

// placeholder so the last handler below still compiles cleanly
// handleListAutoSyncRules returns all auto-sync rules with instance names resolved.
func (app *App) handleListAutoSyncRules(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()

	type ruleResponse struct {
		AutoSyncRule
		InstanceName string `json:"instanceName"`
		InstanceType string `json:"instanceType"`
	}

	rules := make([]ruleResponse, 0, len(cfg.AutoSync.Rules))
	for _, rule := range cfg.AutoSync.Rules {
		rr := ruleResponse{AutoSyncRule: rule}
		if inst, ok := app.config.GetInstance(rule.InstanceID); ok {
			rr.InstanceName = inst.Name
			rr.InstanceType = inst.Type
		}
		rules = append(rules, rr)
	}
	writeJSON(w, rules)
}

// handleCreateAutoSyncRule creates a new auto-sync rule.
func (app *App) handleCreateAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rule AutoSyncRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	// Validate required fields
	if rule.InstanceID == "" {
		writeError(w, 400, "instanceId is required")
		return
	}
	if _, ok := app.config.GetInstance(rule.InstanceID); !ok {
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

	rule.ID = generateID()

	// Check for duplicate inside Update callback to avoid TOCTOU race
	var duplicate bool
	if err := app.config.Update(func(cfg *Config) {
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
func (app *App) handleUpdateAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rule AutoSyncRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	rule.ID = id

	found := false
	if err := app.config.Update(func(cfg *Config) {
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
func (app *App) handleDeleteAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	found := false
	if err := app.config.Update(func(cfg *Config) {
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

func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// --- Custom CF Handlers ---

func (app *App) handleListCustomCFs(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	cfs := app.customCFs.List(appType)
	if cfs == nil {
		cfs = []CustomCF{}
	}
	writeJSON(w, cfs)
}

func (app *App) handleCreateCustomCFs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var req struct {
		CFs []CustomCF `json:"cfs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if len(req.CFs) == 0 {
		writeError(w, 400, "No custom formats provided")
		return
	}

	// Validate and assign IDs
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range req.CFs {
		if req.CFs[i].Name == "" {
			writeError(w, 400, "CF name is required")
			return
		}
		if req.CFs[i].AppType != "radarr" && req.CFs[i].AppType != "sonarr" {
			writeError(w, 400, "Invalid app type for CF: "+req.CFs[i].Name)
			return
		}
		if req.CFs[i].Category == "" {
			req.CFs[i].Category = "Custom"
		}
		// Always generate ID server-side — the ID is used as a filename,
		// so accepting client-supplied IDs would allow path traversal.
		req.CFs[i].ID = generateCustomID()
		if req.CFs[i].ImportedAt == "" {
			req.CFs[i].ImportedAt = now
		}
	}

	added, err := app.customCFs.Add(req.CFs)
	if err != nil {
		writeError(w, 500, "Failed to save custom CFs: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"added": added, "total": len(req.CFs)})
}

func (app *App) handleDeleteCustomCF(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// The ID contains "custom:" prefix which has a colon — reconstruct from path
	// PathValue("id") captures everything after /api/custom-cfs/
	if !strings.HasPrefix(id, "custom:") {
		// Try to find by raw id (the part after custom:)
		id = "custom:" + id
	}

	if err := app.customCFs.Delete(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func (app *App) handleUpdateCustomCF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "custom:") {
		id = "custom:" + id
	}

	var cf CustomCF
	if err := json.NewDecoder(r.Body).Decode(&cf); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	cf.ID = id

	if cf.Name == "" {
		writeError(w, 400, "CF name is required")
		return
	}
	if cf.AppType != "radarr" && cf.AppType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	if cf.Category == "" {
		cf.Category = "Custom"
	}

	if err := app.customCFs.Update(cf); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
}

func (app *App) handleImportCFsFromInstance(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var req struct {
		InstanceID string   `json:"instanceId"`
		CFNames    []string `json:"cfNames"`    // which CFs to import (by name)
		Category   string   `json:"category"`   // target category
		AppType    string   `json:"appType"`     // "radarr" or "sonarr"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}

	if req.AppType != "radarr" && req.AppType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}

	inst, ok := app.config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	// Fetch all CFs from instance
	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch CFs from instance: "+err.Error())
		return
	}

	// Build lookup of requested names
	wantedNames := make(map[string]bool)
	for _, name := range req.CFNames {
		wantedNames[name] = true
	}

	// Filter and convert
	category := req.Category
	if category == "" {
		category = "Custom"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var toImport []CustomCF
	for _, acf := range arrCFs {
		if len(wantedNames) > 0 && !wantedNames[acf.Name] {
			continue
		}
		toImport = append(toImport, CustomCF{
			ID:             generateCustomID(),
			Name:           acf.Name,
			AppType:        req.AppType,
			Category:       category,
			ArrID:          acf.ID,
			Specifications: acf.Specifications,
			SourceInstance: inst.Name,
			ImportedAt:     now,
		})
	}

	if len(toImport) == 0 {
		writeError(w, 400, "No matching CFs found in instance")
		return
	}

	added, err := app.customCFs.Add(toImport)
	if err != nil {
		writeError(w, 500, "Failed to save imported CFs: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"added":   added,
		"total":   len(toImport),
		"skipped": len(toImport) - added,
	})
}

// --- CF Schema ---

// cfSchemaCache caches CF schema per app type to avoid repeated Arr API calls.
var cfSchemaCache sync.Map // appType → json.RawMessage

// handleCFSchema returns the CF specification schema (available implementations + field definitions).
// Proxied from the first connected instance of the requested app type, cached in memory.
func (app *App) handleCFSchema(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	// Check cache first
	if cached, ok := cfSchemaCache.Load(appType); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached.([]byte))
		return
	}

	// Find first instance of this type
	cfg := app.config.Get()
	var inst *Instance
	for i := range cfg.Instances {
		if cfg.Instances[i].Type == appType {
			inst = &cfg.Instances[i]
			break
		}
	}
	if inst == nil {
		writeError(w, 404, "No "+appType+" instance configured")
		return
	}

	// Fetch schema from Arr API
	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	data, status, err := client.doRequest("GET", "/customformat/schema", nil)
	if err != nil {
		writeError(w, 502, "Failed to fetch schema: "+err.Error())
		return
	}
	if status != 200 {
		writeError(w, 502, fmt.Sprintf("Arr returned HTTP %d", status))
		return
	}

	// Cache and return
	// NOTE: Cache is never explicitly invalidated because the CF schema (available implementations
	// and field definitions) comes from the Arr instance, not the TRaSH repo. It only changes
	// when the Arr software itself is updated, which is rare and a restart clears it.
	cfSchemaCache.Store(appType, data)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// --- Scoring Sandbox ---

func (app *App) handleTestProwlarr(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		URL    string `json:"url"`
		APIKey string `json:"apiKey"`
	}
	// Accept ad-hoc URL/key in body, fall back to saved config
	json.NewDecoder(r.Body).Decode(&req) // ignore error — fields optional
	cfg := app.config.Get()
	if req.URL == "" {
		req.URL = cfg.Prowlarr.URL
	}
	if req.APIKey == "" || isMasked(req.APIKey) {
		req.APIKey = cfg.Prowlarr.APIKey
	}
	if req.URL == "" || req.APIKey == "" {
		writeJSON(w, map[string]any{"connected": false, "error": "Prowlarr URL and API key are required"})
		return
	}
	client := NewProwlarrClient(req.URL, req.APIKey, app.httpClient)
	version, err := client.TestConnection()
	if err != nil {
		writeJSON(w, map[string]any{"connected": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"connected": true, "version": version})
}

func (app *App) handleScoringProwlarrIndexers(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	if !cfg.Prowlarr.Enabled || cfg.Prowlarr.URL == "" {
		writeJSON(w, []any{})
		return
	}
	client := NewProwlarrClient(cfg.Prowlarr.URL, cfg.Prowlarr.APIKey, app.httpClient)
	indexers, err := client.ListIndexers()
	if err != nil {
		writeError(w, 502, "Prowlarr error: "+err.Error())
		return
	}
	writeJSON(w, indexers)
}

func (app *App) handleScoringProwlarrSearch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Query      string `json:"query"`
		Categories []int  `json:"categories"`
		IndexerIDs []int  `json:"indexerIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.Query == "" {
		writeError(w, 400, "query is required")
		return
	}
	if len(req.Query) > 500 {
		writeError(w, 400, "query too long (max 500 characters)")
		return
	}

	cfg := app.config.Get()
	if !cfg.Prowlarr.Enabled || cfg.Prowlarr.URL == "" {
		writeError(w, 400, "Prowlarr not configured or disabled")
		return
	}
	client := NewProwlarrClient(cfg.Prowlarr.URL, cfg.Prowlarr.APIKey, app.httpClient)
	releases, err := client.Search(req.Query, req.Categories, req.IndexerIDs)
	if err != nil {
		writeError(w, 502, "Prowlarr search failed: "+err.Error())
		return
	}
	writeJSON(w, releases)
}

// ScoringParseResult is the enriched parse response for the scoring sandbox.
type ScoringParseResult struct {
	Title     string             `json:"title"`
	Parsed    ScoringParsedInfo  `json:"parsed"`
	MatchedCFs []ScoringMatchedCF `json:"matchedCFs"`
	InstanceScore int            `json:"instanceScore"`
}

type ScoringParsedInfo struct {
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	Quality      string   `json:"quality"`
	Languages    []string `json:"languages"`
	ReleaseGroup string   `json:"releaseGroup"`
	Edition      string   `json:"edition,omitempty"`
	Season       int      `json:"season,omitempty"`
	Episodes     []int    `json:"episodes,omitempty"`
}

type ScoringMatchedCF struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	TrashID string `json:"trashId,omitempty"`
}

func (app *App) handleScoringParse(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		InstanceID string `json:"instanceId"`
		Title      string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.InstanceID == "" || req.Title == "" {
		writeError(w, 400, "instanceId and title are required")
		return
	}

	inst, ok := app.config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	result, err := app.parseSingleRelease(inst, req.Title)
	if err != nil {
		writeError(w, 502, "Parse failed: "+err.Error())
		return
	}
	writeJSON(w, result)
}

func (app *App) handleScoringParseBatch(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		InstanceID string   `json:"instanceId"`
		Titles     []string `json:"titles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.InstanceID == "" || len(req.Titles) == 0 {
		writeError(w, 400, "instanceId and titles are required")
		return
	}
	if len(req.Titles) > 15 {
		writeError(w, 400, "Maximum 15 titles per batch")
		return
	}

	inst, ok := app.config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	results := make([]ScoringParseResult, 0, len(req.Titles))
	for _, title := range req.Titles {
		result, err := app.parseSingleRelease(inst, title)
		if err != nil {
			// Include error as empty result with the title
			results = append(results, ScoringParseResult{Title: title})
			continue
		}
		results = append(results, *result)
	}
	writeJSON(w, results)
}

// isLanguageCF returns true for CFs that depend on language matching — these can't
// be evaluated without movie context (TMDB lookup) so they're stripped from sandbox
// parse results. Matches: "Wrong Language", "Language: Not French", "Language: XXX".
func isLanguageCF(name string) bool {
	lower := strings.ToLower(name)
	return lower == "wrong language" || strings.HasPrefix(lower, "language:")
}

// parseSingleRelease calls the Arr Parse API and enriches CFs with trash_ids.
func (app *App) parseSingleRelease(inst Instance, title string) (*ScoringParseResult, error) {
	client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
	data, status, err := client.doRequest("GET", "/parse?title="+url.QueryEscape(title), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(data))
	}

	// Parse the raw response — Radarr uses parsedMovieInfo, Sonarr uses parsedEpisodeInfo
	var raw struct {
		Title             string          `json:"title"`
		ParsedMovieInfo   json.RawMessage `json:"parsedMovieInfo"`
		ParsedEpisodeInfo json.RawMessage `json:"parsedEpisodeInfo"`
		CustomFormats     []struct {
			ID                             int    `json:"id"`
			Name                           string `json:"name"`
			IncludeCustomFormatWhenRenaming bool   `json:"includeCustomFormatWhenRenaming"`
		} `json:"customFormats"`
		CustomFormatScore int `json:"customFormatScore"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Extract parsed info from the appropriate field
	parsed := ScoringParsedInfo{}
	if len(raw.ParsedMovieInfo) > 0 && string(raw.ParsedMovieInfo) != "null" {
		var movie struct {
			MovieTitles  []string `json:"movieTitles"`
			Year         int      `json:"year"`
			Quality      struct {
				Quality struct {
					Name string `json:"name"`
				} `json:"quality"`
			} `json:"quality"`
			Languages    []struct{ Name string `json:"name"` } `json:"languages"`
			ReleaseGroup string `json:"releaseGroup"`
			Edition      string `json:"edition"`
		}
		if json.Unmarshal(raw.ParsedMovieInfo, &movie) == nil {
			if len(movie.MovieTitles) > 0 {
				parsed.Title = movie.MovieTitles[0]
			}
			parsed.Year = movie.Year
			parsed.Quality = movie.Quality.Quality.Name
			for _, l := range movie.Languages {
				parsed.Languages = append(parsed.Languages, l.Name)
			}
			parsed.ReleaseGroup = movie.ReleaseGroup
			parsed.Edition = movie.Edition
		}
	} else if len(raw.ParsedEpisodeInfo) > 0 && string(raw.ParsedEpisodeInfo) != "null" {
		var ep struct {
			SeriesTitle    string `json:"seriesTitle"`
			SeasonNumber   int    `json:"seasonNumber"`
			EpisodeNumbers []int  `json:"episodeNumbers"`
			Quality        struct {
				Quality struct {
					Name string `json:"name"`
				} `json:"quality"`
			} `json:"quality"`
			Languages    []struct{ Name string `json:"name"` } `json:"languages"`
			ReleaseGroup string `json:"releaseGroup"`
		}
		if json.Unmarshal(raw.ParsedEpisodeInfo, &ep) == nil {
			parsed.Title = ep.SeriesTitle
			parsed.Season = ep.SeasonNumber
			parsed.Episodes = ep.EpisodeNumbers
			parsed.Quality = ep.Quality.Quality.Name
			for _, l := range ep.Languages {
				parsed.Languages = append(parsed.Languages, l.Name)
			}
			parsed.ReleaseGroup = ep.ReleaseGroup
		}
	}

	// Fallback for numeric release groups that Arr's parser drops (e.g. "-126811").
	// Only fires when Arr returned empty — alphanumeric groups stay on Arr's parse.
	if parsed.ReleaseGroup == "" {
		if m := numericReleaseGroupRE.FindStringSubmatch(title); m != nil {
			parsed.ReleaseGroup = m[1]
		}
	}

	// Build CF name → trash_id map from TRaSH data
	ad := app.trash.GetAppData(inst.Type)
	cfNameToTrashID := make(map[string]string)
	if ad != nil {
		for trashID, cf := range ad.CustomFormats {
			cfNameToTrashID[cf.Name] = trashID
		}
	}
	// Also include custom CFs
	customCFs := app.customCFs.List(inst.Type)
	for _, ccf := range customCFs {
		cfNameToTrashID[ccf.Name] = ccf.ID
	}

	// Enrich matched CFs with trash_ids. Language-aware CFs ("Wrong Language",
	// "Language: Not X", etc.) are filtered out: the Parse API runs without movie
	// context so it can't resolve TMDB original language, making language CFs fire
	// incorrectly (returning Language=Unknown → Wrong Language always matches).
	// The language section is omitted from the parsed metadata for the same reason.
	matchedCFs := make([]ScoringMatchedCF, 0, len(raw.CustomFormats))
	for _, cf := range raw.CustomFormats {
		if isLanguageCF(cf.Name) {
			continue
		}
		mcf := ScoringMatchedCF{
			ID:   cf.ID,
			Name: cf.Name,
		}
		if tid, ok := cfNameToTrashID[cf.Name]; ok {
			mcf.TrashID = tid
		}
		matchedCFs = append(matchedCFs, mcf)
	}
	parsed.Languages = nil

	return &ScoringParseResult{
		Title:         title,
		Parsed:        parsed,
		MatchedCFs:    matchedCFs,
		InstanceScore: raw.CustomFormatScore,
	}, nil
}

// handleScoringProfileScores returns the full CF→score map for a profile.
// Supports TRaSH profiles (trash:<id>), imported profiles (imported:<id>),
// and instance profiles (inst:<instanceId>:<profileId>).
func (app *App) handleScoringProfileScores(w http.ResponseWriter, r *http.Request) {
	profileKey := r.URL.Query().Get("profileKey")
	appType := r.URL.Query().Get("appType")
	if profileKey == "" || appType == "" {
		writeError(w, 400, "profileKey and appType are required")
		return
	}

	type ScoreEntry struct {
		TrashID string `json:"trashId"`
		Name    string `json:"name"`
		Score   int    `json:"score"`
	}

	result := struct {
		Scores   []ScoreEntry `json:"scores"`
		MinScore int          `json:"minScore"`
	}{}

	if strings.HasPrefix(profileKey, "trash:") {
		trashID := strings.TrimPrefix(profileKey, "trash:")
		snap := app.trash.Snapshot()
		ad := SnapshotAppData(snap, appType)
		if ad == nil {
			writeJSON(w, result)
			return
		}

		// Find profile for minFormatScore
		for _, p := range ad.Profiles {
			if p.TrashID == trashID {
				result.MinScore = p.MinFormatScore
				break
			}
		}

		// Get core CFs with scores
		resolvedCFs, scoreCtx := ResolveProfileCFs(ad, trashID)
		seen := make(map[string]bool)
		for _, cf := range resolvedCFs {
			result.Scores = append(result.Scores, ScoreEntry{
				TrashID: cf.TrashID,
				Name:    cf.Name,
				Score:   cf.Score,
			})
			seen[cf.TrashID] = true
		}

		// Get optional CFs from categories (use the profile's score context)
		cfCategories := ProfileCFCategories(ad, trashID)
		for _, cat := range cfCategories {
			for _, g := range cat.Groups {
				for _, cf := range g.CFs {
					if seen[cf.TrashID] {
						continue
					}
					// Look up score from TRaSH CF data using the profile's score context
					score := 0
					if fullCF, ok := ad.CustomFormats[cf.TrashID]; ok {
						if s, ok := fullCF.TrashScores[scoreCtx]; ok {
							score = s
						} else if s, ok := fullCF.TrashScores["default"]; ok {
							score = s
						}
					}
					result.Scores = append(result.Scores, ScoreEntry{
						TrashID: cf.TrashID,
						Name:    cf.Name,
						Score:   score,
					})
					seen[cf.TrashID] = true
				}
			}
		}

	} else if strings.HasPrefix(profileKey, "imported:") {
		id := strings.TrimPrefix(profileKey, "imported:")
		prof, ok := app.profiles.Get(id)
		if !ok {
			writeError(w, 404, "Imported profile not found")
			return
		}
		result.MinScore = prof.MinFormatScore

		ad := app.trash.GetAppData(appType)
		for trashID, score := range prof.FormatItems {
			name := trashID
			if ad != nil {
				if cf, ok := ad.CustomFormats[trashID]; ok {
					name = cf.Name
				}
			}
			result.Scores = append(result.Scores, ScoreEntry{
				TrashID: trashID,
				Name:    name,
				Score:   score,
			})
		}

	} else if strings.HasPrefix(profileKey, "inst:") {
		// inst:<profileId> — needs instanceId query param
		instanceID := r.URL.Query().Get("instanceId")
		profileIDStr := strings.TrimPrefix(profileKey, "inst:")
		profileID, err := strconv.Atoi(profileIDStr)
		if err != nil || instanceID == "" {
			writeError(w, 400, "Invalid instance profile key")
			return
		}
		inst, ok := app.config.GetInstance(instanceID)
		if !ok {
			writeError(w, 404, "Instance not found")
			return
		}
		client := NewArrClient(inst.URL, inst.APIKey, app.httpClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			writeError(w, 502, "Failed to fetch profiles: "+err.Error())
			return
		}
		for _, p := range profiles {
			if p.ID == profileID {
				result.MinScore = p.MinFormatScore
				for _, fi := range p.FormatItems {
					result.Scores = append(result.Scores, ScoreEntry{
						TrashID: "",
						Name:    fi.Name,
						Score:   fi.Score,
					})
				}
				break
			}
		}
	}

	// Strip language-aware CFs from profile scores so they don't appear in the
	// sandbox's "Unmatched (in profile, not in release)" list either. Same
	// rationale as the parse-side filter: without movie context, language CFs
	// are noise that confuses the total.
	filtered := result.Scores[:0]
	for _, s := range result.Scores {
		if !isLanguageCF(s.Name) {
			filtered = append(filtered, s)
		}
	}
	result.Scores = filtered

	writeJSON(w, result)
}

// handleDebugLog receives frontend log messages.
//
// Category + Message reach /config/debug.log, which admins download via
// handleDebugDownload after incidents. An authenticated caller who can
// inject control characters (newlines, escape sequences) could forge
// `[TIMESTAMP] [CATEGORY] …` lines to pollute the forensic trail. We
// whitelist Category and sanitize Message before logging.
func (app *App) handleDebugLog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Category string `json:"category"`
		Message  string `json:"message"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		return
	}
	if req.Category == "" {
		req.Category = "UI"
	}
	if !isValidLogCategory(req.Category) {
		req.Category = "UI"
	}
	app.debugLog.Log(req.Category, sanitizeLogField(req.Message))
	w.WriteHeader(204)
}

// isValidLogCategory rejects Category values not in the app's known set.
// Unknown / attacker-supplied categories fall through to "UI" rather than
// being written to disk verbatim.
func isValidLogCategory(c string) bool {
	switch c {
	case LogSync, LogCompare, LogAutoSync, LogTrash, LogError, LogUI, LogConfig:
		return true
	}
	return false
}

// sanitizeLogField strips control characters (CR, LF, NUL, other < 0x20)
// and caps length to 1024 bytes for debug-log context. Prevents log
// forgery: an authenticated caller submitting Message with embedded \n
// must not be able to inject fake `[TIMESTAMP] [CATEGORY] …` lines.
func sanitizeLogField(s string) string {
	const maxLen = 1024
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			b = append(b, ' ')
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

// handleDebugDownload serves the debug log file for download.
func (app *App) handleDebugDownload(w http.ResponseWriter, r *http.Request) {
	path := app.debugLog.FilePath()
	f, err := os.Open(path)
	if err != nil {
		writeError(w, 404, "No debug log file found")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeError(w, 500, "Failed to read log file")
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\"clonarr-debug.log\"")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	io.Copy(w, f)
}
