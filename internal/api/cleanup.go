package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// =============================================================================
// CLEANUP — types, handlers, scan helpers, apply helpers
// =============================================================================

// CleanupScanResult is the dry-run response for cleanup operations.
type CleanupScanResult struct {
	Action      string        `json:"action"`
	InstanceID  string        `json:"instanceId"`
	Instance    string        `json:"instance"`
	Items       []CleanupItem `json:"items"`
	TotalCount  int           `json:"totalCount"`
	AffectCount int           `json:"affectCount"`
	// NamingUsesCustomFormats reports whether the instance's file-naming
	// format string contains the {Custom Formats} token. Only set by
	// unused-by-clonarr scans. The frontend uses this to render an info
	// box explaining rename-flag-tagged CFs in the user's actual context.
	NamingUsesCustomFormats bool `json:"namingUsesCustomFormats,omitempty"`
	// ManagedItems lists CFs that ARE referenced by clonarr's sync rules
	// — the complement of Items. Returned only by unused-by-clonarr scans
	// for the "Managed" filter view in the frontend so the user can see
	// what's actually in active use and which Arr profiles use it. Never
	// deleted by cleanupApply.
	ManagedItems []ManagedCFRef `json:"managedItems,omitempty"`
}

type CleanupItem struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Detail   string   `json:"detail,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
	// RenamingFlag is true when the CF has includeCustomFormatWhenRenaming
	// set in Arr. When the instance's naming format uses the {Custom Formats}
	// token, deleting these CFs removes their tags from filenames rendered
	// after the delete (existing files unaffected). Only set by
	// unused-by-clonarr scans.
	RenamingFlag bool `json:"renamingFlag,omitempty"`
}

// ManagedCFRef describes a CF that is currently referenced by a clonarr
// sync rule (selected, score-overridden, or required by the rule's TRaSH/
// imported profile). UsedInProfiles lists every Arr quality profile that
// has the CF at a non-zero score — i.e. where the CF is actually
// influencing release decisions, not just present at score 0. Returned by
// unused-by-clonarr scans for the "Managed" filter view, never deleted.
type ManagedCFRef struct {
	ID             int      `json:"id"`
	Name           string   `json:"name"`
	UsedInProfiles []string `json:"usedInProfiles,omitempty"`
	RenamingFlag   bool     `json:"renamingFlag,omitempty"`
}

// --- Handlers ---

// handleCleanupScan performs a dry-run scan for the requested cleanup action.
// POST /api/instances/{id}/cleanup/scan
func (s *Server) handleCleanupScan(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(instanceID)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

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
		result, err := scanUnsyncedScores(s.Core, client, inst)
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

	case "unused-by-clonarr":
		result, err := scanUnusedByClonarr(s.Core, client, inst, req.Keep)
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
func (s *Server) handleCleanupApply(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(instanceID)
	if !ok {
		writeError(w, http.StatusNotFound, "Instance not found")
		return
	}

	// Prevent concurrent cleanup/sync on the same instance
	mu := s.Core.GetSyncMutex(inst.ID)
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

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)

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

	case "unused-by-clonarr":
		count, err := applyDeleteCFs(client, req.IDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]any{"deleted": count})

	default:
		writeError(w, http.StatusBadRequest, "Unknown action: "+req.Action)
	}
}

// handleGetCleanupKeep returns the saved keep list for an instance.
func (s *Server) handleGetCleanupKeep(w http.ResponseWriter, r *http.Request) {
	instanceID := r.PathValue("id")
	cfg := s.Core.Config.Get()
	keep := cfg.CleanupKeep[instanceID]
	if keep == nil {
		keep = []string{}
	}
	writeJSON(w, keep)
}

// handleSaveCleanupKeep saves the keep list for an instance.
func (s *Server) handleSaveCleanupKeep(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Core.Config.Update(func(cfg *core.Config) {
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

// --- Scan helpers ---

func scanDuplicateCFs(client *arr.ArrClient, inst core.Instance) (*CleanupScanResult, error) {
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
		// Build fingerprint from sorted spec names + implementation + ALL fields
		var parts []string
		for _, spec := range cf.Specifications {
			parts = append(parts, spec.Name+":"+spec.Implementation+":"+fingerprintFields(spec.Fields))
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

// fingerprintFields builds a deterministic string from all fields in a specification.
func fingerprintFields(fields any) string {
	m, ok := fields.(map[string]any)
	if !ok {
		// Try slice of maps (sometimes used for complex fields)
		if slice, ok := fields.([]any); ok {
			var parts []string
			for _, item := range slice {
				parts = append(parts, fingerprintFields(item))
			}
			return "[" + strings.Join(parts, ",") + "]"
		}
		return stringify(fields)
	}

	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		if b.Len() > 0 {
			b.WriteRune(',')
		}
		b.WriteString(k)
		b.WriteRune(':')
		b.WriteString(stringify(m[k]))
	}
	return "{" + b.String() + "}"
}

func scanAllCFs(client *arr.ArrClient, inst core.Instance, action string, keep []string) (*CleanupScanResult, error) {
	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}

	// Build case-sensitive keep set. CF names match case-sensitively across
	// the sync engine (see sync.go existingByName lookup), so a Keep List
	// entry "PCOK" only protects the CF named exactly "PCOK", not "pcok".
	keepSet := make(map[string]bool, len(keep))
	for _, name := range keep {
		keepSet[strings.TrimSpace(name)] = true
	}

	items := make([]CleanupItem, 0, len(cfs))
	for _, cf := range cfs {
		if keepSet[cf.Name] {
			continue
		}
		items = append(items, CleanupItem{
			ID:   cf.ID,
			Name: cf.Name,
		})
	}

	return &CleanupScanResult{
		Action:        action,
		InstanceID:    inst.ID,
		Instance: inst.Name,
		TotalCount:    len(cfs),
		AffectCount:   len(items),
		Items:         items,
	}, nil
}

func scanUnsyncedScores(app *core.App, client *arr.ArrClient, inst core.Instance) (*CleanupScanResult, error) {
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
	importedProfiles := app.Profiles.List(inst.Type)
	for _, ip := range importedProfiles {
		for trashID := range ip.FormatItems {
			if comment, ok := ip.FormatComments[trashID]; ok {
				syncedCFNames[comment] = true
			}
		}
	}
	// Also check TRaSH profiles and all synced CFs from sync history
	cfg := app.Config.Get()
	ad := app.Trash.GetAppData(inst.Type)
	for _, sh := range cfg.SyncHistory {
		if sh.InstanceID == inst.ID {
			// Resolve standard TRaSH profile CFs
			if ad != nil {
				resolved, _ := core.ResolveProfileCFs(ad, sh.ProfileTrashID)
				for _, rcf := range resolved {
					syncedCFNames[rcf.Name] = true
				}
			}
			// Include ALL CFs from sync history (covers extra CFs, custom CFs, score overrides)
			if ad != nil {
				for _, trashID := range sh.SyncedCFs {
					if cf, ok := ad.CustomFormats[trashID]; ok {
						syncedCFNames[cf.Name] = true
					}
				}
			}
		}
	}
	// Include custom CFs synced to this instance
	customCFs := app.CustomCFs.List(inst.Type)
	for _, ccf := range customCFs {
		syncedCFNames[ccf.Name] = true
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
				if cfName == "" || syncedCFNames[cfName] {
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
		Action:        "reset-unsynced-scores",
		InstanceID:    inst.ID,
		Instance: inst.Name,
		TotalCount:    len(cfs),
		AffectCount:   len(items),
		Items:         items,
	}, nil
}

func scanOrphanedScores(client *arr.ArrClient, inst core.Instance) (*CleanupScanResult, error) {
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
		Action:        "orphaned-scores",
		InstanceID:    inst.ID,
		Instance: inst.Name,
		TotalCount:    len(cfs),
		AffectCount:   len(items),
		Items:         items,
	}, nil
}

// --- Apply helpers ---

func applyDeleteCFs(client *arr.ArrClient, ids []int) (int, error) {
	deleted := 0
	var errs []error
	for _, id := range ids {
		if err := client.DeleteCustomFormat(id); err != nil {
			log.Printf("CLEANUP: Failed to delete CF %d: %v", id, err)
			errs = append(errs, err)
			continue
		}
		deleted++
	}
	return deleted, errors.Join(errs...)
}

func applyResetScores(client *arr.ArrClient, cfIDs []int) (int, error) {
	profiles, err := client.ListProfiles()
	if err != nil {
		return 0, err
	}

	resetSet := make(map[int]bool)
	for _, id := range cfIDs {
		resetSet[id] = true
	}

	resetCount := 0
	var errs []error
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
				errs = append(errs, err)
			}
		}
	}

	return resetCount, errors.Join(errs...)
}

// scanUnusedByClonarr finds CFs in the Arr instance that aren't managed by
// any clonarr sync rule.
//
// "Managed by clonarr" means the CF's name appears in any of:
//   - A sync rule's selectedCFs (covers both group-enabled CFs and Override
//     extras — frontend pushes both into selectedCFs at sync time)
//   - A sync rule's scoreOverrides keys (a score override on a CF is intent
//     even when score is zero)
//   - A TRaSH-source rule's TRaSH profile intrinsic CFs (formatItems
//     resolved from TRaSH data — these are required by the profile structure)
//   - An imported/builder profile's CFs (when the rule's profileSource is
//     "imported")
//   - The user's keep list
//
// CFs with includeCustomFormatWhenRenaming=true are NO LONGER auto-skipped.
// They appear in results with RenamingFlag=true; the frontend renders a badge
// and explains the implication (only meaningful when the user's naming format
// actually uses {Custom Formats}, which we report via NamingUsesCustomFormats).
// Auto-skipping was over-defensive — it hid TRaSH streaming/language CFs that
// remain after profile deletion, exactly the cleanup-after-experimentation
// case this scan is meant to catch.
//
// Hard caveat: this scan considers CFs that exist in Arr but were added
// outside of clonarr (Arr UI directly, Recyclarr, Notifiarr, etc.) as
// "unused". The frontend warns about this; the backend trusts the user's
// selections from the preview list.
func scanUnusedByClonarr(app *core.App, client *arr.ArrClient, inst core.Instance, keep []string) (*CleanupScanResult, error) {
	// Hard refusal when TRaSH guide data isn't loaded for this app type.
	// Without it, every TRaSH ID in selectedCFs/scoreOverrides resolves to
	// "" → managedNames misses every TRaSH-derived CF → the user is shown
	// "delete all your TRaSH CFs" as candidates. That's data-loss-grade
	// false positive risk, so refuse the scan with a clear remediation.
	ad := app.Trash.GetAppData(inst.Type)
	if ad == nil {
		return nil, fmt.Errorf("TRaSH guide data is not loaded for %s — Clonarr cannot determine what's managed without it. Check Settings → TRaSH Guides and ensure the repository pull has completed successfully", inst.Type)
	}

	cfs, err := client.ListCustomFormats()
	if err != nil {
		return nil, err
	}

	// Keep list — case-insensitive name protection
	keepSet := make(map[string]bool, len(keep))
	for _, k := range keep {
		k = strings.TrimSpace(k)
		if k != "" {
			keepSet[k] = true
		}
	}

	// Build the managed-name set from clonarr's sync rules + imported profiles.
	managedNames := make(map[string]bool)
	cfg := app.Config.Get()

	// Resolve a CF ID (TRaSH hex string or "custom:<hex>") to its display name
	// via the TRaSH appdata or clonarr's custom-CF store. Returns "" when the
	// ID is unresolvable — those refs are silently skipped, mirroring the
	// frontend's resolveCFName fallback behavior.
	customCFs := app.CustomCFs.List(inst.Type)
	customByID := make(map[string]string, len(customCFs))
	for _, ccf := range customCFs {
		customByID[ccf.ID] = ccf.Name
	}
	resolveName := func(cfID string) string {
		if strings.HasPrefix(cfID, "custom:") {
			return customByID[cfID]
		}
		// ad is guaranteed non-nil here — refused above when not loaded
		if cf, ok := ad.CustomFormats[cfID]; ok {
			return cf.Name
		}
		return ""
	}

	for _, rule := range cfg.AutoSync.Rules {
		if rule.InstanceID != inst.ID {
			continue
		}
		// User selections — Override extras AND group-enabled CFs both land here
		for _, cfID := range rule.SelectedCFs {
			if name := resolveName(cfID); name != "" {
				managedNames[name] = true
			}
		}
		// Score overrides — a score (even 0) is explicit intent
		for cfID := range rule.ScoreOverrides {
			if name := resolveName(cfID); name != "" {
				managedNames[name] = true
			}
		}
		// TRaSH-source rule: include the TRaSH profile's intrinsic CFs
		if rule.ProfileSource == "trash" && rule.TrashProfileID != "" {
			resolved, _ := core.ResolveProfileCFs(ad, rule.TrashProfileID)
			for _, rcf := range resolved {
				managedNames[rcf.Name] = true
			}
		}
		// Imported/builder rule: include the imported profile's CFs
		if rule.ProfileSource == "imported" && rule.ImportedProfileID != "" {
			if ip, ok := app.Profiles.Get(rule.ImportedProfileID); ok {
				for trashID := range ip.FormatItems {
					if comment, ok := ip.FormatComments[trashID]; ok && comment != "" {
						managedNames[comment] = true
					} else if cf, ok := ad.CustomFormats[trashID]; ok {
						managedNames[cf.Name] = true
					}
				}
			}
		}
	}

	// Belt-and-suspenders: also include CFs from sync history. This protects
	// against TRaSH-side schema drift — if a TRaSH profile referenced by a
	// rule no longer resolves cleanly (trash_id renamed/removed upstream),
	// ResolveProfileCFs returns empty and the rule's intrinsic required CFs
	// would be missing from managedNames. The sync history captures what
	// clonarr actually pushed to Arr the last time this rule synced, so it's
	// authoritative for "what clonarr put there". Only the latest entry
	// per (instance, arrProfileId) matters — older entries may include CFs
	// the user has since removed via rule edits.
	latestPerProfile := make(map[int]int) // arrProfileId → index in cfg.SyncHistory
	for i, sh := range cfg.SyncHistory {
		if sh.InstanceID != inst.ID {
			continue
		}
		if prev, has := latestPerProfile[sh.ArrProfileID]; !has || sh.LastSync > cfg.SyncHistory[prev].LastSync {
			latestPerProfile[sh.ArrProfileID] = i
		}
	}
	for _, idx := range latestPerProfile {
		for _, cfID := range cfg.SyncHistory[idx].SyncedCFs {
			if name := resolveName(cfID); name != "" {
				managedNames[name] = true
			}
		}
	}

	// Probe the instance's naming format so the frontend can show context
	// for renaming-flagged CFs. Soft-fail: if the probe errors, we just
	// don't render the info box — scan results are still correct.
	namingUsesCFs := false
	if naming, err := client.GetNaming(); err == nil {
		namingUsesCFs = namingFormatUsesCustomFormats(naming, inst.Type)
	}

	// Build a map of CF ID → list of Arr profile names where the CF has a
	// non-zero score (i.e. actively influences release decisions in that
	// profile). Score-zero entries are omitted because they're inert
	// padding — every Arr profile contains every CF at score 0 by default.
	// Soft-fail: empty map if the profile list can't be fetched.
	profileUsage := make(map[int][]string)
	if profiles, err := client.ListProfiles(); err == nil {
		for _, p := range profiles {
			for _, fi := range p.FormatItems {
				if fi.Score != 0 {
					profileUsage[fi.Format] = append(profileUsage[fi.Format], p.Name)
				}
			}
		}
	}

	// Walk the Arr CF list once: split into unmanaged (Items — delete
	// candidates) and managed (ManagedItems — display only). Keep-list
	// CFs are skipped from both buckets — they're explicit user-protected.
	var items []CleanupItem
	var managedItems []ManagedCFRef
	for _, cf := range cfs {
		if keepSet[cf.Name] {
			continue
		}
		if managedNames[cf.Name] {
			managedItems = append(managedItems, ManagedCFRef{
				ID:             cf.ID,
				Name:           cf.Name,
				UsedInProfiles: profileUsage[cf.ID],
				RenamingFlag:   cf.IncludeCustomFormatWhenRenaming,
			})
			continue
		}
		items = append(items, CleanupItem{
			ID:           cf.ID,
			Name:         cf.Name,
			RenamingFlag: cf.IncludeCustomFormatWhenRenaming,
		})
	}

	return &CleanupScanResult{
		Action:                  "unused-by-clonarr",
		InstanceID:              inst.ID,
		Instance:                inst.Name,
		TotalCount:              len(cfs),
		AffectCount:             len(items),
		Items:                   items,
		ManagedItems:            managedItems,
		NamingUsesCustomFormats: namingUsesCFs,
	}, nil
}

// namingFormatUsesCustomFormats reports whether the instance's primary file-
// naming format string references the Custom Formats token in any of its
// common variations:
//
//	{Custom Formats}            // bare token
//	{[Custom Formats]}          // TRaSH-recommended (literal brackets)
//	{(Custom Formats)}          // parenthesis variant
//	{Custom Formats:30}         // truncated variant
//
// Substring match on "Custom Formats" reliably catches all of these — that
// exact phrase only appears as a Radarr/Sonarr token, not in any other
// part of a naming format. Only the standard format is checked (>99% of
// setups); exotic anime/daily formats can be added if a real case appears.
// Returns false silently when the expected key is absent.
func namingFormatUsesCustomFormats(naming arr.ArrNamingConfig, instType string) bool {
	key := "standardMovieFormat"
	if instType != "radarr" {
		key = "standardEpisodeFormat"
	}
	v, ok := naming[key]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.Contains(s, "Custom Formats")
}
