package core

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"clonarr/internal/arr"
)

// autoSyncAfterPull runs after a successful TRaSH repo pull.
// For each enabled rule, checks if the repo commit changed since last sync,
// builds a dry-run plan, and applies if there are actual changes.
//
// trigger names what kicked off this run so the parent AUTOSYNC operation
// in the log can be filtered: SourceAutoPullStartup (container start),
// SourceAutoPullInterval (scheduled tick), SourceManualPull (user clicked
// Pull in the UI).
func (app *App) AutoSyncAfterPull(trigger string) {
	currentCommit := app.Trash.CurrentCommit()
	if currentCommit == "" {
		if app.DebugLog != nil {
			app.DebugLog.Logf(LogAutoSync, "TRaSH data not loaded — skipping AutoSyncAfterPull")
		}
		return
	}

	// Clean up stale rules/history for Arr profiles that no longer exist
	app.CleanupStaleRules()

	// One-time migration for pre-fix rules: derive PriorAvailableGroups
	// retroactively from each rule's LastSyncCommit so brand-new TRaSH
	// groups (e.g. the May 2026 French Unwanted restructure) aren't
	// auto-disabled by restoreFromSyncHistory's "no group activity =
	// opted out" heuristic. Idempotent — skips rules already migrated.
	app.MigratePriorAvailableGroups()

	cfg := app.Config.Get()
	if cfg.AutoSync.Paused {
		// Global pause is on — skip all auto-driven sync. Manual actions
		// ("Sync All", per-rule Sync now, Save & Sync from a profile) are
		// unaffected.
		app.DebugLog.Logf(LogAutoSync, "Auto-sync paused globally — skipping AutoSyncAfterPull")
		return
	}
	if len(cfg.AutoSync.Rules) == 0 {
		return
	}

	if trigger == "" {
		trigger = SourceAutoPullInterval
	}
	commitShort := currentCommit
	if len(currentCommit) > 7 {
		commitShort = currentCommit[:7]
	}
	tick := app.DebugLog.BeginOp(OpAutoSync, trigger, fmt.Sprintf("commit=%s rules=%d", commitShort, len(cfg.AutoSync.Rules)))
	endResult := "ok | tick complete"
	defer func() { tick.End(endResult) }()

	// Group eligible rules by instance so each instance gets one
	// pre-flight reachability check, not one per rule.
	candidates := filterEligibleRulesForPull(cfg.AutoSync.Rules, currentCommit)
	if len(candidates) == 0 {
		return
	}
	changed, noChange, errorCount, changedSummary := app.runRulesPerInstance(candidates, currentCommit, tick)
	if len(changedSummary) > 0 {
		tick.Logf("changed: %s", strings.Join(changedSummary, " | "))
	}
	endResult = fmt.Sprintf("ok | %d changed, %d no-op, %d errors", changed, noChange, errorCount)
}

// filterEligibleRulesForPull returns rules that AutoSyncAfterPull should
// consider — enabled, not orphaned, not imported, and with a TRaSH
// commit that's actually changed since the rule's last sync. Pulled out
// so both pull-tick and SyncSchedule iterations stay readable.
func filterEligibleRulesForPull(rules []AutoSyncRule, currentCommit string) []AutoSyncRule {
	out := make([]AutoSyncRule, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		if rule.OrphanedAt != "" {
			continue
		}
		if rule.ProfileSource == "imported" {
			continue
		}
		if rule.LastSyncCommit == currentCommit {
			continue
		}
		out = append(out, rule)
	}
	return out
}

// ForceSyncAllRules runs sync on every active rule unconditionally, ignoring
// the LastSyncCommit == currentCommit short-circuit that AutoSyncAfterPull
// uses. Triggered by the SyncSchedule wall-clock timer; the point is to
// catch Arr-side drift (a user editing scores in Sonarr/Radarr directly,
// or another tool overwriting clonarr's settings) on a periodic cadence
// without needing a passive drift detector. Rules that are paused,
// orphaned, imported, or disabled are still skipped — same gates as the
// pull-driven path.
func (app *App) ForceSyncAllRules() {
	currentCommit := app.Trash.CurrentCommit()
	if currentCommit == "" {
		if app.DebugLog != nil {
			app.DebugLog.Logf(LogAutoSync, "TRaSH data not loaded — skipping ForceSyncAllRules")
		}
		return
	}

	app.CleanupStaleRules()
	app.MigratePriorAvailableGroups()

	cfg := app.Config.Get()
	if cfg.AutoSync.Paused {
		app.DebugLog.Logf(LogAutoSync, "Auto-sync paused globally — skipping ForceSyncAllRules")
		return
	}
	if len(cfg.AutoSync.Rules) == 0 {
		return
	}

	commitShort := currentCommit
	if len(currentCommit) > 7 {
		commitShort = currentCommit[:7]
	}
	tick := app.DebugLog.BeginOp(OpAutoSync, SourceAutoSyncSchedule, fmt.Sprintf("commit=%s rules=%d", commitShort, len(cfg.AutoSync.Rules)))
	endResult := "ok | tick complete"
	defer func() { tick.End(endResult) }()

	// Force-sync: no LastSyncCommit gate, just filter the per-pass eligibility.
	candidates := make([]AutoSyncRule, 0, len(cfg.AutoSync.Rules))
	for _, rule := range cfg.AutoSync.Rules {
		if !rule.Enabled || rule.OrphanedAt != "" || rule.ProfileSource == "imported" {
			continue
		}
		candidates = append(candidates, rule)
	}
	if len(candidates) == 0 {
		return
	}
	changed, noChange, errorCount, changedSummary := app.runRulesPerInstance(candidates, currentCommit, tick)
	if len(changedSummary) > 0 {
		tick.Logf("changed: %s", strings.Join(changedSummary, " | "))
	}
	endResult = fmt.Sprintf("ok | %d changed, %d no-op, %d errors", changed, noChange, errorCount)
}

// runRulesPerInstance groups rules by instance, runs a pre-flight
// reachability check (WaitForInstanceReachable) per instance in
// parallel, and processes only the rules whose instance is reachable.
// One notification per unreachable instance per pass (regardless of
// rule count) — no more 21-rules × 10-min retry storm. Reachable
// instances proceed in parallel; rules within an instance still
// serialise via the existing per-instance mutex inside runAutoSyncRule.
func (app *App) runRulesPerInstance(rules []AutoSyncRule, currentCommit string, tick *Operation) (changed, noChange, errorCount int, changedSummary []string) {
	byInstance := make(map[string][]AutoSyncRule)
	for _, r := range rules {
		byInstance[r.InstanceID] = append(byInstance[r.InstanceID], r)
	}

	type partial struct {
		changed, noChange, errorCount int
		summary                       []string
	}
	results := make(chan partial, len(byInstance))
	var wg sync.WaitGroup
	for instID, instRules := range byInstance {
		wg.Add(1)
		go func(instID string, instRules []AutoSyncRule) {
			defer wg.Done()
			inst, ok := app.Config.GetInstance(instID)
			if !ok {
				// Instance vanished mid-pass — mark each rule as errored.
				p := partial{errorCount: len(instRules)}
				for _, r := range instRules {
					app.UpdateAutoSyncRuleError(r.ID, "instance not found: "+instID)
				}
				results <- p
				return
			}
			// Pre-flight reachability check (~30 min budget) — one per
			// instance per pass. Replaces the prior per-rule retry chain.
			if !app.WaitForInstanceReachable(inst) {
				friendlyMsg := inst.Name + " is not reachable — auto-sync skipped after 30 minutes of retries; will retry on next sync"
				log.Printf("Auto-sync: %s unreachable after retry budget — marking %d rule(s) as errored, sending one notification", inst.Name, len(instRules))
				app.DebugLog.Logf(LogAutoSync, "Reachability: %s unreachable after 30-min budget — %d rule(s) affected", inst.Name, len(instRules))
				// One notification per instance per pass, not per rule.
				ad := app.Trash.GetAppData(inst.Type)
				var profileName string
				if len(instRules) > 0 && ad != nil {
					if p := findProfile(ad, instRules[0].TrashProfileID); p != nil {
						profileName = p.Name
					}
				}
				app.NotifyAutoSync(instRules[0], inst, profileName, nil, fmt.Errorf("%s", friendlyMsg))
				for _, r := range instRules {
					app.UpdateAutoSyncRuleError(r.ID, friendlyMsg)
				}
				results <- partial{errorCount: len(instRules)}
				return
			}
			// Arr is reachable — process all rules for this instance.
			// Each runAutoSyncRule call now fail-fasts on connection errors
			// (Arr was up at pre-flight; mid-sync disconnects are rare and
			// can be retried by the user via Sync now).
			var p partial
			for _, r := range instRules {
				outcome, summary := app.runAutoSyncRule(r, currentCommit, tick)
				switch outcome {
				case outcomeChanged:
					p.changed++
					if summary != "" {
						p.summary = append(p.summary, summary)
					}
				case outcomeNoChange:
					p.noChange++
				case outcomeError:
					p.errorCount++
				}
			}
			results <- p
		}(instID, instRules)
	}
	wg.Wait()
	close(results)
	for p := range results {
		changed += p.changed
		noChange += p.noChange
		errorCount += p.errorCount
		changedSummary = append(changedSummary, p.summary...)
	}
	return
}

