package core

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"clonarr/internal/arr"
)

// autoSyncAfterPull runs after a successful TRaSH repo pull.
// For each enabled rule, checks if the repo commit changed since last sync,
// builds a dry-run plan, and applies if there are actual changes.
func (app *App) AutoSyncAfterPull() {
	// Clean up stale rules/history for Arr profiles that no longer exist
	app.CleanupStaleRules()

	cfg := app.Config.Get()
	if len(cfg.AutoSync.Rules) == 0 {
		return
	}

	currentCommit := app.Trash.CurrentCommit()
	if currentCommit == "" {
		return
	}

	for _, rule := range cfg.AutoSync.Rules {
		if !rule.Enabled {
			continue
		}
		if rule.OrphanedAt != "" {
			continue // orphaned (target profile gone from Arr) — needs Restore or Remove
		}
		if rule.ProfileSource == "imported" {
			continue // builder profiles are manual-sync only
		}
		if rule.LastSyncCommit == currentCommit {
			continue // no repo changes since last sync
		}

		app.runAutoSyncRule(rule, currentCommit)
	}
}

// runAutoSyncRule evaluates and applies a single auto-sync rule.
func (app *App) runAutoSyncRule(rule AutoSyncRule, currentCommit string) {
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
		return
	}

	inst, ok := app.Config.GetInstance(rule.InstanceID)
	if !ok {
		log.Printf("Auto-sync: skipping rule %s — instance %s not found", rule.ID, rule.InstanceID)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: skipped — instance %s not found", rule.ID, rule.InstanceID)
		return
	}

	// Per-instance mutex — skip if manual sync is running
	mu := app.GetSyncMutex(inst.ID)
	if !mu.TryLock() {
		log.Printf("Auto-sync: skipping rule %s — sync already in progress for %s", rule.ID, inst.Name)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: skipped — sync already in progress for %s", rule.ID, inst.Name)
		return
	}
	defer mu.Unlock()

	log.Printf("Auto-sync: evaluating rule %s (instance=%s, profile=%s)", rule.ID, inst.Name, rule.TrashProfileID)
	app.DebugLog.Logf(LogAutoSync, "Rule %s: evaluating %q → %s (arrProfileId=%d, overrides=%v)",
		rule.ID, rule.TrashProfileID, inst.Name, rule.ArrProfileID, rule.Overrides != nil)

	ad := app.Trash.GetAppData(inst.Type)
	if ad == nil {
		app.UpdateAutoSyncRuleError(rule.ID, "no TRaSH data for "+inst.Type)
		return
	}

	// Build sync request from rule
	req := SyncRequest{
		InstanceID:       rule.InstanceID,
		ProfileTrashID:   rule.TrashProfileID,
		ArrProfileID:     rule.ArrProfileID,
		SelectedCFs:      rule.SelectedCFs,
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
			return
		}
		imported = &p
	}

	// Dry-run plan
	customCFs := app.CustomCFs.List(inst.Type)
	lastSyncedCFs := app.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, app.HTTPClient)
	if err != nil {
		errMsg := fmt.Sprintf("plan failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, errMsg)
		app.DebugLog.Logf(LogError, "Auto-sync rule %s: plan failed: %s", rule.ID, errMsg)

		// Connection error — instance unreachable: send user-friendly message (not internal stack trace)
		if IsConnectionError(err) {
			friendlyMsg := inst.Name + " is not reachable — will retry on next sync"
			log.Printf("Auto-sync: rule %s — %s is not reachable", rule.ID, inst.Name)
			app.DebugLog.Logf(LogAutoSync, "Rule %s: %s is not reachable: %v", rule.ID, inst.Name, err)
			app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
			profileName := rule.TrashProfileID
			if p := findProfile(ad, rule.TrashProfileID); p != nil {
				profileName = p.Name
			}
			app.NotifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", friendlyMsg))
			return
		}

		app.UpdateAutoSyncRuleError(rule.ID, errMsg)
		// Auto-disable rule if Arr profile no longer exists
		if strings.Contains(err.Error(), "no longer exists") || strings.Contains(err.Error(), "not found") {
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
		app.NotifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", errMsg))
		return
	}

	if !plan.HasChanges() {
		// No actual changes — update commit hash, clear error
		log.Printf("Auto-sync: rule %s — no changes for %s", rule.ID, inst.Name)
		app.DebugLog.Logf(LogAutoSync, "Rule %s: no changes for %s", rule.ID, inst.Name)
		app.UpdateAutoSyncRuleCommit(rule.ID, currentCommit)
		return
	}

	// Apply
	result, err := ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, ResolveSyncBehavior(req.Behavior), app.HTTPClient)
	if err != nil {
		if IsConnectionError(err) {
			friendlyMsg := inst.Name + " is not reachable — will retry on next sync"
			log.Printf("Auto-sync: rule %s apply — %s is not reachable", rule.ID, inst.Name)
			app.DebugLog.Logf(LogAutoSync, "Rule %s: %s is not reachable during apply: %v", rule.ID, inst.Name, err)
			app.UpdateAutoSyncRuleError(rule.ID, friendlyMsg)
			app.NotifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", friendlyMsg))
			return
		}
		errMsg := fmt.Sprintf("apply failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, errMsg)
		app.DebugLog.Logf(LogError, "Auto-sync rule %s: apply failed: %s", rule.ID, errMsg)
		app.UpdateAutoSyncRuleError(rule.ID, errMsg)
		app.NotifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", errMsg))
		return
	}

	log.Printf("Auto-sync: rule %s applied — %d CFs created, %d updated, %d scores on %s",
		rule.ID, result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, inst.Name)
	app.DebugLog.Logf(LogAutoSync, "Rule %s: applied — %d created, %d updated, %d scores on %s",
		rule.ID, result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, inst.Name)

	app.UpdateAutoSyncRuleCommit(rule.ID, currentCommit)

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
		// Collect details for toast (max 5 lines)
		var details []string
		details = append(details, result.CFDetails...)
		details = append(details, result.ScoreDetails...)
		details = append(details, result.QualityDetails...)
		details = append(details, result.SettingsDetails...)
		if len(details) > 5 {
			details = append(details[:4], fmt.Sprintf("...and %d more", len(details)-4))
		}
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

// dispatchNotification sends one payload through one configured agent.
func (app *App) dispatchNotification(agent NotificationAgent, payload NotificationPayload) {
	app.DispatchNotificationAgent(agent, payload)
}

// notifyCleanup sends notifications for auto-cleanup events.
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

// IsConnectionError checks if an error is a network/connection problem (instance unreachable).
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
		strings.Contains(msg, "dial tcp")
}

// updateAutoSyncRuleCommit updates the last sync commit and clears error for a rule.
func (app *App) UpdateAutoSyncRuleCommit(ruleID, commit string) {
	app.Config.Update(func(cfg *Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				cfg.AutoSync.Rules[i].LastSyncCommit = commit
				cfg.AutoSync.Rules[i].LastSyncTime = time.Now().Format(time.RFC3339)
				cfg.AutoSync.Rules[i].LastSyncError = ""
				return
			}
		}
	})
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

// notifyAutoSync sends notifications for an auto-sync result.
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

// notifyRepoUpdate sends notifications when the TRaSH repo has new commits.
// Shows actual file changes (CFs, profiles, groups) from the stored pull diff.
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

// notifyChangelog sends notifications when updates.txt has a new date section.
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
