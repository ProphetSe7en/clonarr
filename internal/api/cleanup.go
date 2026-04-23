package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"errors"
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
}

type CleanupItem struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Detail   string   `json:"detail,omitempty"`
	Profiles []string `json:"profiles,omitempty"`
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
				syncedCFNames[strings.ToLower(comment)] = true
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
	customCFs := app.CustomCFs.List(inst.Type)
	for _, ccf := range customCFs {
		syncedCFNames[strings.ToLower(ccf.Name)] = true
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