// ruleOutcome describes the result classification of a single auto-sync
// rule evaluation, used by AutoSyncAfterPull to compose the tick summary
// without re-parsing log lines.
type ruleOutcome int

const (
	outcomeNoChange ruleOutcome = iota
	outcomeChanged
	outcomeError
)

// runAutoSyncRule evaluates and applies a single auto-sync rule.
// parent is the AUTOSYNC tick that triggered this rule; passed in so
// the per-rule SYNC sub-operation can attach to it for nested-trace
// extraction. Nil parent is fine — the sub-op falls back to a
// standalone op.
//
// Connection errors here fail-fast (no retry). The pre-flight reachability
// check in runRulesPerInstance / WaitForInstanceReachable confirmed Arr
// was up before this function was called; a mid-sync disconnect is rare
// and can be retried by the user via Sync now / Sync All.
//
// Returns the outcome classification (changed / no-change / error) and
// a one-line summary string for the changed case (empty for the others).
// The caller composes these into the tick-summary line so the trace
// shows exactly which rules contributed work without per-rule fanout.
func (app *App) runAutoSyncRule(rule AutoSyncRule, currentCommit string, parent *Operation) (ruleOutcome, string) {
	// Re-check rule still exists (may have been deleted since snapshot was taken)
	cfg := app.Config.Get()
	ruleExists := false
	for _, r := range cfg.AutoSync.Rules {
		if r.ID == rule.ID && r.Enabled {
			ruleExists = true
			break
		}
	}
	if !ruleExists {
		log.Printf("Auto-sync: skipping rule %s — removed or disabled since pull started", rule.ID)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: skipped — removed or disabled since pull started", rule.ID)
		return outcomeNoChange, ""
	}

	inst, ok := app.Config.GetInstance(rule.InstanceID)
	if !ok {
		log.Printf("Auto-sync: skipping rule %s — instance %s not found", rule.ID, rule.InstanceID)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: skipped — instance %s not found", rule.ID, rule.InstanceID)
		return outcomeError, ""
	}

	// Per-instance mutex — skip if manual sync is running
	mu := app.GetSyncMutex(inst.ID)
	if !mu.TryLock() {
		log.Printf("Auto-sync: skipping rule %s — sync already in progress for %s", rule.ID, inst.Name)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: skipped — sync already in progress for %s", rule.ID, inst.Name)
		return outcomeNoChange, ""
	}
	defer mu.Unlock()

	// Background-tick rules don't open per-rule sub-operations — the
	// tick summary already lists which rules produced changes, and a
	// 50-rule tick where 47 are no-op would otherwise emit 300+ lines
	// just on begin/end markers. Manual syncs (handleApply, restore,
	// etc.) keep their full op-tagged trace because that's what bug
	// reports are usually pulled from. Autosync errors still surface
	// via the existing app.DebugLog.Logf(LogError, ...) lines below
	// and via the tick summary's error count.
	//
	// op stays nil — passed through to BuildSyncPlan/ExecuteSyncPlan
	// where the nil-safe op.Logf calls compile to no-ops. If a future
	// Phase wants per-rule autosync detail (e.g. for failure
	// investigation), open a sub-op only when an error is detected and
	// the trace is worth keeping.
	var op *Operation

	log.Printf("Auto-sync: evaluating rule %s (instance=%s, profile=%s)", rule.ID, inst.Name, rule.TrashProfileID)

	ad := app.Trash.GetAppData(inst.Type)
	if ad == nil {
		app.UpdateAutoSyncRuleError(rule.ID, "no TRaSH data for "+inst.Type)
		app.DebugLog.Logf(LogError, "Auto-sync rule %s: no TRaSH data for %s", rule.ID, inst.Type)
		return outcomeError, ""
	}

	// Auto-include CFs from default-on cf-groups that are brand new since
	// the last successful sync of this rule (TRaSH structural restructure
	// detection). Without this, CFs that moved from profile.formatItems
	// into a new default-on group would be dropped by the sync plan and
	// their scores reset to 0 in Arr — see ExpandSelectedCFsForBrandNewGroups
	// for full reasoning. We persist the expanded SelectedCFs back to the
	// rule before plan-building so subsequent syncs keep including them
	// even after the group is no longer brand-new from
	// PriorAvailableGroups' perspective.
	expandedCFs, expansionAdded := ExpandSelectedCFsForBrandNewGroups(rule, ad)
	if len(expansionAdded) > 0 {
		app.UpdateAutoSyncRuleSelectedCFs(rule.ID, expandedCFs)
		rule.SelectedCFs = expandedCFs // local copy for the rest of this run
		log.Printf("Auto-sync: rule %s — auto-included %d CF(s) from new default-on cf-group(s) (TRaSH restructure detection)", rule.ID, len(expansionAdded))
	}

	// Build sync request from rule
	req := SyncRequest{
		InstanceID:       rule.InstanceID,
		ProfileTrashID:   rule.TrashProfileID,
		ArrProfileID:     rule.ArrProfileID,
		SelectedCFs:      expandedCFs,
		KeepArrCFIDs:     rule.KeepArrCFIDs,
		ScoreOverrides:   rule.ScoreOverrides,
		QualityOverrides: rule.QualityOverrides,
		QualityStructure: rule.QualityStructure,
		Behavior:         rule.Behavior,
		Overrides:        rule.Overrides,
	}
	if rule.ProfileSource == "imported" {
		req.ImportedProfileID = rule.ImportedProfileID
	}

	// Resolve imported profile if needed
	var imported *ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := app.Profiles.Get(req.ImportedProfileID)
		if !ok {
			app.UpdateAutoSyncRuleError(rule.ID, "imported profile not found: "+req.ImportedProfileID)
			app.DebugLog.Logf(LogError, "Auto-sync rule %s: imported profile not found: %s", rule.ID, req.ImportedProfileID)
			return outcomeError, ""
		}
		imported = &p
	}

	// Dry-run plan — fail-fast on errors. Connection errors are unusual
	// here since the pre-flight check (WaitForInstanceReachable in
	// runRulesPerInstance) already confirmed Arr was up; if Arr goes
	// down mid-sync, surface the error and let the user/scheduler
	// retry on next pass.
	customCFs := app.CustomCFs.List(inst.Type)
	lastSyncedCFs := app.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, app.HTTPClient, op)
	if err != nil {
		rawMsg := fmt.Sprintf("plan failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, rawMsg)
		app.DebugLog.Logf(LogError, "Auto-sync rule %s: plan failed: %s", rule.ID, rawMsg)

		if IsConnectionError(err) {
			friendlyMsg := inst.Name + " became unreachable during sync — will retry on next sync"
			log.Printf("Auto-sync: rule %s — %s unreachable mid-sync", rule.ID, inst.Name)
			app.DebugLog.Logf(LogAutoSync, "Rule %s: %s unreachable mid-sync: %v", rule.ID, inst.Name, err)
			app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
			profileName := rule.TrashProfileID
			if p := findProfile(ad, rule.TrashProfileID); p != nil {
				profileName = p.Name
			}
			app.NotifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", friendlyMsg))
			return outcomeError, ""
		}

		friendlyMsg := FriendlyAutoSyncError(err, inst.Name, app.IsShuttingDown())
		app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
		// Auto-disable rule if Arr profile no longer exists. Sentinel is
		// sync.go:369's `target profile no longer exists in Arr`. We used
		// to also match a bare "not found" but that fires on
		// `TRaSH profile %s not found` (transient TRaSH parse glitch),
		// `item %s not found` (filestore/customcf/cfgroup lookups),
		// `cutoff %q not found in resolved items`, etc. — none of which
		// should silently disable a sync rule.
		//
		// Belt-and-braces connection-error gate stays so that a future
		// Arr version returning a misleading 404-shaped body during
		// restart can't auto-disable rules. Rules must NEVER be
		// auto-disabled because Arr was unreachable.
		if !IsConnectionError(err) && strings.Contains(err.Error(), "no longer exists") {
			log.Printf("Auto-sync: disabling rule %s — target profile no longer exists", rule.ID)
			app.DebugLog.Logf(LogAutoSync, "Rule %s: auto-disabled — target Arr profile no longer exists (ID %d)", rule.ID, rule.ArrProfileID)
			app.Config.Update(func(cfg *Config) {
				for i := range cfg.AutoSync.Rules {
					if cfg.AutoSync.Rules[i].ID == rule.ID {
						cfg.AutoSync.Rules[i].Enabled = false
						return
					}
				}
			})
		}
		profileName := rule.TrashProfileID
		if p := findProfile(ad, rule.TrashProfileID); p != nil {
			profileName = p.Name
		}
		app.NotifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", friendlyMsg))
		return outcomeError, ""
	}

	if !plan.HasChanges() {
		// No actual changes — update commit hash, clear error. The op end
		// marker carries the "no changes" verdict so we don't emit an
		// extra Logf line here; the per-rule trace stays at 2 lines
		// (begin + end) for the common no-op path.
		log.Printf("Auto-sync: rule %s — no changes for %s", rule.ID, inst.Name)
		app.UpdateAutoSyncRuleCommit(rule.ID, currentCommit, ComputeAvailableGroups(ad, rule.TrashProfileID))
		// Refresh sync history's SelectedCFs to mirror the rule's current
		// SelectedCFs. Two reasons:
		//   1) ExpandSelectedCFsForBrandNewGroups may have just added CFs
		//      (TRaSH structural restructure detection) — without refresh,
		//      frontend's restoreFromSyncHistory would mark them inactive.
		//   2) An earlier expansion sync may have run on the no-changes
		//      path and skipped the refresh; subsequent loads would still
		//      see stale data. Refreshing unconditionally on no-changes
		//      keeps history.SelectedCFs in lockstep with rule.SelectedCFs.
		// We update in-place rather than writing a new history entry to
		// avoid bloating history with no-op events.
		selectedCFMap := make(map[string]bool, len(rule.SelectedCFs))
		for _, id := range rule.SelectedCFs {
			selectedCFMap[id] = true
		}
		app.RefreshLatestSyncHistorySelectedCFs(inst.ID, req.ArrProfileID, selectedCFMap)
		return outcomeNoChange, ""
	}

	// Apply — fail-fast on errors (see plan-step rationale above).
	result, err := ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, ResolveSyncBehavior(req.Behavior), app.HTTPClient, op)
	if err != nil {
		if IsConnectionError(err) {
			friendlyMsg := inst.Name + " became unreachable during sync — will retry on next sync"
			log.Printf("Auto-sync: rule %s apply — %s unreachable mid-sync", rule.ID, inst.Name)
			app.DebugLog.Logf(LogAutoSync, "Rule %s: %s unreachable mid-apply: %v", rule.ID, inst.Name, err)
			app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
			app.NotifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", friendlyMsg))
			return outcomeError, ""
		}
		rawMsg := fmt.Sprintf("apply failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, rawMsg)
		app.DebugLog.Logf(LogError, "Auto-sync rule %s: apply failed: %s", rule.ID, rawMsg)
		friendlyMsg := FriendlyAutoSyncError(err, inst.Name, app.IsShuttingDown())
		app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
		app.NotifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", friendlyMsg))
		return outcomeError, ""
	}

	log.Printf("Auto-sync: rule %s applied — %d CFs created, %d updated, %d scores on %s",
		rule.ID, result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, inst.Name)

	// Arr returned errors. Decide whether to auto-disable the rule.
	// Only disable when EVERY error is a user-fixable config problem
	// (HTTP 400 / 409 / 422 — FluentValidation rejections, conflicts).
	// 5xx, 401/403, ListX fetch failures, raw network errors are all
	// transient or external — retrying is the right move, not disabling
	// the rule and bothering the user. The error summary is still
	// recorded in LastSyncError so the UI shows the badge either way.
	if len(result.Errors) > 0 {
		errSummary := strings.Join(result.Errors, " | ")
		shouldDisable := AllUserConfigErrors(result.Errors)
		if shouldDisable {
			log.Printf("Auto-sync: rule %s disabled — Arr returned %d user-config error(s): %s", rule.ID, len(result.Errors), errSummary)
			app.DebugLog.Logf(LogError, "Auto-sync rule %s: disabling — Arr returned %d user-config error(s): %s", rule.ID, len(result.Errors), flattenForLog(redactSecrets(errSummary)))
			app.UpdateAutoSyncRuleError(rule.ID, errSummary)
			app.Config.Update(func(cfg *Config) {
				for i := range cfg.AutoSync.Rules {
					if cfg.AutoSync.Rules[i].ID == rule.ID {
						cfg.AutoSync.Rules[i].Enabled = false
						return
					}
				}
			})
			app.NotifyAutoSync(rule, inst, plan.ProfileName, result, fmt.Errorf("auto-sync disabled — %s", errSummary))
		} else {
			log.Printf("Auto-sync: rule %s — Arr returned %d transient/external error(s); rule kept enabled, will retry next tick: %s", rule.ID, len(result.Errors), errSummary)
			app.DebugLog.Logf(LogError, "Auto-sync rule %s: transient error(s), kept enabled: %s", rule.ID, flattenForLog(redactSecrets(errSummary)))
			app.UpdateAutoSyncRuleError(rule.ID, errSummary)
			// No NotifyAutoSync — the next tick will retry; only emit a
			// notification when we actually disable.
		}
		return outcomeError, ""
	}

	app.UpdateAutoSyncRuleCommit(rule.ID, currentCommit, ComputeAvailableGroups(ad, rule.TrashProfileID))

	// Update sync history (mirror manual sync — api/sync.go handleApply).
	allCFIDs := make([]string, 0)
	for _, a := range plan.CFActions {
		allCFIDs = append(allCFIDs, a.TrashID)
	}
	// Build selectedCFs map from rule
	selectedCFMap := make(map[string]bool, len(rule.SelectedCFs))
	for _, id := range rule.SelectedCFs {
		selectedCFMap[id] = true
	}
	// CF-set diff against previous entry (catches group-level add/remove
	// that score engine doesn't report when CFs had score=0).
	cfSetDetails := []string{}
	prevEntry := app.Config.GetLatestSyncEntry(inst.ID, req.ArrProfileID)
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
			if len(tid) > 12 {
				return tid[:12]
			}
			return tid
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
	now := time.Now().Format(time.RFC3339)
	entry := SyncHistoryEntry{
		InstanceID:        inst.ID,
		InstanceType:      inst.Type,
		ProfileTrashID:    req.ProfileTrashID,
		ImportedProfileID: req.ImportedProfileID,
		ProfileName:       plan.ProfileName,
		ArrProfileID:      req.ArrProfileID,
		ArrProfileName:    plan.ArrProfileName,
		SyncedCFs:         allCFIDs,
		SelectedCFs:       selectedCFMap,
		ScoreOverrides:    rule.ScoreOverrides,
		QualityOverrides:  rule.QualityOverrides,
		QualityStructure:  rule.QualityStructure,
		Overrides:         rule.Overrides,
		Behavior:          rule.Behavior,
		KeepArrCFIDs:      rule.KeepArrCFIDs,
		CFsCreated:        result.CFsCreated,
		CFsUpdated:        result.CFsUpdated,
		ScoresUpdated:     result.ScoresUpdated,
		LastSync:          now,
		Changes:           changes,
	}
	// Freeze AppliedAt on real-change entries so the History tab's "Last
	// Changed" column shows when changes actually landed, not when the last
	// no-op sync ran. Baseline / no-op entries leave it blank → UI falls
	// back to LastSync.
	if changes != nil {
		entry.AppliedAt = now
	}
	if result.ProfileCreated {
		entry.ArrProfileID = result.ArrProfileID
		entry.ArrProfileName = result.ArrProfileName
		// Update rule with new Arr profile ID
		app.Config.Update(func(cfg *Config) {
			for i := range cfg.AutoSync.Rules {
				if cfg.AutoSync.Rules[i].ID == rule.ID {
					log.Printf("Auto-sync: updating rule %s with new Arr profile ID %d", rule.ID, result.ArrProfileID)
					cfg.AutoSync.Rules[i].ArrProfileID = result.ArrProfileID
					return
				}
			}
		})
	}
	if err := app.Config.UpsertSyncHistory(entry); err != nil {
		log.Printf("Auto-sync: failed to save sync history: %v", err)
	}

	app.NotifyAutoSync(rule, inst, plan.ProfileName, result, nil)

	// Push event to frontend toast queue (only when there are actual changes)
	if result.CFsCreated > 0 || result.CFsUpdated > 0 || result.ScoresUpdated > 0 || result.QualityUpdated || len(result.SettingsDetails) > 0 {
		// Collect details for the frontend toast. The UI owns compacting and
		// expansion, so keep the event payload complete.
		var details []string
		details = append(details, result.CFDetails...)
		details = append(details, result.ScoreDetails...)
		details = append(details, result.QualityDetails...)
		details = append(details, result.SettingsDetails...)
		app.AutoSyncMu.Lock()
		app.AutoSyncEvents = append(app.AutoSyncEvents, AutoSyncEvent{
			InstanceName:   inst.Name,
			ProfileName:    plan.ProfileName,
			ArrProfileName: result.ArrProfileName,
			CFsCreated:     result.CFsCreated,
			CFsUpdated:     result.CFsUpdated,
			ScoresUpdated:  result.ScoresUpdated,
			QualityUpdated: result.QualityUpdated,
			SettingsCount:  len(result.SettingsDetails),
			Details:        details,
			Timestamp:      time.Now().Format(time.RFC3339),
		})
		if len(app.AutoSyncEvents) > 50 {
			trimmed := make([]AutoSyncEvent, 50)
			copy(trimmed, app.AutoSyncEvents[len(app.AutoSyncEvents)-50:])
			app.AutoSyncEvents = trimmed
		}
		app.AutoSyncMu.Unlock()
	}

	// Classify the outcome based on the actual apply result, not the
	// plan summary. plan.HasChanges() is intentionally optimistic in
	// update mode (it always returns true so settings-level changes
	// — which can only be detected at apply time — never get
	// short-circuited). After apply we have ground truth: if nothing
	// was created, updated, scored, quality-touched, or settings-
	// changed, it's a no-op even if HasChanges() said go.
	noWork := result.CFsCreated == 0 && result.CFsUpdated == 0 &&
		result.ScoresUpdated == 0 && !result.QualityUpdated &&
		len(result.SettingsDetails) == 0
	if noWork {
		return outcomeNoChange, ""
	}

	// Build a one-line summary for the parent tick. Format mirrors the
	// manual-sync toast: profile name + the largest user-visible signal,
	// so the tick line lists exactly what changed without expanding the
	// full sub-op trace inline.
	bits := []string{}
	if result.CFsCreated > 0 {
		bits = append(bits, fmt.Sprintf("%d created", result.CFsCreated))
	}
	if result.CFsUpdated > 0 {
		bits = append(bits, fmt.Sprintf("%d updated", result.CFsUpdated))
	}
	if result.ScoresUpdated > 0 {
		bits = append(bits, fmt.Sprintf("%d scores", result.ScoresUpdated))
	}
	if len(result.SettingsDetails) > 0 {
		bits = append(bits, fmt.Sprintf("%d settings", len(result.SettingsDetails)))
	}
	summary := fmt.Sprintf("%q on %s: %s", plan.ProfileName, inst.Name, strings.Join(bits, ", "))
	return outcomeChanged, summary
}

