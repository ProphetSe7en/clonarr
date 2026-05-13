package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"time"

	"clonarr/internal/arr"
	"clonarr/internal/core"
)

// autoSyncRuleEquivalent reports whether two AutoSyncRule values carry
// identical user-facing state. Server-managed fields (LastSyncCommit,
// LastSyncTime, LastSyncError, UpdatedAt, PriorAvailableGroups, OrphanedAt)
// are ignored — they live outside the editor's reach and would otherwise
// produce spurious "changed" verdicts on every PUT. Used by
// handleUpdateAutoSyncRule on the ?save_only=1 path to avoid bumping
// UpdatedAt (and triggering the "● unsynced" indicator) when the user
// clicked Save without making any edits.
func autoSyncRuleEquivalent(a, b core.AutoSyncRule) bool {
	clear := func(r core.AutoSyncRule) core.AutoSyncRule {
		r.LastSyncCommit = ""
		r.LastSyncTime = ""
		r.LastSyncError = ""
		r.UpdatedAt = ""
		r.PriorAvailableGroups = nil
		r.OrphanedAt = ""
		return r
	}
	return reflect.DeepEqual(clear(a), clear(b))
}

// handleGetAutoSyncSettings returns the minimal auto-sync config (notification
// agents are served by handleListNotificationAgents).
func (s *Server) handleGetAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	writeJSON(w, map[string]any{
		"enabled": cfg.AutoSync.Enabled,
		"paused":  cfg.AutoSync.Paused,
	})
}

