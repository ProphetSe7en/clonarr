package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"

	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// --- Sync ---

// qualityOverrideSummary describes whether the request carries a Quality
// override and which form. Returned values:
//   "none"             — no Quality override sent
//   "structure(N)"     — full qualityStructure override (N items, post-frontend
//                        identity-filter so reaching here means user actually
//                        diverged from profile defaults)
//   "flat(N)"          — legacy qualityOverrides map (N entries)
//
// Used in dry-run / apply log lines to make the per-Quality-channel state
// explicit, so testers can verify e.g. that opening the Edit modal without
// changes produces "quality: none" (qualityStructureMatchesDefaults filter
// on the frontend prevented a phantom override).
func qualityOverrideSummary(req core.SyncRequest) string {
	if len(req.QualityStructure) > 0 {
		return fmt.Sprintf("structure(%d)", len(req.QualityStructure))
	}
	if len(req.QualityOverrides) > 0 {
		return fmt.Sprintf("flat(%d)", len(req.QualityOverrides))
	}
	return "none"
}

func (s *Server) handleDryRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var req core.SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	inst, ok := s.Core.Config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	ad := s.Core.Trash.GetAppData(inst.Type)
	var imported *core.ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := s.Core.Profiles.Get(req.ImportedProfileID)
		if !ok {
			writeError(w, 404, "Imported profile not found")
			return
		}
		imported = &p
	}
	// v2.5.8: pre-sync CF expansion is no longer needed — BuildSyncPlan
	// resolves the effective set via ComputeTrashDefaults ∪ SelectedCFs -
	// ExcludedCFs directly from current TRaSH state. We still forward the
	// rule's ExcludedCFs into the request when running with ExpandRule so
	// the dry-run preview reflects user opt-outs that the request body
	// didn't supply (Sync All's frontend doesn't always send them).
	if req.ExpandRule && ad != nil && req.ArrProfileID > 0 && req.ProfileTrashID != "" {
		for _, r := range s.Core.Config.Get().AutoSync.Rules {
			if r.InstanceID == req.InstanceID && r.ArrProfileID == req.ArrProfileID && r.Enabled && r.OrphanedAt == "" {
				if req.ExcludedCFs == nil {
					req.ExcludedCFs = append([]string(nil), r.ExcludedCFs...)
				}
				break
			}
		}
	}
	customCFs := s.Core.CustomCFs.List(inst.Type)
	lastSyncedCFs := s.Core.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := core.BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, s.Core.HTTPClient, nil)
	if err != nil {
		log.Printf("Dry-run error for %s: %v", inst.Name, err)
		writeError(w, 400, err.Error())
		return
	}

	behavior := core.ResolveSyncBehavior(req.Behavior)
	s.Core.DebugLog.Logf(core.LogSync, "Dry-run: %q → %s | %d selected CFs | overrides: %s | quality: %s | scoreOverrides: %d | behavior: %s/%s/%s",
		plan.ProfileName, inst.Name, len(req.SelectedCFs),
		core.OverrideSummary(req.Overrides),
		qualityOverrideSummary(req),
		len(req.ScoreOverrides),
		behavior.AddMode, behavior.RemoveMode, behavior.ResetMode)
	s.Core.DebugLog.Logf(core.LogSync, "Dry-run result: %d create, %d update, %d unchanged | %d scores to set, %d to zero",
		plan.Summary.CFsToCreate, plan.Summary.CFsToUpdate, plan.Summary.CFsUnchanged,
		plan.Summary.ScoresToSet, plan.Summary.ScoresToZero)

	writeJSON(w, plan)
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	var req core.SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	inst, ok := s.Core.Config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	// Open an operation scope for this sync. Source distinguishes a
	// Profile Builder save (imported profile) from a TRaSH-direct sync
	// so post-mortem reads can grep for one or the other. Frontend-side
	// rollback re-uses this endpoint with historic state — currently
	// indistinguishable from a manual TRaSH-direct sync at the API; if
	// we ever add a request flag, switch the source here.
	source := core.SourceManualTrashRule
	if req.ImportedProfileID != "" {
		source = core.SourceManualBuilder
	}
	// Resolve the Arr profile name from sync history so the op trace
	// reads "instance=Radarr-4K profile='Standard Movies' (#49)" rather
	// than the bare ID — debug logs are user-facing too.
	arrProfileLabel := fmt.Sprintf("arrProfileId=%d", req.ArrProfileID)
	if req.ArrProfileID != 0 {
		if hist := s.Core.Config.GetLatestSyncEntry(req.InstanceID, req.ArrProfileID); hist != nil && hist.ArrProfileName != "" {
			arrProfileLabel = fmt.Sprintf("profile=%q (#%d)", hist.ArrProfileName, req.ArrProfileID)
		}
	}
	op := s.Core.DebugLog.BeginOp(core.OpSync, source, fmt.Sprintf("instance=%s %s", inst.Name, arrProfileLabel))
	// Default end result; reassigned on the success path below so an early
	// return through any error branch records what went wrong.
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	// C5: Only one sync per instance at a time
	mu := s.Core.GetSyncMutex(inst.ID)
	if !mu.TryLock() {
		endResult = "error: sync already in progress"
		writeError(w, 409, "Sync already in progress for this instance")
		return
	}
	defer mu.Unlock()

	// Single snapshot for both plan + execute (C2: prevents data drift between steps)
	ad := s.Core.Trash.GetAppData(inst.Type)
	var imported *core.ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := s.Core.Profiles.Get(req.ImportedProfileID)
		if !ok {
			endResult = "error: imported profile not found"
			writeError(w, 404, "Imported profile not found")
			return
		}
		imported = &p
	}
	// v2.5.8: pre-sync CF expansion is no longer needed. BuildSyncPlan reads
	// current TRaSH state via ComputeTrashDefaults at sync time, so CFs that
	// TRaSH moved into a new default-on group are auto-included without any
	// rule mutation. We still forward the rule's ExcludedCFs into the
	// request body if the caller didn't supply them (Sync All's quick-sync
	// frontend path doesn't always send override fields), so opt-outs are
	// honored regardless of whether the caller round-tripped them.
	if req.ExpandRule && ad != nil && req.ArrProfileID > 0 && req.ProfileTrashID != "" {
		for _, r := range s.Core.Config.Get().AutoSync.Rules {
			if r.InstanceID == req.InstanceID && r.ArrProfileID == req.ArrProfileID && r.Enabled && r.OrphanedAt == "" {
				if req.ExcludedCFs == nil {
					req.ExcludedCFs = append([]string(nil), r.ExcludedCFs...)
				}
				break
			}
		}
	}
	customCFs := s.Core.CustomCFs.List(inst.Type)
	lastSyncedCFs := s.Core.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	behavior := core.ResolveSyncBehavior(req.Behavior)
	plan, err := core.BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, s.Core.HTTPClient, op)
	if err != nil {
		log.Printf("Apply plan error for %s: %v", inst.Name, err)
		s.Core.DebugLog.Logf(core.LogError, "Apply plan error for %s: %v", inst.Name, err)
		endResult = fmt.Sprintf("error: plan failed: %v", err)
		writeError(w, 500, "Failed to build sync plan")
		return
	}

	result, err := core.ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, behavior, s.Core.HTTPClient, op)
	if err != nil {
		log.Printf("Apply exec error for %s: %v", inst.Name, err)
		s.Core.DebugLog.Logf(core.LogError, "Apply exec error for %s: %v", inst.Name, err)
		endResult = fmt.Sprintf("error: execute failed: %v", err)
		writeError(w, 500, "Failed to execute sync")
		return
	}

	// Clean dangling "custom:" refs from the matching auto-sync rule (if any)
	// after a successful apply — mirrors runAutoSyncRule's post-success
	// cleanup. No rule = no-op (Save & Sync from a fresh profile-detail
	// editor without a stored rule). TRaSH-id orphans are left alone.
	if len(plan.DanglingCustomCFs) > 0 {
		for _, r := range s.Core.Config.Get().AutoSync.Rules {
			if r.InstanceID == req.InstanceID && r.ArrProfileID == req.ArrProfileID {
				if removed := s.Core.CleanupDanglingCustomCFsOnRule(r.ID, plan.DanglingCustomCFs); len(removed) > 0 {
					log.Printf("Apply: rule %s — cleaned %d dangling custom-CF reference(s) after sync", r.ID, len(removed))
					s.Core.DebugLog.Logf(core.LogSync, "Apply: rule %s removed %d dangling custom CFs", r.ID, len(removed))
				}
				break
			}
		}
	}

	// Apply log line: prefer the Arr profile name (from plan/result)
	// over the raw ID. result.ArrProfileName is set in update mode;
	// in create mode the new profile isn't named yet so we fall back
	// to the trash profile name with the new ID in parens.
	arrName := result.ArrProfileName
	if arrName == "" {
		arrName = plan.ArrProfileName
	}
	applyTarget := fmt.Sprintf("Arr profile #%d", req.ArrProfileID)
	if arrName != "" {
		applyTarget = fmt.Sprintf("%q (#%d)", arrName, req.ArrProfileID)
	}
	s.Core.DebugLog.Logf(core.LogSync, "Apply: %q → %s | %s | mode=%s | overrides: %s | quality: %s | scoreOverrides: %d | %d created, %d updated, %d scores | %d errors",
		plan.ProfileName, inst.Name, applyTarget, func() string {
			if req.ArrProfileID == 0 {
				return "create"
			}
			return "update"
		}(),
		core.OverrideSummary(req.Overrides),
		qualityOverrideSummary(req),
		len(req.ScoreOverrides),
		result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, len(result.Errors))
	endResult = fmt.Sprintf("ok | %d created, %d updated, %d scores, %d errors", result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, len(result.Errors))
	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			s.Core.DebugLog.Logf(core.LogError, "Apply error: %s", e)
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
	// CF-set diff against previous entry — only report set transitions for
	// CFs whose score doesn't change either side of zero, since the score
	// engine's "Score set: X (Y)" / "Score cleared: X (was Y)" lines
	// already cover any score-bearing activation/deactivation. Reporting
	// both produces the duplicate "Activated: X" + "Score set: X" noise
	// the user flagged on PR-2733 history.
	cfSetDetails := []string{}
	prevEntry := s.Core.Config.GetLatestSyncEntry(inst.ID, req.ArrProfileID)
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
		// Score the engine actually wrote for each CF in this sync — used
		// to skip CFs that the score-detail block will already mention.
		newScoreByName := make(map[string]int)
		for _, sa := range plan.ScoreActions {
			newScoreByName[sa.CFName] = sa.NewScore
		}
		for _, tid := range allCFIDs {
			if !prevSet[tid] {
				name := resolveName(tid)
				if score, ok := newScoreByName[name]; ok && score != 0 {
					continue // score detail will say "Score set: X (Y)"
				}
				cfSetDetails = append(cfSetDetails, "Now in profile: "+name)
			}
		}
		for _, tid := range prevEntry.SyncedCFs {
			if !newSet[tid] {
				name := resolveName(tid)
				if score, ok := newScoreByName[name]; ok && score == 0 {
					// engine likely emits a "Score cleared: X" detail
					continue
				}
				cfSetDetails = append(cfSetDetails, "No longer in profile: "+name)
			}
		}
	}
	// Merge: cfSetDetails (set diff, zero-score cases) + result.CFDetails (creates/updates)
	allCFDetails := append(cfSetDetails, result.CFDetails...)
	// Prior TRaSH commit for Sync History attribution on Updated CFs.
	// Read the existing rule's LastSyncCommit before it gets overwritten
	// further down. First-time syncs have no prior rule, leaving the
	// commit empty so BuildCFCommitsFromDetails returns nil cleanly.
	// Also check whether the rule had a drift fingerprint pending: if
	// so, this manual API hit is conceptually a drift-apply (user
	// clicked Apply on the drift modal, or auto-mode pushed the saved
	// state back), and the History chip should reflect that instead of
	// the generic "Manual" label.
	priorCommit := ""
	driftPending := false
	for _, r := range s.Core.Config.Get().AutoSync.Rules {
		if r.InstanceID == inst.ID && r.ArrProfileID == req.ArrProfileID {
			priorCommit = r.LastSyncCommit
			if r.WatchState != nil && r.WatchState.LastDriftFingerprint != "" {
				driftPending = true
			}
			break
		}
	}
	currentTrashCommit := s.Core.Trash.CurrentCommit()
	triggerSource := source
	if driftPending {
		// Drift fingerprint is set, but TRaSH has also advanced since
		// the last sync (priorCommit != currentTrashCommit). Stale
		// drift + a new TRaSH commit means the user is most likely
		// here to pull the update, not to reconcile drift. Prefer the
		// TRaSH-update label so the chip matches what actually drove
		// the sync. Genuine drift-only sync (TRaSH unchanged) still
		// labels as drift_apply correctly.
		if priorCommit != "" && currentTrashCommit != "" && priorCommit != currentTrashCommit {
			// TRaSH advanced. Keep the original source (Manual /
			// Builder etc.) so the chip reads as the user's actual
			// click, not a drift apply.
		} else {
			triggerSource = core.SourceDriftApply
		}
	}
	var changes *core.SyncChanges
	if len(allCFDetails) > 0 || len(result.ScoreDetails) > 0 ||
		len(result.QualityDetails) > 0 || len(result.SettingsDetails) > 0 {
		changes = &core.SyncChanges{
			CFDetails:       allCFDetails,
			ScoreDetails:    result.ScoreDetails,
			QualityDetails:  result.QualityDetails,
			SettingsDetails: result.SettingsDetails,
			CFCommits:       core.BuildCFCommitsFromDetails(s.Core.Trash, inst.Type, allCFDetails, result.ScoreDetails, priorCommit, currentTrashCommit),
			CFSpecDiffs:     result.CFSpecDiffs,
		}
	}

	now := time.Now().Format(time.RFC3339)
	entry := core.SyncHistoryEntry{
		InstanceID:        inst.ID,
		InstanceType:      inst.Type,
		ProfileTrashID:    req.ProfileTrashID,
		ImportedProfileID: req.ImportedProfileID,
		ProfileName:       plan.ProfileName,
		ArrProfileID:      req.ArrProfileID,
		ArrProfileName:    plan.ArrProfileName,
		SyncedCFs:         allCFIDs,
		SelectedCFs:       selectedCFMap,
		ExcludedCFs:       append([]string(nil), req.ExcludedCFs...),
		ScoreOverrides:    req.ScoreOverrides,
		QualityOverrides:  req.QualityOverrides,
		QualityStructure:  req.QualityStructure,
		Overrides:         req.Overrides,
		Behavior:          req.Behavior,
		KeepArrCFIDs:      req.KeepArrCFIDs,
		CFsCreated:        result.CFsCreated,
		CFsUpdated:        result.CFsUpdated,
		ScoresUpdated:     result.ScoresUpdated,
		LastSync:          now,
		Changes:           changes,
		TriggerType:       core.TriggerTypeFromSource(triggerSource),
		Categories:        core.DeriveSyncCategories(result),
	}
	// AppliedAt freezes the "when changes landed" timestamp. Only set when
	// the entry carries real changes — baseline / no-op entries leave it
	// blank so UI falls back to LastSync.
	if changes != nil {
		entry.AppliedAt = now
	}
	// Use newly created profile info when available
	if result.ProfileCreated {
		entry.ArrProfileID = result.ArrProfileID
		entry.ArrProfileName = result.ArrProfileName
		// Update auto-sync rule that has arrProfileId=0 (was waiting for profile creation)
		s.Core.Config.Update(func(cfg *core.Config) {
			for i := range cfg.AutoSync.Rules {
				r := &cfg.AutoSync.Rules[i]
				if r.ArrProfileID == 0 && r.InstanceID == req.InstanceID &&
					((r.TrashProfileID != "" && r.TrashProfileID == req.ProfileTrashID) ||
						(r.ImportedProfileID != "" && r.ImportedProfileID == req.ImportedProfileID)) {
					log.Printf("Sync: updating auto-sync rule %s with new Arr profile ID %d", r.ID, result.ArrProfileID)
					s.Core.DebugLog.Logf(core.LogSync, "Auto-sync rule %s updated with new Arr profile ID %d", r.ID, result.ArrProfileID)
					r.ArrProfileID = result.ArrProfileID
					return
				}
			}
		})
	}
	if err := s.Core.Config.UpsertSyncHistory(entry); err != nil {
		log.Printf("Failed to save sync history: %v", err)
		s.Core.DebugLog.Logf(core.LogError, "Failed to save sync history: %v", err)
	}

	// Ensure an auto-sync rule exists for this profile (disabled by default)
	// If a rule exists but source type changed (builder↔TRaSH), update it to match.
	//
	// Skip the rule-update path entirely when the apply produced errors —
	// otherwise a sync that Arr rejected (e.g. unsatisfiable min-score, CF
	// with empty condition name) would persist the failing config and every
	// subsequent auto-sync would re-attempt with the same bad data. Keep
	// the previous rule state instead so the user has to address the errors
	// before progress is locked in. Sync history (saved above) still records
	// the failed attempt for visibility. Profile-creation handles its own
	// rule update earlier in this function on the result.ProfileCreated
	// path; that block stays separate because it's about discovering the
	// new ArrProfileID, not persisting user intent.
	if len(result.Errors) > 0 {
		log.Printf("Sync: skipping rule update for %s — sync had %d error(s); rule keeps previous state", inst.Name, len(result.Errors))
		s.Core.DebugLog.Logf(core.LogSync, "Apply: skipping rule update — %d error(s) returned by Arr; previous rule state preserved", len(result.Errors))
		op.Logf("apply: rule update skipped — %d error(s) returned, previous rule state preserved", len(result.Errors))

		// Auto-disable the rule only when EVERY error is a user-config
		// problem (HTTP 400/409/422). Transient/external errors
		// (5xx, 401/403, ListX fetch failures, raw network errors)
		// keep the rule enabled so the next tick / next manual click
		// can retry — disabling on a server blip would leave the user
		// with a wrongly-disabled rule. We always set LastSyncError
		// for visibility in the UI badge regardless of disable
		// decision. Connection errors return as Go-level err earlier
		// and never reach this path.
		errSummary := strings.Join(result.Errors, " | ")
		if req.ArrProfileID > 0 {
			shouldDisable := core.AllUserConfigErrors(result.Errors)
			s.Core.Config.Update(func(cfg *core.Config) {
				for i := range cfg.AutoSync.Rules {
					if cfg.AutoSync.Rules[i].InstanceID == inst.ID && cfg.AutoSync.Rules[i].ArrProfileID == req.ArrProfileID {
						cfg.AutoSync.Rules[i].LastSyncError = errSummary
						if shouldDisable {
							cfg.AutoSync.Rules[i].Enabled = false
						}
						return
					}
				}
			})
			if shouldDisable {
				op.Logf("apply: rule auto-disabled — every error is user-config (HTTP 400/409/422); error badge will appear in UI; user must address errors and manually re-enable")
			} else {
				op.Logf("apply: rule kept enabled — at least one error is transient/external (5xx, 401/403, network); will retry next tick or next manual click")
			}
		}
		writeJSON(w, result)
		return
	}
	arrID := req.ArrProfileID
	if result.ProfileCreated {
		arrID = result.ArrProfileID
	}
	newSource := "trash"
	if req.ImportedProfileID != "" {
		newSource = "imported"
	}
	s.Core.Config.Update(func(cfg *core.Config) {
		for i, r := range cfg.AutoSync.Rules {
			if r.InstanceID == req.InstanceID && r.ArrProfileID == arrID {
				// Rule exists — update source type and selections if they changed
				if r.ProfileSource != newSource || r.TrashProfileID != req.ProfileTrashID || r.ImportedProfileID != req.ImportedProfileID {
					s.Core.DebugLog.Logf(core.LogSync, "Auto-sync rule %s: updating source %s→%s for Arr profile %d", r.ID, r.ProfileSource, newSource, arrID)
				}
				cfg.AutoSync.Rules[i].ProfileSource = newSource
				cfg.AutoSync.Rules[i].TrashProfileID = req.ProfileTrashID
				cfg.AutoSync.Rules[i].ImportedProfileID = req.ImportedProfileID
				cfg.AutoSync.Rules[i].SelectedCFs = req.SelectedCFs
				cfg.AutoSync.Rules[i].ExcludedCFs = req.ExcludedCFs
				cfg.AutoSync.Rules[i].ScoreOverrides = req.ScoreOverrides
				cfg.AutoSync.Rules[i].QualityOverrides = req.QualityOverrides
				cfg.AutoSync.Rules[i].QualityStructure = req.QualityStructure
				cfg.AutoSync.Rules[i].Behavior = req.Behavior
				cfg.AutoSync.Rules[i].Overrides = req.Overrides
				// Persist KeepArrCFIDs alongside the other request fields.
				// Without this, the Compare flow's "leave this extra alone"
				// list lived only on the request — frontend follows up with
				// a separate PUT to the rule, but if that PUT failed
				// (network blip), apply ran with the new keep-list while
				// the rule kept the old → next Sync All used stale data
				// and zeroed the just-preserved customs. Now request and
				// rule stay atomically in sync within the same handler.
				cfg.AutoSync.Rules[i].KeepArrCFIDs = req.KeepArrCFIDs
				// Clean sync — clear any LastSyncError set by a previous
				// failed attempt so the error badge disappears in the UI
				// once the user has actually fixed the bad config.
				cfg.AutoSync.Rules[i].LastSyncError = ""
				// Equalize UpdatedAt with LastSyncTime so the Profiles tab
				// "● Unsynced changes" indicator clears. The auto-sync
				// engine path bumps both via UpdateAutoSyncRuleCommit; this
				// is the manual /api/sync/apply equivalent.
				nowSync := time.Now().Format(time.RFC3339)
				cfg.AutoSync.Rules[i].LastSyncTime = nowSync
				cfg.AutoSync.Rules[i].UpdatedAt = nowSync
				// Mirror auto-sync engine's UpdateAutoSyncRuleCommit so the
				// manual /api/sync/apply path tracks the same recovery
				// metadata. Two snapshots needed for TRaSH-restructure
				// resilience:
				//   - LastSyncCommit + PriorAvailableGroups: per-group
				//     state for the brand-new-group detection path.
				//   - PriorSyncedCFs: per-CF set of everything that ended
				//     up in the Arr profile. Used by the CF-level recovery
				//     pass in ExpandSelectedCFsForBrandNewGroups + the
				//     editor's restoreFromSyncHistory so CFs that reached
				//     Arr via profile.formatItems (not via SelectedCFs)
				//     survive a structural restructure that moves them
				//     into an existing default-on cf-group.
				if currentCommit := s.Core.Trash.CurrentCommit(); currentCommit != "" {
					cfg.AutoSync.Rules[i].LastSyncCommit = currentCommit
					if cfg.AutoSync.Rules[i].TrashProfileID != "" {
						cfg.AutoSync.Rules[i].PriorAvailableGroups = core.ComputeAvailableGroups(ad, cfg.AutoSync.Rules[i].TrashProfileID)
					}
				}
				// Mirror the auto-sync engine's clearing in
				// UpdateAutoSyncRuleCommit — the rule just successfully
				// synced, so any Profile Sync detection entries are now
				// stale and the "Out of sync" pill must drop. WatchState
				// LastUpstreamFingerprint is retained so the next detection
				// tick doesn't immediately re-fire for the same upstream.
				// LastDriftFingerprint IS cleared — by pushing target to
				// Arr we just overwrote whatever drifted, so drift is
				// reconciled by definition. Without this clear, the
				// status pill stays stuck on "Out of sync" until the
				// next scheduled drift pass.
				cfg.AutoSync.Rules[i].PendingChanges = nil
				if cfg.AutoSync.Rules[i].WatchState != nil {
					cfg.AutoSync.Rules[i].WatchState.LastDriftFingerprint = ""
					cfg.AutoSync.Rules[i].WatchState.LastDriftNotifiedAt = nowSync
				}
				syncedCFs := make([]string, 0, len(plan.CFActions))
				for _, a := range plan.CFActions {
					syncedCFs = append(syncedCFs, a.TrashID)
				}
				cfg.AutoSync.Rules[i].PriorSyncedCFs = syncedCFs
				// Mirror autosync.go's UpdateAutoSyncRuleCommit fingerprint
				// clear (introduced 2026-06-02 for Profile Sync ↔ CF Sync
				// pill consistency). Without this on the manual sync path,
				// users clicking Sync Now from Profile Sync Rules see the
				// profile pill flip to "in sync" while the CF Sync Rules
				// pills stay stuck on "Arr drift" until the next scheduled
				// drift pass. After a successful push the live Arr spec
				// matches disk by definition, so prior drift is reconciled.
				if len(syncedCFs) > 0 {
					instID := cfg.AutoSync.Rules[i].InstanceID
					for j := range cfg.Instances {
						if cfg.Instances[j].ID != instID {
							continue
						}
						fp := cfg.Instances[j].CFDriftFingerprints
						if len(fp) == 0 {
							break
						}
						for _, tid := range syncedCFs {
							delete(fp, tid)
						}
						if len(fp) == 0 {
							cfg.Instances[j].CFDriftFingerprints = nil
						}
						break
					}
				}
				return
			}
		}
		nowSync := time.Now().Format(time.RFC3339)
		// Same commit/snapshot/CF capture for newly-appended rules so the
		// first sync of a fresh Save-and-Sync flow lands with complete
		// recovery metadata.
		currentCommit := s.Core.Trash.CurrentCommit()
		var priorGroups map[string]bool
		if currentCommit != "" && req.ProfileTrashID != "" {
			priorGroups = core.ComputeAvailableGroups(ad, req.ProfileTrashID)
		}
		newSyncedCFs := make([]string, 0, len(plan.CFActions))
		for _, a := range plan.CFActions {
			newSyncedCFs = append(newSyncedCFs, a.TrashID)
		}
		cfg.AutoSync.Rules = append(cfg.AutoSync.Rules, core.AutoSyncRule{
			ID:                   core.GenerateID(),
			Enabled:              false,
			InstanceID:           req.InstanceID,
			ProfileSource:        newSource,
			TrashProfileID:       req.ProfileTrashID,
			ImportedProfileID:    req.ImportedProfileID,
			ArrProfileID:         arrID,
			SelectedCFs:          req.SelectedCFs,
			ExcludedCFs:          req.ExcludedCFs,
			KeepArrCFIDs:         req.KeepArrCFIDs,
			ScoreOverrides:       req.ScoreOverrides,
			QualityOverrides:    req.QualityOverrides,
			QualityStructure:     req.QualityStructure,
			Behavior:             req.Behavior,
			Overrides:            req.Overrides,
			LastSyncTime:         nowSync,
			UpdatedAt:            nowSync,
			LastSyncCommit:       currentCommit,
			PriorAvailableGroups: priorGroups,
			PriorSyncedCFs:       newSyncedCFs,
		})
		// Same fingerprint clear as the update-existing-rule branch.
		// Edge case: another rule on this instance was already pushing
		// these CFs and they had drifted; creating a fresh rule that
		// pushes them again reconciles Arr ↔ disk for those tids
		// regardless of which rule owns the push semantically.
		if len(newSyncedCFs) > 0 {
			for j := range cfg.Instances {
				if cfg.Instances[j].ID != req.InstanceID {
					continue
				}
				fp := cfg.Instances[j].CFDriftFingerprints
				if len(fp) == 0 {
					break
				}
				for _, tid := range newSyncedCFs {
					delete(fp, tid)
				}
				if len(fp) == 0 {
					cfg.Instances[j].CFDriftFingerprints = nil
				}
				break
			}
		}
	})

	writeJSON(w, result)
}