// applyOrphanMarking is the pure-logic core of soft-tombstone cleanup —
// no I/O, no logging side-effects, no app state. Given a config, the set
// of valid Arr profile IDs per reachable instance, and a timestamp, it
// mutates rules + history in-place to reflect orphan transitions and
// returns the user-visible CleanupEvents. Extracted as a free function
// so unit tests can drive every transition (mark, clear, idempotent,
// unreachable-skip) without spinning up an httptest Arr.
//
// validProfiles map semantics:
//
//	missing key   → instance not probed (e.g. unreachable) — never mutate
//	present key   → instance was probed; value is the set of valid IDs
//	  empty value → instance returned 0 profiles, treat as "all gone"
//	                (intentional after dropping the old startup-skip safety)
//
// instNames is used only to populate user-facing CleanupEvent.InstanceName.
// Pass "" for instances that aren't found if you don't have the lookup
// — the function won't error.
func applyOrphanMarking(cfg *Config, validProfiles map[string]map[int]bool, instNames map[string]string, now string) []CleanupEvent {
	var events []CleanupEvent
	for i := range cfg.AutoSync.Rules {
		r := &cfg.AutoSync.Rules[i]
		valid, ok := validProfiles[r.InstanceID]
		if !ok {
			continue // unreachable instance — leave as-is
		}
		profileExists := valid[r.ArrProfileID]
		if !profileExists && r.OrphanedAt == "" {
			r.OrphanedAt = now
		} else if profileExists && r.OrphanedAt != "" {
			r.OrphanedAt = ""
		}
	}

	// Mirror onto sync history. Emit a CleanupEvent only on the first
	// transition to orphaned (per profile), not on every probe.
	seenOrphan := make(map[string]bool) // instID|arrProfileID
	for i := range cfg.SyncHistory {
		h := &cfg.SyncHistory[i]
		valid, ok := validProfiles[h.InstanceID]
		if !ok {
			continue
		}
		profileExists := valid[h.ArrProfileID]
		if !profileExists && h.OrphanedAt == "" {
			h.OrphanedAt = now
			key := h.InstanceID + "|" + strconv.Itoa(h.ArrProfileID)
			if !seenOrphan[key] {
				seenOrphan[key] = true
				events = append(events, CleanupEvent{
					ProfileName:  h.ProfileName,
					InstanceName: instNames[h.InstanceID],
					ArrProfileID: h.ArrProfileID,
					Timestamp:    now,
				})
			}
		} else if profileExists && h.OrphanedAt != "" {
			h.OrphanedAt = ""
		}
	}
	return events
}

