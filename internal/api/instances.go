package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// --- Instances ---

func (s *Server) handleListInstances(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	instances := cfg.Instances
	if instances == nil {
		instances = []core.Instance{}
	}
	// Mask API keys (safe: Get() returns deep copy)
	for i := range instances {
		if instances[i].APIKey != "" {
			instances[i].APIKey = maskKey(instances[i].APIKey)
		}
	}
	writeJSON(w, instances)
}

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var inst core.Instance
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

	created, err := s.Core.Config.AddInstance(inst)
	if err != nil {
		writeError(w, 500, "Failed to save instance")
		return
	}

	// M2: mask API key in response
	created.APIKey = maskKey(created.APIKey)
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, created)
}

func (s *Server) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var inst core.Instance
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
		existing, ok := s.Core.Config.GetInstance(id)
		if ok {
			inst.APIKey = existing.APIKey
		}
	}

	updated, err := s.Core.Config.UpdateInstance(id, inst)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}

	// M1: mask API key in response
	updated.APIKey = maskKey(updated.APIKey)
	writeJSON(w, updated)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	if err := s.Core.Config.DeleteInstance(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// --- core.Instance Test ---

func (s *Server) handleTestInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}

	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	status, err := client.TestConnection()
	if err != nil {
		errMsg := err.Error()
		if core.IsConnectionError(err) {
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
func (s *Server) handleTestConnection(w http.ResponseWriter, r *http.Request) {
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

	client := arr.NewArrClient(req.URL, req.APIKey, s.Core.HTTPClient)
	status, err := client.TestConnection()
	if err != nil {
		errMsg := err.Error()
		if core.IsConnectionError(err) {
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

// --- core.Instance Data (profiles, CFs from Arr) ---

func (s *Server) handleInstanceProfiles(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to connect to instance")
		return
	}
	writeJSON(w, profiles)
}

// handleQualityDefinitions returns quality names from an Arr instance for the quality builder.
func (s *Server) handleQualityDefinitions(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
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
func (s *Server) handleRenameProfile(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to list profiles: "+err.Error())
		return
	}
	var target *arr.ArrQualityProfile
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
	s.Core.Config.Update(func(cfg *core.Config) {
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
// converts it to an core.ImportedProfile (same as the import system), and saves it.
func (s *Server) handleInstanceProfileExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profileIdStr := r.PathValue("profileId")
	profileId, err := strconv.Atoi(profileIdStr)
	if err != nil {
		writeError(w, 400, "Invalid profile ID")
		return
	}

	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}

	var profile *arr.ArrQualityProfile
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
	ad := s.Core.Trash.GetAppData(inst.Type)
	customCFs := s.Core.CustomCFs.List(inst.Type)
	customCFByID := make(map[string]core.CustomCF, len(customCFs))
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
		for _, sh := range s.Core.Config.GetSyncHistory(inst.ID) {
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
	var qualities []core.QualityItem
	for _, qi := range profile.Items {
		q := core.QualityItem{Allowed: qi.Allowed}
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

	// Create core.ImportedProfile — same struct as the import system
	imported := core.ImportedProfile{
		ID:                    core.GenerateID(),
		Name:                  profile.Name,
		AppType:               inst.Type,
		Source:                "instance",
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

func (s *Server) handleInstanceLanguages(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
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
func (s *Server) handleInstanceBackup(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

	allCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to fetch custom formats: "+err.Error())
		return
	}

	var selectedProfiles []arr.ArrQualityProfile
	var selectedCFs []arr.ArrCF

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
func (s *Server) handleInstanceRestore(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	dryRun := r.URL.Query().Get("dryRun") == "true"

	backup, ok := decodeJSON[struct {
		Profiles      []arr.ArrQualityProfile `json:"profiles"`
		CustomFormats []arr.ArrCF             `json:"customFormats"`
	}](w, r, 10<<20)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

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
	existingCFByName := make(map[string]*arr.ArrCF, len(existingCFs))
	for i := range existingCFs {
		existingCFByName[strings.ToLower(existingCFs[i].Name)] = &existingCFs[i]
	}
	existingProfileByName := make(map[string]*arr.ArrQualityProfile, len(existingProfiles))
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

func (s *Server) handleInstanceCFs(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	cfs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, 502, "Failed to connect to instance")
		return
	}
	writeJSON(w, cfs)
}

func (s *Server) handleInstanceQualitySizes(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		writeError(w, 502, "Failed to fetch quality sizes: "+err.Error())
		return
	}
	writeJSON(w, defs)
}

func (s *Server) handleGetInstanceNaming(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	naming, err := client.GetNaming()
	if err != nil {
		writeError(w, 502, "Failed to fetch naming config: "+err.Error())
		return
	}
	writeJSON(w, naming)
}

func (s *Server) handleApplyNaming(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.requireInstance(w, r)
	if !ok {
		return
	}

	req, ok := decodeJSON[struct {
		Preset  string `json:"preset"` // convenience: e.g. "plex-tmdb" — resolves from TRaSH data
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
		ad := s.Core.Trash.GetAppData(inst.Type)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

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

func (s *Server) handleSyncQualitySizes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	req, ok2 := decodeJSON[struct {
		Definitions []arr.ArrQualityDefinition `json:"definitions"`
		Type        string                     `json:"type"` // convenience: "movie", "anime", etc — builds definitions from TRaSH data
	}](w, r, 1<<20)
	if !ok2 {
		return
	}

	// If type is provided, build definitions by comparing TRaSH data with instance
	if req.Type != "" && len(req.Definitions) == 0 {
		defs, err := s.buildQualitySizeDefs(inst, req.Type)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	if err := client.UpdateQualityDefinitions(req.Definitions); err != nil {
		writeError(w, 502, "Sync failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]any{"status": "synced", "count": len(req.Definitions)})
}

// buildQualitySizeDefs compares TRaSH quality sizes with instance and returns
// definitions that need updating (same logic as autoSyncQualitySizes).
func (s *Server) buildQualitySizeDefs(inst core.Instance, qsType string) ([]arr.ArrQualityDefinition, error) {
	ad := s.Core.Trash.GetAppData(inst.Type)
	if ad == nil {
		return nil, fmt.Errorf("no TRaSH data for %s", inst.Type)
	}
	var trashQS *core.TrashQualitySize
	for _, qs := range ad.QualitySizes {
		if qs.Type == qsType {
			trashQS = qs
			break
		}
	}
	if trashQS == nil {
		return nil, fmt.Errorf("no TRaSH quality sizes for type %q", qsType)
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch definitions: %w", err)
	}

	defByName := make(map[string]*arr.ArrQualityDefinition)
	for i := range defs {
		defByName[defs[i].Quality.Name] = &defs[i]
		defByName[defs[i].Title] = &defs[i]
	}

	cfg := s.Core.Config.Get()
	overrides := cfg.QualitySizeOverrides[inst.ID]

	var updated []arr.ArrQualityDefinition
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
		if math.Abs(arr.FloatVal(def.MinSize)-qs.Min) >= 0.05 ||
			math.Abs(arr.FloatVal(def.PreferredSize)-qs.Preferred) >= 0.05 ||
			math.Abs(arr.FloatVal(def.MaxSize)-qs.Max) >= 0.05 {
			updated = append(updated, arr.ArrQualityDefinition{
				ID:            def.ID,
				Quality:       def.Quality,
				Title:         def.Title,
				MinSize:       arr.FloatPtr(qs.Min),
				PreferredSize: arr.FloatPtr(qs.Preferred),
				MaxSize:       arr.FloatPtr(qs.Max),
			})
		}
	}
	return updated, nil
}

func (s *Server) handleGetQSOverrides(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	overrides := map[string]core.QSOverride{}
	if cfg.QualitySizeOverrides != nil {
		if inst, ok := cfg.QualitySizeOverrides[id]; ok {
			overrides = inst
		}
	}
	writeJSON(w, overrides)
}

func (s *Server) handleSaveQSOverrides(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	var overrides map[string]core.QSOverride
	if err := json.NewDecoder(r.Body).Decode(&overrides); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	err := s.Core.Config.Update(func(cfg *core.Config) {
		if cfg.QualitySizeOverrides == nil {
			cfg.QualitySizeOverrides = make(map[string]map[string]core.QSOverride)
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

func (s *Server) handleGetQSAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	as := core.QSAutoSync{}
	if cfg.QualitySizeAutoSync != nil {
		if v, ok := cfg.QualitySizeAutoSync[id]; ok {
			as = v
		}
	}
	writeJSON(w, as)
}

func (s *Server) handleSaveQSAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var as core.QSAutoSync
	if err := json.NewDecoder(r.Body).Decode(&as); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	err := s.Core.Config.Update(func(cfg *core.Config) {
		if cfg.QualitySizeAutoSync == nil {
			cfg.QualitySizeAutoSync = make(map[string]core.QSAutoSync)
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
func (s *Server) AutoSyncQualitySizes() {
	cfg := s.Core.Config.Get()
	if len(cfg.QualitySizeAutoSync) == 0 {
		return
	}

	for instID, as := range cfg.QualitySizeAutoSync {
		if !as.Enabled || as.Type == "" {
			continue
		}
		inst, ok := s.Core.Config.GetInstance(instID)
		if !ok {
			continue
		}

		// Get TRaSH quality sizes for this type
		ad := s.Core.Trash.GetAppData(inst.Type)
		if ad == nil {
			continue
		}
		var trashQS *core.TrashQualitySize
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
		client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
		defs, err := client.ListQualityDefinitions()
		if err != nil {
			log.Printf("Auto-sync QS [%s]: failed to fetch definitions: %v", inst.Name, err)
			s.Core.DebugLog.Logf(core.LogAutoSync, "QS [%s]: failed to fetch definitions: %v", inst.Name, err)
			continue
		}

		// Build lookup
		defByName := make(map[string]*arr.ArrQualityDefinition)
		for i := range defs {
			defByName[defs[i].Quality.Name] = &defs[i]
			defByName[defs[i].Title] = &defs[i]
		}

		// Get overrides (Custom qualities to skip)
		overrides := cfg.QualitySizeOverrides[instID]

		var updated []arr.ArrQualityDefinition
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

			if math.Abs(arr.FloatVal(def.MinSize)-qs.Min) >= 0.05 ||
				math.Abs(arr.FloatVal(def.PreferredSize)-qs.Preferred) >= 0.05 ||
				math.Abs(arr.FloatVal(def.MaxSize)-qs.Max) >= 0.05 {
				updated = append(updated, arr.ArrQualityDefinition{
					ID:            def.ID,
					Quality:       def.Quality,
					Title:         def.Title,
					MinSize:       arr.FloatPtr(qs.Min),
					PreferredSize: arr.FloatPtr(qs.Preferred),
					MaxSize:       arr.FloatPtr(qs.Max),
				})
			}
		}

		if len(updated) == 0 {
			log.Printf("Auto-sync QS [%s/%s]: all values match", inst.Name, as.Type)
			continue
		}

		if err := client.UpdateQualityDefinitions(updated); err != nil {
			log.Printf("Auto-sync QS [%s/%s]: sync failed: %v", inst.Name, as.Type, err)
			s.Core.DebugLog.Logf(core.LogError, "QS [%s/%s]: sync failed: %v", inst.Name, as.Type, err)
		} else {
			log.Printf("Auto-sync QS [%s/%s]: synced %d qualities", inst.Name, as.Type, len(updated))
			s.Core.DebugLog.Logf(core.LogAutoSync, "QS [%s/%s]: synced %d qualities", inst.Name, as.Type, len(updated))
		}
	}
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
	ArrProfileID     int                        `json:"arrProfileId"`
	ArrProfileName   string                     `json:"arrProfileName"`
	TrashProfileID   string                     `json:"trashProfileId"`
	TrashProfileName string                     `json:"trashProfileName"`
	CFStates         map[string]ArrCFState      `json:"cfStates"` // keyed by trash_id
	ExtraCFs         []arr.ArrProfileFormatItem `json:"extraCFs"` // CFs in Arr profile not in TRaSH profile
	// NEW: TRaSH group-based comparison
	FormatItems []CompareFormatItem `json:"formatItems"`
	Groups      []CompareGroup      `json:"groups"`
	// Profile settings and quality comparison
	SettingsDiffs []SettingDiff          `json:"settingsDiffs,omitempty"`
	QualityDiffs  []QualityItemDiff `json:"qualityDiffs,omitempty"`
	// LEGACY (keep for now)
	OptionalCategories []CompareCategory `json:"optionalCategories"` // optional CF groups categorized
	Summary            ComparisonSummary `json:"summary"`
	Error              string            `json:"error,omitempty"`
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
	Total      int `json:"total"`
	Present    int `json:"present"`
	Matching   int `json:"matching"`
	WrongScore int `json:"wrongScore"`
	Missing    int `json:"missing"`
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
	Category string               `json:"category"`
	Groups   []OptionalGroupState `json:"groups"`
}

// OptionalGroupState describes the state of an optional CF group in the Arr instance.
type OptionalGroupState struct {
	Name        string            `json:"name"`
	ShortName   string            `json:"shortName"`
	Description string            `json:"description,omitempty"`
	Exclusive   bool              `json:"exclusive,omitempty"` // pick one (Golden Rule)
	CFs         []OptionalCFState `json:"cfs"`
	Present     int               `json:"present"`    // how many CFs exist in Arr
	Total       int               `json:"total"`      // total CFs in group
	WrongScore  int               `json:"wrongScore"` // present but wrong score
}

// OptionalCFState describes one CF within an optional/required group.
type OptionalCFState struct {
	TrashID      string `json:"trashId"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Exists       bool   `json:"exists"`
	ArrID        int    `json:"arrId,omitempty"`
	Default      bool   `json:"default"` // TRaSH recommended
	CurrentScore int    `json:"currentScore"`
	DesiredScore int    `json:"desiredScore"`
	ScoreMatch   bool   `json:"scoreMatch"`
}

// buildProfileComparison compares a specific Arr profile against a TRaSH profile.
// Uses TRaSH CF groups consistently: ungrouped formatItems in FormatItems, grouped CFs in Groups.
// scoreOverrides (trash_id → score) carry the user's desired-score-per-CF from a prior Clonarr
// sync. When a CF has an override, the Compare uses that value as the desired score instead of
// TRaSH's default — so CFs deliberately set to 0 (or any custom score) don't show as "wrong".
func buildProfileComparison(inst core.Instance, ad *core.AppData, trashProfileID string, arrProfileID int, syncedCFs []string, scoreOverrides map[string]int, httpClient *http.Client) *ProfileComparison {
	comp := &ProfileComparison{
		ArrProfileID:   arrProfileID,
		TrashProfileID: trashProfileID,
		CFStates:       make(map[string]ArrCFState),
		FormatItems:    []CompareFormatItem{},
		Groups:         []CompareGroup{},
	}

	// Find TRaSH profile
	var trashProfile *core.TrashQualityProfile
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, httpClient)

	// Fetch existing CFs from instance
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		comp.Error = "Failed to fetch CFs: " + err.Error()
		return comp
	}

	// Build name → arr.ArrCF map (case-insensitive keys for matching)
	arrByName := make(map[string]*arr.ArrCF)
	for i := range arrCFs {
		arrByName[strings.ToLower(arrCFs[i].Name)] = &arrCFs[i]
	}

	// Fetch the specific Arr profile to get scores
	arrProfiles, err := client.ListProfiles()
	if err != nil {
		comp.Error = "Failed to fetch profiles: " + err.Error()
		return comp
	}

	var arrProfile *arr.ArrQualityProfile
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

	// Get TRaSH group data via core.ProfileDetailData
	detailData := core.ProfileDetailData(ad, trashProfileID)

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
	requiredGroups, optionalGroups := core.ProfileCFGroups(ad, trashProfileID)
	catMap := make(map[string][]OptionalGroupState)
	for _, groups := range [][]core.ProfileCFGroup{requiredGroups, optionalGroups} {
		for _, pg := range groups {
			category, shortName := core.ParseCategoryPrefix(pg.Name)
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
		return core.GetCategoryOrder(catNames[i]) < core.GetCategoryOrder(catNames[j])
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
	var collectArrQualities func(items []arr.ArrQualityItem)
	collectArrQualities = func(items []arr.ArrQualityItem) {
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
func (s *Server) handleCompareProfile(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(instID)
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

	ad := s.Core.Trash.GetAppData(inst.Type)
	if ad == nil {
		writeError(w, 404, "No TRaSH data available")
		return
	}

	// Get synced CFs + score overrides from sync history. SyncedCFs distinguishes deliberately
	// synced score-0 CFs from Arr defaults; ScoreOverrides lets the Compare see user-chosen
	// desired scores (e.g. intentional 0 to disable a CF) so they don't show as "wrong score".
	var lastSyncedCFs []string
	var lastScoreOverrides map[string]int
	history := s.Core.Config.GetSyncHistory(inst.ID)
	for _, sh := range history {
		if sh.ArrProfileID == arrProfileID {
			lastSyncedCFs = sh.SyncedCFs
			lastScoreOverrides = sh.ScoreOverrides
			break
		}
	}
	comp := buildProfileComparison(inst, ad, trashProfileID, arrProfileID, lastSyncedCFs, lastScoreOverrides, s.Core.HTTPClient)

	if comp.Error == "" {
		s.Core.DebugLog.Logf(core.LogCompare, "%q vs %q on %s | %d matching, %d wrong, %d missing, %d extra",
			comp.ArrProfileName, comp.TrashProfileName, inst.Name,
			comp.Summary.Matching, comp.Summary.WrongScore, comp.Summary.Missing, comp.Summary.Extra)
	} else {
		s.Core.DebugLog.Logf(core.LogError, "Compare failed on %s: %s", inst.Name, comp.Error)
	}

	writeJSON(w, comp)
}

// handleRemoveProfileCFs handles POST /api/instances/{id}/profile-cfs/remove
// Removes specified CF scores from an Arr quality profile (sets score to 0).
func (s *Server) handleRemoveProfileCFs(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(instID)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	profiles, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to fetch profiles: "+err.Error())
		return
	}

	var profile *arr.ArrQualityProfile
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
func (s *Server) handleSyncSingleCF(w http.ResponseWriter, r *http.Request) {
	instID := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(instID)
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

	ad := s.Core.Trash.GetAppData(inst.Type)
	if ad == nil {
		writeError(w, 404, "No TRaSH data available")
		return
	}
	trashCF := ad.CustomFormats[req.TrashID]
	if trashCF == nil {
		writeError(w, 404, "CF not found in TRaSH data")
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

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
		newCF := core.TrashCFToArr(trashCF)
		created, err := client.CreateCustomFormat(newCF)
		if err != nil {
			writeError(w, 502, "Failed to create CF: "+err.Error())
			return
		}
		arrCFID = created.ID
		action = "created"
	} else {
		// Update existing CF specs to match TRaSH (handles name case differences and spec changes)
		updatedCF := core.TrashCFToArr(trashCF)
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
	var profile *arr.ArrQualityProfile
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
		profile.FormatItems = append(profile.FormatItems, arr.ArrProfileFormatItem{
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
	if len(tidShort) > 8 {
		tidShort = tidShort[:8]
	}
	s.Core.DebugLog.Logf(core.LogSync, "Single CF sync: %q (%s) score=%d on %s | action=%s",
		trashCF.Name, tidShort, req.Score, inst.Name, action)
}

func (s *Server) handleTrashProfileDetail(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	trashID := r.PathValue("id")

	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	// Single snapshot for entire request (C1: safe after lock release)
	snap := s.Core.Trash.Snapshot()
	ad := core.SnapshotAppData(snap, appType)
	if ad == nil {
		writeError(w, 404, "No TRaSH data")
		return
	}

	var profile *core.TrashQualityProfile
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
	resolvedCFs, scoreCtx := core.ResolveProfileCFs(ad, trashID)

	// New: TRaSH group-based detail data for sync view
	detailData := core.ProfileDetailData(ad, trashID)

	// Legacy: category-based groups (kept for backward compat until frontend fully migrated)
	cfCategories := core.ProfileCFCategories(ad, trashID)

	// Build response
	resp := map[string]any{
		"profile":          profile,
		"scoreCtx":         scoreCtx,
		"coreCFs":          resolvedCFs,
		"totalCoreCFs":     len(resolvedCFs),
		"cfCategories":     cfCategories,
		"formatItemNames":  detailData.FormatItemNames,
		"trashGroups":      detailData.Groups,
		"formatItemsOrder": profile.FormatItemsOrder,
	}

	writeJSON(w, resp)
}

func (s *Server) handleTrashQualitySizes(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	sizes := ad.QualitySizes
	if sizes == nil {
		sizes = []*core.TrashQualitySize{}
	}
	writeJSON(w, sizes)
}

func (s *Server) handleTrashNaming(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil || ad.Naming == nil {
		writeJSON(w, map[string]any{})
		return
	}
	writeJSON(w, ad.Naming)
}

