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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
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
	// Wrap config with version for frontend
	writeJSON(w, struct {
		Config
		Version string `json:"version"`
	}{cfg, Version})
}

func (app *App) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var req struct {
		TrashRepo    *TrashRepo      `json:"trashRepo,omitempty"`
		PullInterval *string         `json:"pullInterval,omitempty"`
		DevMode      *bool           `json:"devMode,omitempty"`
		DebugLogging *bool           `json:"debugLogging"`
		Prowlarr     *ProwlarrConfig `json:"prowlarr,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	pullChanged := false
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

	// Build CF name → trash_id map from TRaSH data
	ad := app.trash.GetAppData(inst.Type)
	nameToTrashID := make(map[string]string)
	if ad != nil {
		for trashID, cf := range ad.CustomFormats {
			nameToTrashID[cf.Name] = trashID
		}
	}

	// Map profile formatItems: Arr CF ID → trash_id + score
	// Only include non-zero scored CFs — score=0 CFs cannot be distinguished
	// from unrelated CFs in the instance (Arr lists ALL CFs on every profile)
	formatItems := make(map[string]int)
	formatComments := make(map[string]string)
	unmapped := []string{}
	for _, fi := range profile.FormatItems {
		if fi.Score == 0 {
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
		if math.Abs(def.MinSize-qs.Min) >= 0.05 ||
			math.Abs(def.PreferredSize-qs.Preferred) >= 0.05 ||
			math.Abs(def.MaxSize-qs.Max) >= 0.05 {
			updated = append(updated, ArrQualityDefinition{
				ID:            def.ID,
				Quality:       def.Quality,
				Title:         def.Title,
				MinSize:       qs.Min,
				PreferredSize: qs.Preferred,
				MaxSize:       qs.Max,
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

			if math.Abs(def.MinSize-qs.Min) >= 0.05 ||
				math.Abs(def.PreferredSize-qs.Preferred) >= 0.05 ||
				math.Abs(def.MaxSize-qs.Max) >= 0.05 {
				updated = append(updated, ArrQualityDefinition{
					ID:            def.ID,
					Quality:       def.Quality,
					Title:         def.Title,
					MinSize:       qs.Min,
					PreferredSize: qs.Preferred,
					MaxSize:       qs.Max,
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
	go func() {
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
	}()
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
type CompareFormatItem struct {
	TrashID      string `json:"trashId"`
	Name         string `json:"name"`
	Exists       bool   `json:"exists"`
	ArrID        int    `json:"arrId,omitempty"`
	CurrentScore int    `json:"currentScore"`
	DesiredScore int    `json:"desiredScore"`
	ScoreMatch   bool   `json:"scoreMatch"`
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
	TrashID      string `json:"trashId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Required     bool   `json:"required"`
	Default      bool   `json:"default"`
	Exists       bool   `json:"exists"`
	InUse        bool   `json:"inUse"`          // non-zero score in Arr (user actively chose this)
	ArrID        int    `json:"arrId,omitempty"`
	CurrentScore int    `json:"currentScore"`
	DesiredScore int    `json:"desiredScore"`
	ScoreMatch   bool   `json:"scoreMatch"`
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
func buildProfileComparison(inst Instance, ad *AppData, trashProfileID string, arrProfileID int, syncedCFs []string, httpClient *http.Client) *ProfileComparison {
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
			cfi := CompareFormatItem{
				TrashID:      fi.TrashID,
				Name:         fi.Name,
				DesiredScore: desiredScore,
			}
			if arrCF, ok := arrByName[strings.ToLower(fi.Name)]; ok {
				cfi.Exists = true
				cfi.ArrID = arrCF.ID
				cfi.CurrentScore = arrScores[arrCF.ID]
				cfi.ScoreMatch = cfi.CurrentScore == cfi.DesiredScore
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
					if group.DefaultEnabled && cf.Required {
						// Required CF in default group is genuinely missing
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

	// Extra CFs: in Arr profile with non-zero score, not tracked by any TRaSH CF
	for _, fi := range arrProfile.FormatItems {
		if fi.Score == 0 {
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
	// Language
	arrLang := "Unknown"
	if arrProfile.Language != nil {
		arrLang = arrProfile.Language.Name
	}
	addSetting("Language", arrLang, trashProfile.Language)
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

	// Get synced CFs from sync history (to distinguish deliberately synced score-0 CFs from Arr defaults)
	var lastSyncedCFs []string
	history := app.config.GetSyncHistory(inst.ID)
	for _, sh := range history {
		if sh.ArrProfileID == arrProfileID {
			lastSyncedCFs = sh.SyncedCFs
			break
		}
	}
	comp := buildProfileComparison(inst, ad, trashProfileID, arrProfileID, lastSyncedCFs, app.httpClient)

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
		YAML    string `json:"yaml"`
		Name    string `json:"name"`    // optional override name
		AppType string `json:"appType"` // for TRaSH JSON detection context (from active tab)
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
		if appType == "" {
			appType = "radarr"
		}
		ad := trashData[appType]
		p, err := parseProfileJSON([]byte(content), appType, ad)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		profiles = []ImportedProfile{*p}
	} else {
		var err error
		profiles, err = parseRecyclarrYAML([]byte(content), trashData)
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

	if _, err := app.profiles.Add(profiles); err != nil {
		writeError(w, 500, "failed to save: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"imported": len(profiles),
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

	if _, err := app.profiles.Add([]ImportedProfile{p}); err != nil {
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
		// First reset all scores to 0, then delete CFs
		profiles, err := client.ListProfiles()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to list profiles: "+err.Error())
			return
		}
		resetCount := 0
		for i := range profiles {
			changed := false
			for j := range profiles[i].FormatItems {
				if profiles[i].FormatItems[j].Score != 0 {
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
		count, err := applyDeleteCFs(client, req.IDs)
		if err != nil {
			// Report partial success: scores were already reset
			writeJSON(w, map[string]any{"deleted": count, "scoresReset": resetCount, "error": err.Error()})
			return
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

// handleGetAutoSyncSettings returns the global auto-sync settings (without rules).
func (app *App) handleGetAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	cfg := app.config.Get()
	writeJSON(w, map[string]any{
		"enabled":               cfg.AutoSync.Enabled,
		"notifyOnSuccess":       cfg.AutoSync.NotifyOnSuccess,
		"notifyOnFailure":       cfg.AutoSync.NotifyOnFailure,
		"notifyOnRepoUpdate":    cfg.AutoSync.NotifyOnRepoUpdate,
		"discordWebhook":        cfg.AutoSync.DiscordWebhook,
		"discordWebhookUpdates": cfg.AutoSync.DiscordWebhookUpdates,
		"gotifyEnabled":         cfg.AutoSync.GotifyEnabled,
		"gotifyUrl":             cfg.AutoSync.GotifyURL,
		"gotifyToken":           cfg.AutoSync.GotifyToken,
		"gotifyPriorityCritical": cfg.AutoSync.GotifyPriorityCritical,
		"gotifyPriorityWarning":  cfg.AutoSync.GotifyPriorityWarning,
		"gotifyPriorityInfo":     cfg.AutoSync.GotifyPriorityInfo,
		"gotifyCriticalValue":    cfg.AutoSync.GotifyCriticalValue,
		"gotifyWarningValue":     cfg.AutoSync.GotifyWarningValue,
		"gotifyInfoValue":        cfg.AutoSync.GotifyInfoValue,
	})
}

// handleSaveAutoSyncSettings updates global auto-sync settings.
func (app *App) handleSaveAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Enabled               bool   `json:"enabled"`
		NotifyOnSuccess       bool   `json:"notifyOnSuccess"`
		NotifyOnFailure       bool   `json:"notifyOnFailure"`
		NotifyOnRepoUpdate    bool   `json:"notifyOnRepoUpdate"`
		DiscordWebhook        string `json:"discordWebhook"`
		DiscordWebhookUpdates string `json:"discordWebhookUpdates"`
		GotifyEnabled         bool   `json:"gotifyEnabled"`
		GotifyURL             string `json:"gotifyUrl"`
		GotifyToken           string `json:"gotifyToken"`
		GotifyPriorityCritical bool  `json:"gotifyPriorityCritical"`
		GotifyPriorityWarning  bool  `json:"gotifyPriorityWarning"`
		GotifyPriorityInfo     bool  `json:"gotifyPriorityInfo"`
		GotifyCriticalValue    int   `json:"gotifyCriticalValue"`
		GotifyWarningValue     int   `json:"gotifyWarningValue"`
		GotifyInfoValue        int   `json:"gotifyInfoValue"`
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

	if err := app.config.Update(func(cfg *Config) {
		cfg.AutoSync.Enabled = req.Enabled
		cfg.AutoSync.NotifyOnSuccess = req.NotifyOnSuccess
		cfg.AutoSync.NotifyOnFailure = req.NotifyOnFailure
		cfg.AutoSync.NotifyOnRepoUpdate = req.NotifyOnRepoUpdate
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
	}); err != nil {
		writeError(w, 500, "Failed to save settings")
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func (app *App) handleTestGotify(w http.ResponseWriter, r *http.Request) {
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
		"title":   "Clonarr Test",
		"message": "If you see this, Gotify is configured correctly!",
		"priority": 5,
		"extras": map[string]any{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	}
	body, _ := json.Marshal(payload)
	gotifyURL := strings.TrimRight(req.URL, "/") + "/message?token=" + url.QueryEscape(req.Token)
	resp, err := app.notifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
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
		if req.CFs[i].ID == "" {
			req.CFs[i].ID = generateCustomID()
		}
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

	// Enrich matched CFs with trash_ids
	matchedCFs := make([]ScoringMatchedCF, 0, len(raw.CustomFormats))
	for _, cf := range raw.CustomFormats {
		mcf := ScoringMatchedCF{
			ID:   cf.ID,
			Name: cf.Name,
		}
		if tid, ok := cfNameToTrashID[cf.Name]; ok {
			mcf.TrashID = tid
		}
		matchedCFs = append(matchedCFs, mcf)
	}

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

	writeJSON(w, result)
}

// handleDebugLog receives frontend log messages.
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
	app.debugLog.Log(req.Category, req.Message)
	w.WriteHeader(204)
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