// CleanupStaleRules marks (does NOT delete) auto-sync rules and sync
// history entries when their target Arr profile no longer exists. Setting
// OrphanedAt instead of splice-deleting preserves the full sync intent
// (CFs, scores, qualities, overrides) so the user can either Restore the
// profile from saved state or Remove it manually. Only acts on instances
// that are reachable — unreachable instances are skipped (never modifies
// state on connection error).
//
// Re-running marks idempotently: an already-orphaned rule keeps its
// original OrphanedAt timestamp. A previously-orphaned rule whose Arr
// profile reappears (e.g. user restored manually in Arr) gets its
// OrphanedAt cleared so the rule resumes normal operation.
//
// The previous "0 profiles → skip as startup-state" safety is no longer
// needed: marking is non-destructive, so there's no risk of losing data
// during a transient empty response.
//
// This is the I/O wrapper. The pure mark/clear logic lives in
// applyOrphanMarking and is unit-tested directly.
func (app *App) CleanupStaleRules() {
	cfg := app.Config.Get()
	instNames := make(map[string]string)

	validProfiles := make(map[string]map[int]bool)
	for _, inst := range cfg.Instances {
		instNames[inst.ID] = inst.Name
		client := arr.NewArrClient(inst.URL, inst.APIKey, app.HTTPClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			app.DebugLog.Logf(LogAutoSync, "Cleanup: skipping %s — not reachable: %v", inst.Name, err)
			continue
		}
		ids := make(map[int]bool)
		for _, p := range profiles {
			ids[p.ID] = true
		}
		validProfiles[inst.ID] = ids
	}

	var events []CleanupEvent
	now := time.Now().Format(time.RFC3339)
	app.Config.Update(func(cfg *Config) {
		events = applyOrphanMarking(cfg, validProfiles, instNames, now)
	})

	for _, ev := range events {
		log.Printf("Cleanup: marking sync history for %q orphaned (Arr profile %d gone from %s)", ev.ProfileName, ev.ArrProfileID, ev.InstanceName)
		app.DebugLog.Logf(LogAutoSync, "Cleanup: marked %q orphaned (profile %d gone from %s)", ev.ProfileName, ev.ArrProfileID, ev.InstanceName)
	}

	// Store events for frontend to pick up + send external notifications
	if len(events) > 0 {
		app.CleanupMu.Lock()
		app.CleanupEvents = append(app.CleanupEvents, events...)
		if len(app.CleanupEvents) > 50 {
			trimmed := make([]CleanupEvent, 50)
			copy(trimmed, app.CleanupEvents[len(app.CleanupEvents)-50:])
			app.CleanupEvents = trimmed
		}
		app.CleanupMu.Unlock()
		app.NotifyCleanup(events)
	}
}