// handleSaveAutoSyncSettings updates the top-level enabled and paused flags.
// Notification agents are managed via /api/auto-sync/notification-agents.
func (s *Server) handleSaveAutoSyncSettings(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Enabled *bool `json:"enabled,omitempty"`
		Paused  *bool `json:"paused,omitempty"`
	}](w, r, 4096)
	if !ok {
		return
	}
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		if req.Enabled != nil {
			cfg.AutoSync.Enabled = *req.Enabled
		}
		if req.Paused != nil {
			cfg.AutoSync.Paused = *req.Paused
		}
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
		InstanceName   string `json:"instanceName"`
		InstanceType   string `json:"instanceType"`
		OptionalCount  int    `json:"optionalCount"`
		OverridesCount int    `json:"overridesCount"`
	}

	// Cache per (instanceType, trashProfileID) to avoid duplicate
	// ProfileDetailData calls when many rules target the same profile.
	type detailKey struct{ appType, trashID string }
	detailCache := make(map[detailKey]*core.ProfileDetailResult)

	rules := make([]ruleResponse, 0, len(cfg.AutoSync.Rules))
	for _, rule := range cfg.AutoSync.Rules {
		rr := ruleResponse{AutoSyncRule: rule}
		if inst, ok := s.Core.Config.GetInstance(rule.InstanceID); ok {
			rr.InstanceName = inst.Name
			rr.InstanceType = inst.Type
			// Lazy-compute counts — mirrors what the Profile Detail editor
			// shows in its status bar so the sync-rules-list badge always
			// agrees with the editor for the same rule. Skipped for
			// imported-profile rules (no TRaSH profile to derive groups
			// from) and orphaned rules (would clutter the listing).
			if rule.TrashProfileID != "" && rule.OrphanedAt == "" {
				key := detailKey{inst.Type, rule.TrashProfileID}
				detail, ok := detailCache[key]
				if !ok {
					ad := s.Core.Trash.GetAppData(inst.Type)
					detail = core.ProfileDetailData(ad, rule.TrashProfileID)
					detailCache[key] = detail
				}
				rr.OverridesCount, rr.OptionalCount = core.ComputeRuleCounts(rule, detail)
			}
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
//
// Takes the per-instance sync mutex before mutating so a concurrent
// /api/sync/apply on the same instance can't race the rule update. Without
// this, the Edit-&-Sync flow's post-apply PUT could land between two Sync
// All operations and leave rule.SelectedCFs out of step with the body the
// just-completed Sync All persisted — next Sync All would then re-apply a
// stale set and zero customs the user just preserved.
func (s *Server) handleUpdateAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var rule core.AutoSyncRule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	rule.ID = id

	// Gate on the same per-instance sync mutex /api/sync/apply uses. The
	// rule's InstanceID comes from the request body (frontend echoes the
	// existing rule's instanceId verbatim — there is no UI to move a rule
	// between instances). If a sync is in flight, return 409 so the client
	// can retry; this matches the contract of /api/sync/apply.
	if rule.InstanceID != "" {
		mu := s.Core.GetSyncMutex(rule.InstanceID)
		if !mu.TryLock() {
			writeError(w, 409, "Sync in progress for this instance — try again in a moment")
			return
		}
		defer mu.Unlock()
	}

	// save_only=1 marks this PUT as a "save without sync" action from the
	// Profile Detail editor. It bumps UpdatedAt so the Profiles tab can show
	// "● unsynced" on the rule card — but only if the body actually carries
	// a change (autoSyncRuleEquivalent guards against open-and-Save no-ops
	// and toggle-on-then-off net-zero edits). Other PUT callers (post-sync
	// echo from startApply, Compare-flow keepArrCFIDs update, enabled-toggle)
	// omit the param and the server preserves UpdatedAt from disk — the apply
	// path already equalized UpdatedAt with LastSyncTime, so the indicator
	// stays off.
	saveOnly := r.URL.Query().Get("save_only") == "1"

	found := false
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == id {
				existing := cfg.AutoSync.Rules[i]
				rule.LastSyncCommit = existing.LastSyncCommit
				rule.LastSyncTime = existing.LastSyncTime
				switch {
				case saveOnly && !autoSyncRuleEquivalent(rule, existing):
					rule.UpdatedAt = time.Now().Format(time.RFC3339)
				default:
					// No real change OR non-save-only caller — preserve
					// the disk's UpdatedAt so the indicator state survives.
					rule.UpdatedAt = existing.UpdatedAt
				}
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

// handleRestoreAutoSyncRule re-creates a profile in Arr from the saved sync
// intent on an orphaned rule. The rule gets a fresh ArrProfileID and its
// OrphanedAt is cleared. A name collision against existing Arr profiles or
// other active clonarr rules returns 409 with the conflicting names so the
// frontend can prompt for an override.
//
// Body (optional):
//
//	{ "newName": "Custom Replacement Name" }
//
// When newName is omitted, the rule's last synced ProfileName is used.
//
// Status codes:
//
//	200 — restored, returns { arrProfileId, arrProfileName }
//	404 — rule not found
//	409 — name collision (returns { error, conflictWith: "arr"|"clonarr" })
//	412 — rule is not orphaned (only orphaned rules support restore)
//	502 — Arr unreachable or profile-create failed
func (s *Server) handleRestoreAutoSyncRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	req := struct {
		NewName string `json:"newName,omitempty"`
	}{}
	// Body is optional — accept empty body silently.
	if r.ContentLength > 0 {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	}
	req.NewName = strings.TrimSpace(req.NewName)

	cfg := s.Core.Config.Get()
	var rule *core.AutoSyncRule
	for i := range cfg.AutoSync.Rules {
		if cfg.AutoSync.Rules[i].ID == id {
			rule = &cfg.AutoSync.Rules[i]
			break
		}
	}
	if rule == nil {
		writeError(w, 404, "Rule not found")
		return
	}
	// Capture the now-orphaned ArrProfileID before any mutation so the
	// orphaned history entries can be cleaned up after the restore lands.
	oldArrProfileID := rule.ArrProfileID
	if rule.OrphanedAt == "" {
		writeError(w, 412, "Rule is not orphaned — restore only applies to rules whose target profile was deleted in Arr")
		return
	}

	inst, ok := s.Core.Config.GetInstance(rule.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found for this rule")
		return
	}

	// Pull the latest history entry for this orphaned rule. That snapshot
	// has the full intent (CFs, scores, qualities, overrides, original
	// name) we'll re-push to Arr.
	hist := s.Core.Config.GetLatestSyncEntry(inst.ID, rule.ArrProfileID)
	if hist == nil {
		writeError(w, 412, "No sync history found for this rule — nothing to restore from")
		return
	}

	targetName := hist.ProfileName
	if rule.ProfileSource == "trash" && hist.ArrProfileName != "" {
		// Prefer the user's actual Arr-side name from the last sync (covers
		// the case where they renamed it in Arr after the initial sync).
		targetName = hist.ArrProfileName
	}
	if req.NewName != "" {
		targetName = req.NewName
	}

	// Collision check 1: Arr profile with this name already exists.
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	existing, err := client.ListProfiles()
	if err != nil {
		writeError(w, 502, "Failed to query Arr profiles: "+err.Error())
		return
	}
	for _, p := range existing {
		if strings.EqualFold(p.Name, targetName) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":        fmt.Sprintf("A profile named %q already exists in %s", p.Name, inst.Name),
				"conflictWith": "arr",
				"existingName": p.Name,
				"suggested":    targetName + " (Restored)",
			})
			return
		}
	}

	// Collision check 2: another active (non-orphaned) clonarr rule on the
	// same instance points to a profile with this name. Catches the rare
	// case where a user has two rules synced to differently-named Arr
	// profiles, both got orphaned, and the user is restoring the second
	// one with a name that overlaps the first's restored output.
	for _, other := range cfg.AutoSync.Rules {
		if other.ID == rule.ID || other.OrphanedAt != "" || other.InstanceID != inst.ID {
			continue
		}
		otherHist := s.Core.Config.GetLatestSyncEntry(inst.ID, other.ArrProfileID)
		if otherHist == nil {
			continue
		}
		otherName := otherHist.ArrProfileName
		if otherName == "" {
			otherName = otherHist.ProfileName
		}
		if strings.EqualFold(otherName, targetName) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":        fmt.Sprintf("Another active sync rule already manages a profile named %q on %s", otherName, inst.Name),
				"conflictWith": "clonarr",
				"existingName": otherName,
				"suggested":    targetName + " (Restored)",
			})
			return
		}
	}

	// Acquire the per-instance sync mutex so we don't race a scheduled or
	// manual sync running concurrently for the same Arr instance.
	mu := s.Core.GetSyncMutex(inst.ID)
	if !mu.TryLock() {
		writeError(w, 409, "Sync already in progress for this instance — try again in a moment")
		return
	}
	defer mu.Unlock()

	// Build a SyncRequest that mirrors the rule's saved intent. Setting
	// ArrProfileID=0 forces ExecuteSyncPlan into create-profile mode.
	syncReq := core.SyncRequest{
		InstanceID:        inst.ID,
		ProfileTrashID:    rule.TrashProfileID,
		ImportedProfileID: rule.ImportedProfileID,
		ArrProfileID:      0,
		ProfileName:       targetName,
		SelectedCFs:       append([]string(nil), rule.SelectedCFs...),
		ScoreOverrides:    rule.ScoreOverrides,
		QualityOverrides:  rule.QualityOverrides,
		QualityStructure:  rule.QualityStructure,
		Overrides:         rule.Overrides,
		Behavior:          rule.Behavior,
	}

	ad := s.Core.Trash.GetAppData(inst.Type)
	var imported *core.ImportedProfile
	if syncReq.ImportedProfileID != "" {
		p, ok := s.Core.Profiles.Get(syncReq.ImportedProfileID)
		if !ok {
			writeError(w, 404, "Imported profile referenced by rule no longer exists")
			return
		}
		imported = &p
	}
	customCFs := s.Core.CustomCFs.List(inst.Type)

	op := s.Core.DebugLog.BeginOp(core.OpSync, core.SourceManualResume, fmt.Sprintf("rule=%s instance=%s restore", rule.ID, inst.Name))
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	plan, err := core.BuildSyncPlan(ad, inst, syncReq, imported, customCFs, nil, s.Core.HTTPClient, op)
	if err != nil {
		endResult = fmt.Sprintf("error: plan failed: %v", err)
		writeError(w, 500, "Failed to build restore plan: "+err.Error())
		return
	}
	behavior := core.ResolveSyncBehavior(syncReq.Behavior)
	result, err := core.ExecuteSyncPlan(ad, inst, syncReq, plan, imported, customCFs, behavior, s.Core.HTTPClient, op)
	if err != nil {
		endResult = fmt.Sprintf("error: execute failed: %v", err)
		writeError(w, 502, "Failed to recreate profile in Arr: "+err.Error())
		return
	}

	// Restore re-creates the Arr profile from the rule's stored intent —
	// dangling "custom:" refs detected during plan-build are cleaned here
	// on the same success boundary so the restored rule starts clean. No-op
	// when nothing was dangling.
	if removed := s.Core.CleanupDanglingCustomCFsOnRule(id, plan.DanglingCustomCFs); len(removed) > 0 {
		log.Printf("Restore: rule %s — cleaned %d dangling custom-CF reference(s)", id, len(removed))
		s.Core.DebugLog.Logf(core.LogAutoSync, "Restore: rule %s removed %d dangling custom CFs", id, len(removed))
	}
	if !result.ProfileCreated || result.ArrProfileID == 0 {
		endResult = "error: profile creation did not succeed"
		writeError(w, 502, "Profile creation in Arr did not succeed")
		return
	}
	if result.ArrProfileName != "" {
		endResult = fmt.Sprintf("ok | restored %q (#%d)", result.ArrProfileName, result.ArrProfileID)
	} else {
		endResult = fmt.Sprintf("ok | restored Arr profile #%d", result.ArrProfileID)
	}

	// Persist: clear OrphanedAt, update ArrProfileID; mirror onto history.
	s.Core.Config.Update(func(cfg *core.Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == id {
				now := time.Now().Format(time.RFC3339)
				cfg.AutoSync.Rules[i].ArrProfileID = result.ArrProfileID
				cfg.AutoSync.Rules[i].OrphanedAt = ""
				cfg.AutoSync.Rules[i].LastSyncTime = now
				cfg.AutoSync.Rules[i].UpdatedAt = now
				cfg.AutoSync.Rules[i].LastSyncError = ""
				break
			}
		}
		// Existing history entries for the old ArrProfileID stay attached
		// to that ID (they describe past state). Append a fresh entry
		// reflecting the restored profile's new ID.
	})

	// Append a sync history entry for the restoration so the user sees
	// it in the History tab as the most recent action.
	allCFIDs := make([]string, 0, len(plan.CFActions))
	for _, a := range plan.CFActions {
		allCFIDs = append(allCFIDs, a.TrashID)
	}
	selectedCFMap := make(map[string]bool, len(syncReq.SelectedCFs))
	for _, cfID := range syncReq.SelectedCFs {
		selectedCFMap[cfID] = true
	}
	now := time.Now().Format(time.RFC3339)
	entry := core.SyncHistoryEntry{
		InstanceID:        inst.ID,
		InstanceType:      inst.Type,
		ProfileTrashID:    rule.TrashProfileID,
		ImportedProfileID: rule.ImportedProfileID,
		ProfileName:       targetName,
		ArrProfileID:      result.ArrProfileID,
		ArrProfileName:    result.ArrProfileName,
		SyncedCFs:         allCFIDs,
		SelectedCFs:       selectedCFMap,
		ScoreOverrides:    syncReq.ScoreOverrides,
		QualityOverrides:  syncReq.QualityOverrides,
		QualityStructure:  syncReq.QualityStructure,
		Overrides:         syncReq.Overrides,
		Behavior:          syncReq.Behavior,
		CFsCreated:        result.CFsCreated,
		CFsUpdated:        result.CFsUpdated,
		ScoresUpdated:     result.ScoresUpdated,
		LastSync:          now,
		AppliedAt:         now,
		Changes: &core.SyncChanges{
			SettingsDetails: []string{fmt.Sprintf("Restored profile (created new ArrProfileID %d)", result.ArrProfileID)},
		},
	}
	if err := s.Core.Config.UpsertSyncHistory(entry); err != nil {
		log.Printf("Restore: failed to persist sync history: %v", err)
	}

	// Clean up the orphaned history entries that pointed at the deleted
	// Arr profile. They contributed their saved intent to this very
	// restore and are no longer informational — the new entry above is
	// the live record going forward. Non-fatal: a failure here just
	// leaves the old rows in place, the restore itself already succeeded.
	if err := s.Core.Config.DeleteSyncHistory(inst.ID, oldArrProfileID); err != nil {
		log.Printf("Restore: cleanup of pre-restore history (arrProfileId=%d) failed (non-fatal): %v", oldArrProfileID, err)
	}

	s.Core.DebugLog.Logf(core.LogSync, "Restore: rule %s → %s | old arrProfileId=%d → new arrProfileId=%d (%s) | %d CFs created, %d updated, %d scores",
		id, inst.Name, oldArrProfileID, result.ArrProfileID, result.ArrProfileName,
		result.CFsCreated, result.CFsUpdated, result.ScoresUpdated)

	writeJSON(w, map[string]any{
		"arrProfileId":   result.ArrProfileID,
		"arrProfileName": result.ArrProfileName,
		"cfsCreated":     result.CFsCreated,
		"cfsUpdated":     result.CFsUpdated,
		"scoresUpdated":  result.ScoresUpdated,
	})
}

