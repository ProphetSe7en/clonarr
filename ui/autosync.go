package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// autoSyncAfterPull runs after a successful TRaSH repo pull.
// For each enabled rule, checks if the repo commit changed since last sync,
// builds a dry-run plan, and applies if there are actual changes.
func (app *App) autoSyncAfterPull() {
	// Clean up stale rules/history for Arr profiles that no longer exist
	app.cleanupStaleRules()

	cfg := app.config.Get()
	if len(cfg.AutoSync.Rules) == 0 {
		return
	}

	currentCommit := app.trash.CurrentCommit()
	if currentCommit == "" {
		return
	}

	for _, rule := range cfg.AutoSync.Rules {
		if !rule.Enabled {
			continue
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
	cfg := app.config.Get()
	ruleExists := false
	for _, r := range cfg.AutoSync.Rules {
		if r.ID == rule.ID && r.Enabled {
			ruleExists = true
			break
		}
	}
	if !ruleExists {
		log.Printf("Auto-sync: skipping rule %s — removed or disabled since pull started", rule.ID)
		app.debugLog.Logf(LogAutoSync, "Rule %s: skipped — removed or disabled since pull started", rule.ID)
		return
	}

	inst, ok := app.config.GetInstance(rule.InstanceID)
	if !ok {
		log.Printf("Auto-sync: skipping rule %s — instance %s not found", rule.ID, rule.InstanceID)
		return
	}

	// Per-instance mutex — skip if manual sync is running
	mu := getSyncMutex(inst.ID)
	if !mu.TryLock() {
		log.Printf("Auto-sync: skipping rule %s — sync already in progress for %s", rule.ID, inst.Name)
		return
	}
	defer mu.Unlock()

	log.Printf("Auto-sync: evaluating rule %s (instance=%s, profile=%s)", rule.ID, inst.Name, rule.TrashProfileID)
	app.debugLog.Logf(LogAutoSync, "Rule %s: evaluating %q → %s (arrProfileId=%d, overrides=%v)",
		rule.ID, rule.TrashProfileID, inst.Name, rule.ArrProfileID, rule.Overrides != nil)

	ad := app.trash.GetAppData(inst.Type)
	if ad == nil {
		app.updateAutoSyncRuleError(rule.ID, "no TRaSH data for "+inst.Type)
		return
	}

	// Build sync request from rule
	req := SyncRequest{
		InstanceID:     rule.InstanceID,
		ProfileTrashID: rule.TrashProfileID,
		ArrProfileID:   rule.ArrProfileID,
		SelectedCFs:    rule.SelectedCFs,
		Behavior:       rule.Behavior,
		Overrides:      rule.Overrides,
		ScoreOverrides: rule.ScoreOverrides,
	}
	if rule.ProfileSource == "imported" {
		req.ImportedProfileID = rule.ImportedProfileID
	}

	// Resolve imported profile if needed
	var imported *ImportedProfile
	if req.ImportedProfileID != "" {
		p, ok := app.profiles.Get(req.ImportedProfileID)
		if !ok {
			app.updateAutoSyncRuleError(rule.ID, "imported profile not found: "+req.ImportedProfileID)
			return
		}
		imported = &p
	}

	// Dry-run plan
	customCFs := app.customCFs.List(inst.Type)
	lastSyncedCFs := app.getLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs)
	if err != nil {
		errMsg := fmt.Sprintf("plan failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, errMsg)
		app.debugLog.Logf(LogError, "Auto-sync rule %s: plan failed: %s", rule.ID, errMsg)

		// Connection error — instance unreachable: send user-friendly message (not internal stack trace)
		if isConnectionError(err) {
			friendlyMsg := inst.Name + " is not reachable — will retry on next sync"
			log.Printf("Auto-sync: rule %s — %s is not reachable", rule.ID, inst.Name)
			app.updateAutoSyncRuleError(rule.ID, friendlyMsg)
			profileName := rule.TrashProfileID
			if p := findProfile(ad, rule.TrashProfileID); p != nil { profileName = p.Name }
			app.notifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", friendlyMsg))
			return
		}

		app.updateAutoSyncRuleError(rule.ID, errMsg)
		// Auto-disable rule if Arr profile no longer exists
		if strings.Contains(err.Error(), "no longer exists") || strings.Contains(err.Error(), "not found") {
			log.Printf("Auto-sync: disabling rule %s — target profile no longer exists", rule.ID)
			app.debugLog.Logf(LogAutoSync, "Rule %s: auto-disabled — target Arr profile no longer exists (ID %d)", rule.ID, rule.ArrProfileID)
			app.config.Update(func(cfg *Config) {
				for i := range cfg.AutoSync.Rules {
					if cfg.AutoSync.Rules[i].ID == rule.ID {
						cfg.AutoSync.Rules[i].Enabled = false
						return
					}
				}
			})
		}
		profileName := rule.TrashProfileID
		if p := findProfile(ad, rule.TrashProfileID); p != nil { profileName = p.Name }
		app.notifyAutoSync(rule, inst, profileName, nil, fmt.Errorf("%s", errMsg))
		return
	}

	if !plan.HasChanges() {
		// No actual changes — update commit hash, clear error
		log.Printf("Auto-sync: rule %s — no changes for %s", rule.ID, inst.Name)
		app.debugLog.Logf(LogAutoSync, "Rule %s: no changes for %s", rule.ID, inst.Name)
		app.updateAutoSyncRuleCommit(rule.ID, currentCommit)
		return
	}

	// Apply
	result, err := ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, ResolveSyncBehavior(req.Behavior))
	if err != nil {
		if isConnectionError(err) {
			friendlyMsg := inst.Name + " is not reachable — will retry on next sync"
			log.Printf("Auto-sync: rule %s apply — %s is not reachable", rule.ID, inst.Name)
			app.updateAutoSyncRuleError(rule.ID, friendlyMsg)
			app.notifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", friendlyMsg))
			return
		}
		errMsg := fmt.Sprintf("apply failed: %v", err)
		log.Printf("Auto-sync: rule %s — %s", rule.ID, errMsg)
		app.debugLog.Logf(LogError, "Auto-sync rule %s: apply failed: %s", rule.ID, errMsg)
		app.updateAutoSyncRuleError(rule.ID, errMsg)
		app.notifyAutoSync(rule, inst, plan.ProfileName, nil, fmt.Errorf("%s", errMsg))
		return
	}

	log.Printf("Auto-sync: rule %s applied — %d CFs created, %d updated, %d scores on %s",
		rule.ID, result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, inst.Name)
	app.debugLog.Logf(LogAutoSync, "Rule %s: applied — %d created, %d updated, %d scores on %s",
		rule.ID, result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, inst.Name)

	app.updateAutoSyncRuleCommit(rule.ID, currentCommit)

	// Update sync history (same as manual sync)
	allCFIDs := make([]string, 0)
	for _, a := range plan.CFActions {
		allCFIDs = append(allCFIDs, a.TrashID)
	}
	// Build selectedCFs map from rule
	selectedCFMap := make(map[string]bool, len(rule.SelectedCFs))
	for _, id := range rule.SelectedCFs {
		selectedCFMap[id] = true
	}
	entry := SyncHistoryEntry{
		InstanceID:     inst.ID,
		ProfileTrashID: req.ProfileTrashID,
		ProfileName:    plan.ProfileName,
		ArrProfileID:   req.ArrProfileID,
		ArrProfileName: plan.ArrProfileName,
		SyncedCFs:      allCFIDs,
		SelectedCFs:    selectedCFMap,
		Overrides:      rule.Overrides,
		Behavior:       rule.Behavior,
		CFsCreated:     result.CFsCreated,
		CFsUpdated:     result.CFsUpdated,
		ScoresUpdated:  result.ScoresUpdated,
		LastSync:       time.Now().Format(time.RFC3339),
	}
	if result.ProfileCreated {
		entry.ArrProfileID = result.ArrProfileID
		entry.ArrProfileName = result.ArrProfileName
		// Update rule with new Arr profile ID
		app.config.Update(func(cfg *Config) {
			for i := range cfg.AutoSync.Rules {
				if cfg.AutoSync.Rules[i].ID == rule.ID {
					log.Printf("Auto-sync: updating rule %s with new Arr profile ID %d", rule.ID, result.ArrProfileID)
					cfg.AutoSync.Rules[i].ArrProfileID = result.ArrProfileID
					return
				}
			}
		})
	}
	if err := app.config.UpsertSyncHistory(entry); err != nil {
		log.Printf("Auto-sync: failed to save sync history: %v", err)
	}

	app.notifyAutoSync(rule, inst, plan.ProfileName, result, nil)
}

// cleanupStaleRules removes auto-sync rules and sync history for Arr profiles that no longer exist.
// Only acts on instances that are reachable — unreachable instances are skipped (never deletes on connection error).
func (app *App) cleanupStaleRules() {
	cfg := app.config.Get()
	instNames := make(map[string]string) // instanceID → name

	// Build set of valid Arr profile IDs per reachable instance
	validProfiles := make(map[string]map[int]bool) // instanceID → set of arrProfileIDs
	for _, inst := range cfg.Instances {
		instNames[inst.ID] = inst.Name
		client := NewArrClient(inst.URL, inst.APIKey)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			continue // instance unreachable — do NOT remove any rules
		}
		ids := make(map[int]bool)
		for _, p := range profiles {
			ids[p.ID] = true
		}
		validProfiles[inst.ID] = ids
	}

	// Remove stale rules and sync history, collect events
	var events []CleanupEvent
	app.config.Update(func(cfg *Config) {
		cleaned := make([]AutoSyncRule, 0, len(cfg.AutoSync.Rules))
		for _, r := range cfg.AutoSync.Rules {
			valid, ok := validProfiles[r.InstanceID]
			if ok && !valid[r.ArrProfileID] {
				log.Printf("Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, instNames[r.InstanceID])
				continue
			}
			cleaned = append(cleaned, r)
		}
		cfg.AutoSync.Rules = cleaned

		cleanedHistory := make([]SyncHistoryEntry, 0, len(cfg.SyncHistory))
		for _, h := range cfg.SyncHistory {
			valid, ok := validProfiles[h.InstanceID]
			if ok && !valid[h.ArrProfileID] {
				log.Printf("Cleanup: removing stale sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, instNames[h.InstanceID])
				events = append(events, CleanupEvent{
					ProfileName:  h.ProfileName,
					InstanceName: instNames[h.InstanceID],
					ArrProfileID: h.ArrProfileID,
					Timestamp:    time.Now().Format(time.RFC3339),
				})
				continue
			}
			cleanedHistory = append(cleanedHistory, h)
		}
		cfg.SyncHistory = cleanedHistory
	})

	// Store events for frontend to pick up + send Discord notification
	if len(events) > 0 {
		app.cleanupMu.Lock()
		app.cleanupEvents = append(app.cleanupEvents, events...)
		if len(app.cleanupEvents) > 50 {
			app.cleanupEvents = app.cleanupEvents[len(app.cleanupEvents)-50:]
		}
		app.cleanupMu.Unlock()
		app.notifyCleanup(events)
	}
}

// notifyCleanup sends a Discord notification for auto-cleanup events.
func (app *App) notifyCleanup(events []CleanupEvent) {
	cfg := app.config.Get()
	webhook := cfg.AutoSync.DiscordWebhook
	if webhook == "" || !cfg.AutoSync.NotifyOnFailure {
		return
	}

	description := ""
	for _, ev := range events {
		description += fmt.Sprintf("**%s** removed from %s — profile was deleted in Arr\n", ev.ProfileName, ev.InstanceName)
	}

	embed := map[string]any{
		"title":       "Sync Rules Cleaned Up",
		"description": strings.TrimSpace(description),
		"color":       0xd29922, // amber
		"footer":      map[string]string{"text": "Clonarr " + Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Cleanup: Discord notification failed: %v", err)
		return
	}
	resp.Body.Close()
}

// isConnectionError checks if an error is a network/connection problem (instance unreachable).
func isConnectionError(err error) bool {
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
func (app *App) updateAutoSyncRuleCommit(ruleID, commit string) {
	app.config.Update(func(cfg *Config) {
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
func (app *App) updateAutoSyncRuleError(ruleID, errMsg string) {
	app.config.Update(func(cfg *Config) {
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				cfg.AutoSync.Rules[i].LastSyncError = errMsg
				cfg.AutoSync.Rules[i].LastSyncTime = time.Now().Format(time.RFC3339)
				return
			}
		}
	})
}

// notifyAutoSync sends Discord notification for auto-sync result.
func (app *App) notifyAutoSync(rule AutoSyncRule, inst Instance, profileName string, result *SyncResult, syncErr error) {
	cfg := app.config.Get()
	if cfg.AutoSync.DiscordWebhook == "" {
		return
	}
	if syncErr != nil && !cfg.AutoSync.NotifyOnFailure {
		return
	}
	if syncErr == nil && !cfg.AutoSync.NotifyOnSuccess {
		return
	}

	var color int
	var title, description string

	if syncErr != nil {
		color = 0xf85149 // red
		title = "Auto-Sync Failed"
		description = fmt.Sprintf("**Instance:** %s\n**Profile:** %s\n**Error:** %s",
			inst.Name, profileName, syncErr.Error())
	} else {
		color = 0x3fb950 // green
		title = "Auto-Sync Applied"
		description = fmt.Sprintf("**Instance:** %s\n**Profile:** %s", inst.Name, profileName)
		if result.CFsCreated > 0 || result.CFsUpdated > 0 {
			description += fmt.Sprintf("\n**CFs:** %d created, %d updated", result.CFsCreated, result.CFsUpdated)
			for _, d := range result.CFDetails {
				if len(description) > 1800 { description += "\n  - ..."; break }
				description += "\n  - " + d
			}
		}
		if result.ScoresUpdated > 0 {
			description += fmt.Sprintf("\n**Scores:** %d updated", result.ScoresUpdated)
			for _, d := range result.ScoreDetails {
				if len(description) > 1800 { description += "\n  - ..."; break }
				description += "\n  - " + d
			}
		}
		if result.QualityUpdated {
			description += "\n**Quality:** Profile quality items updated"
			for _, d := range result.QualityDetails {
				if len(description) > 1800 { description += "\n  - ..."; break }
				description += "\n  - " + d
			}
		}
		if result.CFsCreated == 0 && result.CFsUpdated == 0 && result.ScoresUpdated == 0 && !result.QualityUpdated {
			description += "\n**No changes** — profile already in sync"
		}
	}

	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"footer":      map[string]string{"text": "Clonarr " + Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		log.Printf("Auto-sync: failed to marshal Discord payload: %v", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(cfg.AutoSync.DiscordWebhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Auto-sync: Discord notification failed: %v", err)
		return
	}
	resp.Body.Close()
}

// notifyRepoUpdate sends a Discord notification when the TRaSH repo has new commits.
// Includes changelog entries from updates.txt for human-readable context.
func (app *App) notifyRepoUpdate(prevCommit, newCommit string) {
	cfg := app.config.Get()
	if !cfg.AutoSync.NotifyOnRepoUpdate {
		return
	}
	webhook := cfg.AutoSync.DiscordWebhook
	if webhook == "" {
		return
	}

	description := fmt.Sprintf("**Commit:** `%s` → `%s`", prevCommit, newCommit)

	// Build changelog from parsed updates.txt (already available after pull)
	status := app.trash.Status()
	if len(status.Changelog) > 0 {
		section := status.Changelog[0] // latest section
		description += fmt.Sprintf("\n\n**Changes** — %s", section.Date)
		for _, e := range section.Entries {
			icon := "🔧"
			if e.Type == "feat" {
				icon = "✨"
			} else if e.Type == "refactor" {
				icon = "♻️"
			}
			line := fmt.Sprintf("\n%s **%s:** %s", icon, e.Scope, e.Msg)
			if e.PR != "" {
				line += fmt.Sprintf(" ([#%s](https://github.com/TRaSH-Guides/Guides/pull/%s))", e.PR, e.PR)
			}
			description += line
		}
	}

	embed := map[string]any{
		"title":       "TRaSH Guides Updated",
		"description": description,
		"color":       0x58a6ff, // blue
		"footer":      map[string]string{"text": "Clonarr " + Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		log.Printf("Repo update: failed to marshal Discord payload: %v", err)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("Repo update: Discord notification failed: %v", err)
		return
	}
	resp.Body.Close()
	log.Printf("Repo update: Discord notification sent (%s → %s)", prevCommit, newCommit)
}