// dispatchNotification sends one payload through one configured notification agent.
// This is the single call site in autosync.go — all notification event builders
// (NotifyAutoSync, NotifyCleanup, NotifyRepoUpdate, NotifyChangelog) iterate over
// agents and delegate here.
func (app *App) dispatchNotification(agent NotificationAgent, payload NotificationPayload) {
	app.DispatchNotificationAgent(agent, payload)
}

// NotifyCleanup sends notifications for auto-cleanup events.
// Generates a markdown summary of removed rules/profiles and dispatches
// to all agents that have OnCleanup enabled. Uses SeverityWarning to reflect
// that cleanup is notable but not an error.
func (app *App) NotifyCleanup(events []CleanupEvent) {
	cfg := app.Config.Get()

	description := ""
	for _, ev := range events {
		description += fmt.Sprintf("**%s** — deleted in %s, sync rule removed\n", ev.ProfileName, ev.InstanceName)
	}
	description = strings.TrimSpace(description)

	title := "Clonarr: Sync Rules Cleaned Up"
	payload := NotificationPayload{
		Title:    title,
		Message:  description,
		Color:    0xd29922,
		Severity: NotificationSeverityWarning,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnCleanup {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
}

// FriendlyAutoSyncError converts a raw plan/apply error into plain-language
// text for the rule card and Discord/NTFY/Apprise notifications. Runs AFTER
// IsConnectionError — connection failures take the unreachable-mid-sync
// branch above. This covers the remaining shapes that the Arr client can
// surface (HTTP error statuses, JSON parse failures, context cancellation)
// plus a fallback that strips the Go-specific `request failed: Get "URL":`
// prefix when no specific pattern matches. The full raw error stays in the
// debug log under LogError for developer investigation.
//
// shuttingDown lets the caller pass whether clonarr is mid-shutdown so a
// "context canceled" error can be attributed correctly (shutdown vs
// user-initiated cancel vs HTTP timeout).
func FriendlyAutoSyncError(err error, instName string, shuttingDown bool) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	if strings.Contains(msg, "HTTP 401") || strings.Contains(msg, "HTTP 403") {
		return instName + " rejected the request — check that the API key in clonarr matches the one in " + instName + " (Settings → General)"
	}
	if strings.Contains(msg, "HTTP 404") {
		return instName + " returned 'not found' — the profile or custom format may have been deleted on the Arr side"
	}
	if strings.Contains(msg, "HTTP 409") || strings.Contains(msg, "HTTP 422") {
		return instName + " rejected the sync as invalid — open the rule and review the profile settings against " + instName
	}
	// Match exact 5xx codes so a 200-char-truncated Arr response body
	// containing the literal "HTTP 5" doesn't false-positive.
	if strings.Contains(msg, "HTTP 500") || strings.Contains(msg, "HTTP 502") || strings.Contains(msg, "HTTP 503") || strings.Contains(msg, "HTTP 504") {
		return instName + " returned a server error — check the " + instName + " logs and try again"
	}
	if strings.Contains(lower, "context deadline exceeded") {
		return instName + " did not respond in time — will retry on next sync"
	}
	if strings.Contains(lower, "context canceled") {
		if shuttingDown {
			return "Sync was canceled — clonarr is shutting down"
		}
		return "Sync was canceled — will retry on next sync"
	}
	if strings.Contains(lower, "parse") || strings.Contains(lower, "unmarshal") || strings.Contains(lower, "decode") {
		return instName + " returned an unexpected response — usually a version mismatch or partial restart; will retry on next sync"
	}

	// Fallback: strip the Go-specific `request failed: Get "URL":` prefix
	// so the remaining message reads naturally. Keep the cleaned tail; if
	// it's empty, fall back to a generic line.
	cleaned := msg
	if idx := strings.Index(cleaned, "request failed: "); idx >= 0 {
		cleaned = cleaned[idx+len("request failed: "):]
	}
	if i := strings.Index(cleaned, `": `); i >= 0 && strings.HasPrefix(cleaned, "Get \"") {
		cleaned = cleaned[i+3:]
	}
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return instName + " returned an error — will retry on next sync"
	}
	return instName + " returned an error: " + cleaned
}

// IsConnectionError checks if an error is a network/connection problem (instance unreachable).
//
// Patterns cover three failure shapes:
//   - Dial-time: refused, no such host, network unreachable, dial tcp.
//   - Mid-request: server closed idle connection, EOF, unexpected EOF,
//     broken pipe — Arr restarted while clonarr held an idle keep-alive
//     socket or was mid-response. Classifying these as connection errors
//     (rather than user-config errors) is what lets the pre-flight retry
//     and friendly "is not reachable" toast take over.
//   - Timeout: i/o timeout, connection reset, TLS handshake timeout,
//     Client.Timeout exceeded.
func IsConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "server closed idle connection") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected EOF") ||
		strings.HasSuffix(msg, ": EOF") ||
		strings.Contains(msg, "TLS handshake timeout") ||
		strings.Contains(msg, "Client.Timeout exceeded")
}