// --- Sync History ---

func (s *Server) handleSyncHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}
	// Mark stale entries for this instance as orphaned (do NOT delete) so
	// the user can either Restore or Remove via the UI. Soft-tombstone
	// preserves full sync intent. Skip silently when the instance is
	// unreachable — never mutate state on a connection error.
	inst, ok := s.Core.Config.GetInstance(id)
	if ok {
		client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
		} else {
			validIDs := make(map[int]bool)
			for _, p := range profiles {
				validIDs[p.ID] = true
			}
			var events []core.CleanupEvent
			now := time.Now().Format(time.RFC3339)
			s.Core.Config.Update(func(cfg *core.Config) {
				seenOrphan := make(map[int]bool)
				for i := range cfg.SyncHistory {
					h := &cfg.SyncHistory[i]
					if h.InstanceID != id {
						continue
					}
					profileExists := validIDs[h.ArrProfileID]
					if !profileExists && h.OrphanedAt == "" {
						h.OrphanedAt = now
						if !seenOrphan[h.ArrProfileID] {
							seenOrphan[h.ArrProfileID] = true
							log.Printf("Cleanup: marking sync history for %q orphaned (Arr profile %d gone from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
							s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: marking %q orphaned (profile %d gone from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
							events = append(events, core.CleanupEvent{
								ProfileName:  h.ProfileName,
								InstanceName: inst.Name,
								ArrProfileID: h.ArrProfileID,
								Timestamp:    now,
							})
						}
					} else if profileExists && h.OrphanedAt != "" {
						h.OrphanedAt = ""
					}
				}
				for i := range cfg.AutoSync.Rules {
					r := &cfg.AutoSync.Rules[i]
					if r.InstanceID != id || r.ArrProfileID == 0 {
						continue
					}
					profileExists := validIDs[r.ArrProfileID]
					if !profileExists && r.OrphanedAt == "" {
						log.Printf("Cleanup: marking auto-sync rule %s orphaned (Arr profile %d gone from %s)", r.ID, r.ArrProfileID, inst.Name)
						s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: marking rule %s orphaned (profile %d gone from %s)", r.ID, r.ArrProfileID, inst.Name)
						r.OrphanedAt = now
					} else if profileExists && r.OrphanedAt != "" {
						r.OrphanedAt = ""
					}
				}
			})
			if len(events) > 0 {
				s.Core.CleanupMu.Lock()
				s.Core.CleanupEvents = append(s.Core.CleanupEvents, events...)
				if len(s.Core.CleanupEvents) > 50 {
					trimmed := make([]core.CleanupEvent, 50)
					copy(trimmed, s.Core.CleanupEvents[len(s.Core.CleanupEvents)-50:])
					s.Core.CleanupEvents = trimmed
				}
				s.Core.CleanupMu.Unlock()
				s.Core.NotifyCleanup(events)
			}
		}
	}
	entries := s.Core.Config.GetSyncHistory(id)
	if entries == nil {
		entries = []core.SyncHistoryEntry{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleProfileChangeHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	arrProfileIDStr := r.PathValue("arrProfileId")
	arrProfileID, err := strconv.Atoi(arrProfileIDStr)
	if err != nil || id == "" {
		writeError(w, 400, "Invalid instance or profile ID")
		return
	}
	entries := s.Core.Config.GetProfileChangeHistory(id, arrProfileID)
	if entries == nil {
		entries = []core.SyncHistoryEntry{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleDeleteSyncHistory(w http.ResponseWriter, r *http.Request) {
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
	if err := s.Core.Config.DeleteSyncHistory(id, arrProfileID); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleCleanupEvents returns and clears pending cleanup events.
func (s *Server) handleCleanupEvents(w http.ResponseWriter, r *http.Request) {
	s.Core.CleanupMu.Lock()
	events := s.Core.CleanupEvents
	s.Core.CleanupEvents = nil
	s.Core.CleanupMu.Unlock()
	if events == nil {
		events = []core.CleanupEvent{}
	}
	writeJSON(w, events)
}

// handleAutoSyncEvents returns and clears pending auto-sync events for frontend toast.
func (s *Server) handleAutoSyncEvents(w http.ResponseWriter, r *http.Request) {
	s.Core.AutoSyncMu.Lock()
	events := s.Core.AutoSyncEvents
	s.Core.AutoSyncEvents = nil
	s.Core.AutoSyncMu.Unlock()
	if events == nil {
		events = []core.AutoSyncEvent{}
	}
	writeJSON(w, events)
}