// autoSyncReachabilityDelays defines the wait pattern between Arr
// reachability probes at the start of an auto-sync pass. Used by
// WaitForInstanceReachable — a per-instance pre-flight check that runs
// BEFORE any sync work. Total budget = 60+60+120+120+240+600+600 = 1800s
// = 30 minutes. The pattern starts tight (catches typical 1-3 min Arr
// restart windows fast) then opens up (rides out longer outages without
// thrashing). Once Arr responds on any probe, sync proceeds normally
// with no retry inside individual rule operations.
var autoSyncReachabilityDelays = []time.Duration{
	0, // first probe is immediate
	60 * time.Second,
	60 * time.Second,
	120 * time.Second,
	120 * time.Second,
	240 * time.Second,
	600 * time.Second,
	600 * time.Second,
}

// WaitForInstanceReachable probes the Arr instance with backoff until
// either it responds or the 30-minute budget is exhausted. Returns true
// on first successful probe, false if every probe failed. Honors
// app.ShutdownCh so graceful shutdown isn't blocked. Single notification
// + LastSyncError lives in the caller; this helper just answers
// "is this Arr reachable right now (eventually)?".
//
// Replaces the previous per-rule retry-with-backoff approach inside
// BuildSyncPlan/ExecuteSyncPlan. By gating the entire pass on a single
// pre-flight check per instance, we avoid the 21-rules × 10-min-each
// alert-spam scenario where each rule independently exhausts its own
// retry chain over hours.
func (app *App) WaitForInstanceReachable(inst Instance) bool {
	for i, delay := range autoSyncReachabilityDelays {
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-app.ShutdownCh:
				log.Printf("Auto-sync: %s reachability check aborted by shutdown signal", inst.Name)
				return false
			}
		}
		client := arr.NewArrClient(inst.URL, inst.APIKey, app.HTTPClient)
		if _, err := client.TestConnection(); err == nil {
			if i > 0 {
				log.Printf("Auto-sync: %s reachable on probe %d/%d — proceeding with sync", inst.Name, i+1, len(autoSyncReachabilityDelays))
				app.DebugLog.Logf(LogAutoSync, "Reachability: %s reachable on probe %d/%d", inst.Name, i+1, len(autoSyncReachabilityDelays))
			}
			return true
		}
		if i < len(autoSyncReachabilityDelays)-1 {
			next := autoSyncReachabilityDelays[i+1]
			log.Printf("Auto-sync: %s unreachable (probe %d/%d) — next probe in %v", inst.Name, i+1, len(autoSyncReachabilityDelays), next)
			app.DebugLog.Logf(LogAutoSync, "Reachability: %s unreachable on probe %d/%d, next in %v", inst.Name, i+1, len(autoSyncReachabilityDelays), next)
		}
	}
	return false
}

// ExpandSelectedCFsForBrandNewGroups returns rule.SelectedCFs augmented
// with CFs from default-on cf-groups that are BRAND NEW since the rule's
// last successful sync. A group is "brand new" if its trash_id is NOT in
// rule.PriorAvailableGroups but it currently applies to the rule's
// profile (via quality_profiles.include) AND has Default == "true".
//
// Why: when TRaSH moves CFs from profile.formatItems into a new default-on
// cf-group (e.g. May 2026 French Unwanted restructure), the rule's saved
// SelectedCFs doesn't carry those CFs (they were implicit via formatItems,
// not explicitly selected). Without expansion, sync builds a plan that
// drops those CFs → Arr resets their scores to 0. With expansion, the
// new group's required+default CFs are included so the sync output matches
// pre-restructure behavior.
//
// ComputeRuleCounts derives the editor's "Override mode · N changes" +
// "Optional CFs: M" totals for a rule. Lazy-computed at /api/auto-sync/
// rules-fetch time so the sync-rules-list badge always agrees with what
// the user sees in the Profile Detail editor for the same rule (single
// source of truth — no stale-on-disk fields, no migration needed).
//
// Mirrors frontend's pdOverrideSummary + pdGroupOptionalCount:
//
//   - overrides = profile-level changes (general + quality + per-CF score
//     overrides + extras). Walks rule.Overrides, rule.QualityStructure,
//     rule.QualityOverrides, and rule.ScoreOverrides directly.
//   - optional  = TRaSH-blessed activations outside profile defaults.
//     Walks the profile's cf-groups (via ProfileDetailData) and counts
//     non-required CFs whose selected state diverges from cf.Default.
//     For groups with no optional members (single-required-CF default-OFF
//     groups like HDR Formats HDR / DV Boost), counts the group toggle
//     as the only signal.
//
// detail may be nil when ProfileDetailData fails (missing profile in TRaSH
// data); in that case optional = 0. overrides is always computable from
// rule alone.
func ComputeRuleCounts(rule AutoSyncRule, detail *ProfileDetailResult) (overrides, optional int) {
	// --- Overrides ---
	if rule.Overrides != nil {
		ov := rule.Overrides
		if ov.Language != nil {
			overrides++
		}
		if ov.MinFormatScore != nil {
			overrides++
		}
		if ov.MinUpgradeFormatScore != nil {
			overrides++
		}
		if ov.CutoffFormatScore != nil {
			overrides++
		}
		if ov.UpgradeAllowed != nil {
			overrides++
		}
		if ov.CutoffQuality != nil && *ov.CutoffQuality != "" {
			overrides++
		}
	}
	overrides += len(rule.QualityStructure)
	overrides += len(rule.QualityOverrides)
	for tid := range rule.ScoreOverrides {
		// Both TRaSH-base score overrides and Additional CFs (custom: prefix)
		// count toward the total — matches frontend's general+quality+
		// cfScores+extraCFs sum.
		_ = tid
		overrides++
	}

	// --- Optional ---
	if detail == nil {
		return overrides, 0
	}
	ruleSet := make(map[string]bool, len(rule.SelectedCFs))
	for _, tid := range rule.SelectedCFs {
		ruleSet[tid] = true
	}
	for _, group := range detail.Groups {
		anyInRule := false
		hasOptionalMembers := false
		for _, cf := range group.CFs {
			if !cf.Required {
				hasOptionalMembers = true
			}
			if ruleSet[cf.TrashID] {
				anyInRule = true
			}
		}
		// Per-CF: count non-required CFs that diverge from default.
		for _, cf := range group.CFs {
			if cf.Required {
				continue
			}
			cur := ruleSet[cf.TrashID]
			def := false
			if group.DefaultEnabled {
				def = cf.Default
			}
			if cur != def {
				optional++
			}
		}
		// Group-toggle: only count when group has no optional members
		// (otherwise per-CF count already reflects activation).
		if !hasOptionalMembers {
			grpOn := group.DefaultEnabled
			if !group.DefaultEnabled && anyInRule {
				grpOn = true
			} else if group.DefaultEnabled && !anyInRule && len(group.CFs) > 0 {
				grpOn = false
			}
			if grpOn != group.DefaultEnabled {
				optional++
			}
		}
	}
	return overrides, optional
}

// The second return value is the list of newly-added CF trash_ids,
// empty if no expansion was needed. Callers persist these back to
// rule.SelectedCFs after a successful sync so subsequent syncs (when
// the group is no longer brand-new) keep including them.
func ExpandSelectedCFsForBrandNewGroups(rule AutoSyncRule, ad *AppData) ([]string, []string) {
	if ad == nil || rule.TrashProfileID == "" {
		return rule.SelectedCFs, nil
	}
	// Conservative: only expand when we have a confirmed snapshot from a
	// prior sync. Without one we can't distinguish "rule never synced
	// (user's create-rule UI choices are authoritative — they may have
	// explicitly opted out of a default-on group)" from "TRaSH added a
	// new group since last sync". Skipping expansion in the unsafe case
	// means a user with a dead LastSyncCommit might still see the
	// reset-bug after a structural restructure — they fix it via UI
	// toggle. That's the lesser evil compared to silently overriding
	// an explicit opt-out.
	if len(rule.PriorAvailableGroups) == 0 {
		return rule.SelectedCFs, nil
	}
	expanded := append([]string{}, rule.SelectedCFs...)
	seen := make(map[string]bool, len(expanded))
	for _, tid := range expanded {
		seen[tid] = true
	}
	var added []string

	for _, group := range ad.CFGroups {
		if group.Default != "true" {
			continue // not a default-on group; user must opt-in explicitly
		}
		applies := false
		for _, profTID := range group.QualityProfiles.Include {
			if profTID == rule.TrashProfileID {
				applies = true
				break
			}
		}
		if !applies {
			continue
		}
		// Skip groups that already existed at the last successful sync.
		// If user had a chance to toggle and didn't enable, that's an
		// implicit opt-out — we respect it.
		if rule.PriorAvailableGroups != nil {
			if _, existed := rule.PriorAvailableGroups[group.TrashID]; existed {
				continue
			}
		}
		// Brand-new default-on group. Add its required+default CFs.
		for _, cf := range group.CustomFormats {
			isDefault := cf.Default != nil && *cf.Default
			if !cf.Required && !isDefault {
				continue
			}
			if !seen[cf.TrashID] {
				expanded = append(expanded, cf.TrashID)
				seen[cf.TrashID] = true
				added = append(added, cf.TrashID)
			}
		}
	}
	return expanded, added
}

// MigratePriorAvailableGroups scans all rules and populates
// PriorAvailableGroups for any rule that has a LastSyncCommit but no
// snapshot yet. Computes the snapshot retroactively by reading the
// trash-guides repo at the rule's LastSyncCommit. Idempotent — rules
// already carrying a snapshot are skipped.
//
// Why: restoreFromSyncHistory uses PriorAvailableGroups to distinguish
// "user explicitly opted out of an existing group" from "group is
// brand new since last sync". Without this migration, pre-fix rules
// hit by TRaSH structural restructures (CFs moving from formatItems
// into a new default-on cf-group) would have the new group auto-set
// to false, silently zeroing the user's previous CF blocks.
func (app *App) MigratePriorAvailableGroups() {
	cfg := app.Config.Get()
	if len(cfg.AutoSync.Rules) == 0 {
		return
	}

	// Cache by commit hash — many rules typically share the same
	// LastSyncCommit, so we git-read each commit's groups only once.
	type commitGroupLookup struct {
		groups map[string]CommitGroupInfo
		ok     bool
	}
	commitCache := make(map[string]commitGroupLookup)

	var migrated int
	for _, rule := range cfg.AutoSync.Rules {
		if rule.PriorAvailableGroups != nil {
			continue // already migrated
		}
		if rule.LastSyncCommit == "" {
			continue // never successfully synced — nothing to migrate from
		}
		if rule.TrashProfileID == "" {
			continue // imported profile — group resolution is a separate path
		}

		lookup, cached := commitCache[rule.LastSyncCommit]
		if !cached {
			groups, ok := app.Trash.GroupsAtCommit(rule.LastSyncCommit)
			lookup = commitGroupLookup{groups: groups, ok: ok}
			commitCache[rule.LastSyncCommit] = lookup
		}
		if !lookup.ok {
			continue // repo/commit unavailable; leave nil so migration can retry later
		}

		// Filter to groups that included this rule's profile at that commit.
		snapshot := make(map[string]bool)
		for groupTID, info := range lookup.groups {
			for _, profTID := range info.Includes {
				if profTID == rule.TrashProfileID {
					snapshot[groupTID] = info.DefaultEnabled
					break
				}
			}
		}

		// Save. Even an empty snapshot from a successful commit lookup is
		// recorded (as empty map) so we don't repeat the migration on every
		// pull tick — the empty map itself signals "we've looked, found
		// nothing relevant".
		ruleID := rule.ID
		app.Config.Update(func(cfg *Config) {
			for i := range cfg.AutoSync.Rules {
				if cfg.AutoSync.Rules[i].ID == ruleID {
					if cfg.AutoSync.Rules[i].PriorAvailableGroups == nil {
						cfg.AutoSync.Rules[i].PriorAvailableGroups = snapshot
					}
					return
				}
			}
		})
		migrated++
	}

	if migrated > 0 {
		log.Printf("Migration: populated PriorAvailableGroups for %d rule(s) from LastSyncCommit", migrated)
	}
}

// RefreshLatestSyncHistorySelectedCFs updates the latest sync history
// entry's SelectedCFs map for an instance+profile pair. Used when the
// auto-sync no-changes path runs but ExpandSelectedCFsForBrandNewGroups
// added CFs — the rule's SelectedCFs has been updated, but the existing
// sync history entry's SelectedCFs is now stale. Frontend's
// restoreFromSyncHistory uses sync history's SelectedCFs (not the rule's)
// to drive the UI's group/CF toggle state, so without this refresh it
// would mark the expanded CFs as inactive and Dry Run would propose
// resetting their scores to 0. We update in-place rather than writing
// a new history entry to avoid bloating history with no-op events.
func (app *App) RefreshLatestSyncHistorySelectedCFs(instanceID string, arrProfileID int, selectedCFMap map[string]bool) {
	app.Config.Update(func(cfg *Config) {
		latestIdx := -1
		var latestTime string
		for i := range cfg.SyncHistory {
			sh := &cfg.SyncHistory[i]
			if sh.InstanceID != instanceID || sh.ArrProfileID != arrProfileID {
				continue
			}
			if sh.LastSync > latestTime {
				latestTime = sh.LastSync
				latestIdx = i
			}
		}
		if latestIdx == -1 {
			return // no entry to refresh
		}
		cfg.SyncHistory[latestIdx].SelectedCFs = selectedCFMap
	})
}

// UpdateAutoSyncRuleSelectedCFs persists a rule's SelectedCFs list. Used
// after ExpandSelectedCFsForBrandNewGroups added CFs from a brand-new
// default-on cf-group; we save the expanded set so subsequent syncs (when
// the group is no longer brand-new from PriorAvailableGroups' perspective)
// continue to include those CFs.
func (app *App) UpdateAutoSyncRuleSelectedCFs(ruleID string, selectedCFs []string) {
	app.Config.Update(func(cfg *Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				cfg.AutoSync.Rules[i].SelectedCFs = selectedCFs
				return
			}
		}
	})
}

// updateAutoSyncRuleCommit updates the last sync commit and clears error for a rule.
// priorGroups is the snapshot of cf-groups available for this rule's profile at
// this sync — written so future loads can distinguish "user opted out of an
// existing group" from "group is brand new since last sync". Pass nil to leave
// the snapshot unchanged (e.g. when the caller hasn't computed it).
func (app *App) UpdateAutoSyncRuleCommit(ruleID, commit string, priorGroups map[string]bool) {
	app.Config.Update(func(cfg *Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				cfg.AutoSync.Rules[i].LastSyncCommit = commit
				cfg.AutoSync.Rules[i].LastSyncTime = time.Now().Format(time.RFC3339)
				cfg.AutoSync.Rules[i].LastSyncError = ""
				if priorGroups != nil {
					cfg.AutoSync.Rules[i].PriorAvailableGroups = priorGroups
				}
				return
			}
		}
	})
}

// ComputeAvailableGroups returns map of cf-group trash_id → group.Default == "true"
// for cf-groups that include profileTrashID in their quality_profiles.include
// list. Used at sync time to snapshot rule state into PriorAvailableGroups.
// Frontend then uses presence/absence of a group's trash_id in this map to
// distinguish "group existed at last sync" (use existing heuristic) from
// "group is brand new since last sync" (use group's defaultEnabled).
func ComputeAvailableGroups(ad *AppData, profileTrashID string) map[string]bool {
	if ad == nil || profileTrashID == "" {
		return nil
	}
	result := make(map[string]bool)
	for _, group := range ad.CFGroups {
		for _, profTID := range group.QualityProfiles.Include {
			if profTID == profileTrashID {
				result[group.TrashID] = group.Default == "true"
				break
			}
		}
	}
	return result
}

// updateAutoSyncRuleError sets the last error for a rule (does NOT update commit).
func (app *App) UpdateAutoSyncRuleError(ruleID, errMsg string) {
	app.Config.Update(func(cfg *Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				cfg.AutoSync.Rules[i].LastSyncError = errMsg
				cfg.AutoSync.Rules[i].LastSyncTime = time.Now().Format(time.RFC3339)
				return
			}
		}
	})
}

// NotifyAutoSync sends notifications for an auto-sync result.
// Dispatches to agents based on the outcome:
//   - syncErr != nil: sends to agents with OnSyncFailure enabled (red/critical).
//   - syncErr == nil: sends to agents with OnSyncSuccess enabled (green/info).
//
// The notification body includes instance name, profile name, and a detailed
// breakdown of CFs created/updated, score changes, quality changes, and
// settings changes. Messages are truncated at ~1800 chars to respect provider limits.
func (app *App) NotifyAutoSync(rule AutoSyncRule, inst Instance, profileName string, result *SyncResult, syncErr error) {
	cfg := app.Config.Get()

	var color int
	severity := NotificationSeverityInfo
	var title, description string

	if syncErr != nil {
		color = 0xf85149 // red
		severity = NotificationSeverityCritical
		title = "Auto-Sync Failed"
		description = fmt.Sprintf("**Instance:** %s\n**Profile:** %s\n**Error:** %s",
			inst.Name, profileName, syncErr.Error())
	} else {
		color = 0x3fb950 // green
		title = "Auto-Sync Applied"
		arrName := ""
		if result != nil && result.ArrProfileName != "" && result.ArrProfileName != profileName {
			arrName = " → " + result.ArrProfileName
		}
		description = fmt.Sprintf("**Instance:** %s\n**Profile:** %s%s", inst.Name, profileName, arrName)
		if result.CFsCreated > 0 || result.CFsUpdated > 0 {
			description += fmt.Sprintf("\n**CFs:** %d created, %d updated", result.CFsCreated, result.CFsUpdated)
			for _, d := range result.CFDetails {
				if len(description) > 1800 {
					description += "\n- ..."
					break
				}
				description += "\n- " + d
			}
		}
		if result.ScoresUpdated > 0 || result.ScoresZeroed > 0 {
			parts := []string{}
			if result.ScoresUpdated > 0 {
				parts = append(parts, fmt.Sprintf("%d updated", result.ScoresUpdated))
			}
			if result.ScoresZeroed > 0 {
				parts = append(parts, fmt.Sprintf("%d reset to 0", result.ScoresZeroed))
			}
			description += fmt.Sprintf("\n**Scores:** %s", strings.Join(parts, ", "))
			for _, d := range result.ScoreDetails {
				if len(description) > 1800 {
					description += "\n- ..."
					break
				}
				description += "\n- " + d
			}
		}
		if result.QualityUpdated {
			description += "\n**Quality:** Profile quality items updated"
			for _, d := range result.QualityDetails {
				if len(description) > 1800 {
					description += "\n- ..."
					break
				}
				description += "\n- " + d
			}
		}
		if len(result.SettingsDetails) > 0 {
			description += "\n**Settings:**"
			for _, d := range result.SettingsDetails {
				description += "\n- " + d
			}
		}
		if result.CFsCreated == 0 && result.CFsUpdated == 0 && result.ScoresUpdated == 0 && result.ScoresZeroed == 0 && !result.QualityUpdated && len(result.SettingsDetails) == 0 {
			return // no actual changes — skip notification
		}
	}

	fullTitle := "Clonarr: " + title
	payload := NotificationPayload{
		Title:    fullTitle,
		Message:  description,
		Color:    color,
		Severity: severity,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if syncErr != nil && !agent.Events.OnSyncFailure {
			continue
		}
		if syncErr == nil && !agent.Events.OnSyncSuccess {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
}

// NotifyRepoUpdate sends notifications when the TRaSH Guides repository has
// new commits. Includes the commit range and, when available, a human-readable
// diff summary of changed CFs/profiles/groups from the stored pull diff.
// Dispatches to agents with OnRepoUpdate enabled. Uses RouteUpdates so providers
// with dual-channel support (Discord) deliver to the updates channel.
func (app *App) NotifyRepoUpdate(prevCommit, newCommit string) {
	cfg := app.Config.Get()

	description := fmt.Sprintf("**Commit:** `%s` → `%s`", prevCommit, newCommit)
	status := app.Trash.Status()
	if status.LastDiff != nil && status.LastDiff.Summary != "" {
		description += "\n" + status.LastDiff.Summary
	}

	title := "Clonarr: TRaSH Guides Updated"
	payload := NotificationPayload{
		Title:    title,
		Message:  description,
		Color:    0x58a6ff,
		Severity: NotificationSeverityInfo,
		Route:    NotificationRouteUpdates,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnRepoUpdate {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
	log.Printf("Repo update: notifications dispatched (%s → %s)", prevCommit, newCommit)
}

// NotifyChangelog sends notifications when updates.txt has a new weekly date
// section from TRaSH Guides. Builds platform-specific message bodies:
//   - Discord/Pushover: inline bold entries with emoji icons and PR links.
//   - Gotify: markdown bullet list for proper line-break rendering.
//
// Uses TypeMessages to override the Gotify body while keeping the default
// for other providers. Dispatches to agents with OnChangelog enabled.
func (app *App) NotifyChangelog(section ChangelogSection) {
	cfg := app.Config.Get()

	// Build Discord/Pushover description
	discordMsg := fmt.Sprintf("**Week of %s** — %d changes", section.Date, len(section.Entries))
	for _, e := range section.Entries {
		icon := "🔧"
		if e.Type == "feat" {
			icon = "✨"
		} else if e.Type == "fix" {
			icon = "🐛"
		} else if e.Type == "refactor" {
			icon = "♻️"
		}
		line := fmt.Sprintf("\n%s **%s:** %s", icon, e.Scope, e.Msg)
		if e.PR != "" {
			line += fmt.Sprintf(" ([#%s](https://github.com/TRaSH-Guides/Guides/pull/%s))", e.PR, e.PR)
		}
		if len(discordMsg) > 1800 {
			discordMsg += "\n- ..."
			break
		}
		discordMsg += line
	}

	// Gotify uses markdown bullet list for proper line breaks
	gotifyMsg := fmt.Sprintf("**Week of %s** — %d changes\n\n", section.Date, len(section.Entries))
	for _, e := range section.Entries {
		icon := "🔧"
		if e.Type == "feat" {
			icon = "✨"
		} else if e.Type == "fix" {
			icon = "🐛"
		} else if e.Type == "refactor" {
			icon = "♻️"
		}
		line := fmt.Sprintf("- %s **%s:** %s", icon, e.Scope, e.Msg)
		if e.PR != "" {
			line += fmt.Sprintf(" [#%s](https://github.com/TRaSH-Guides/Guides/pull/%s)", e.PR, e.PR)
		}
		if len(gotifyMsg) > 1800 {
			gotifyMsg += "\n- ..."
			break
		}
		gotifyMsg += line + "\n"
	}

	title := "Clonarr: TRaSH Weekly Changelog"
	payload := NotificationPayload{
		Title:        title,
		Message:      discordMsg,
		TypeMessages: map[string]string{"gotify": gotifyMsg},
		Color:        0xd29922,
		Severity:     NotificationSeverityWarning,
		Route:        NotificationRouteUpdates,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnChangelog {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
	log.Printf("Changelog: notifications dispatched (week of %s)", section.Date)
}
