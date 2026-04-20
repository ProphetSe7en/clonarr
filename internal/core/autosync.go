package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"clonarr/internal/arr"
	"clonarr/internal/utils"
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
		InstanceID:     rule.InstanceID,
		ProfileTrashID: rule.TrashProfileID,
		ArrProfileID:   rule.ArrProfileID,
		SelectedCFs:    rule.SelectedCFs,
		ScoreOverrides:   rule.ScoreOverrides,
		QualityOverrides: rule.QualityOverrides,
		QualityStructure: rule.QualityStructure,
		Behavior:       rule.Behavior,
		Overrides:      rule.Overrides,
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
			if p := findProfile(ad, rule.TrashProfileID); p != nil { profileName = p.Name }
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
		if p := findProfile(ad, rule.TrashProfileID); p != nil { profileName = p.Name }
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
		InstanceID:        inst.ID,
		InstanceType:      inst.Type,
		ProfileTrashID:    req.ProfileTrashID,
		ImportedProfileID: req.ImportedProfileID,
		ProfileName:       plan.ProfileName,
		ArrProfileID:   req.ArrProfileID,
		ArrProfileName: plan.ArrProfileName,
		SyncedCFs:      allCFIDs,
		SelectedCFs:    selectedCFMap,
		ScoreOverrides:   rule.ScoreOverrides,
		QualityOverrides: rule.QualityOverrides,
		QualityStructure: rule.QualityStructure,
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

// cleanupStaleRules removes auto-sync rules and sync history for Arr profiles that no longer exist.
// Only acts on instances that are reachable — unreachable instances are skipped (never deletes on connection error).
func (app *App) CleanupStaleRules() {
	cfg := app.Config.Get()
	instNames := make(map[string]string) // instanceID → name

	// Build set of valid Arr profile IDs per reachable instance
	validProfiles := make(map[string]map[int]bool) // instanceID → set of arrProfileIDs
	for _, inst := range cfg.Instances {
		instNames[inst.ID] = inst.Name
		client := arr.NewArrClient(inst.URL, inst.APIKey, app.HTTPClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			app.DebugLog.Logf(LogAutoSync, "Startup cleanup: skipping %s — not reachable: %v", inst.Name, err)
			continue // instance unreachable — do NOT remove any rules
		}
		if len(profiles) == 0 {
			log.Printf("Cleanup: skipping %s — returned 0 profiles (instance may still be starting)", inst.Name)
			app.DebugLog.Logf(LogAutoSync, "Startup cleanup: skipping %s — 0 profiles returned, likely still starting", inst.Name)
			continue // safety: Arr may be starting up and not yet serving profiles
		}
		ids := make(map[int]bool)
		for _, p := range profiles {
			ids[p.ID] = true
		}
		validProfiles[inst.ID] = ids
	}

	// Remove stale rules and sync history, collect events
	var events []CleanupEvent
	app.Config.Update(func(cfg *Config) {
		cleaned := make([]AutoSyncRule, 0, len(cfg.AutoSync.Rules))
		for _, r := range cfg.AutoSync.Rules {
			valid, ok := validProfiles[r.InstanceID]
			if ok && !valid[r.ArrProfileID] {
				log.Printf("Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, instNames[r.InstanceID])
				app.DebugLog.Logf(LogAutoSync, "Startup cleanup: removing auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, instNames[r.InstanceID])
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
				app.DebugLog.Logf(LogAutoSync, "Startup cleanup: removing sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, instNames[h.InstanceID])
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

// sendDiscord sends a Discord embed to the given webhook URL.
func (app *App) sendDiscord(webhook, title, description string, color int) error {
	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"footer":      map[string]string{"text": "Clonarr " + app.Version + " by ProphetSe7en"},
	}
	payload, err := json.Marshal(map[string]any{"embeds": []any{embed}})
	if err != nil {
		return err
	}
	resp, err := app.SafeClient.Post(webhook, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned %d", resp.StatusCode)
	}
	return nil
}

// sendGotify sends a Gotify push notification with the given severity level.
// level: "critical", "warning", or "info"
// agent provides the credentials and priority configuration.
func (app *App) sendGotify(agent NotificationAgent, title, message, level string) {
	nc := agent.Config
	if nc.GotifyURL == "" || nc.GotifyToken == "" {
		return
	}

	var priority int
	switch level {
	case "critical":
		if !nc.GotifyPriorityCritical {
			return
		}
		if nc.GotifyCriticalValue != nil {
			priority = *nc.GotifyCriticalValue
		}
	case "warning":
		if !nc.GotifyPriorityWarning {
			return
		}
		if nc.GotifyWarningValue != nil {
			priority = *nc.GotifyWarningValue
		}
	default:
		if !nc.GotifyPriorityInfo {
			return
		}
		if nc.GotifyInfoValue != nil {
			priority = *nc.GotifyInfoValue
		}
	}

	// Ensure markdown renders properly in Gotify:
	// - Double newline before bold headers for paragraph break
	// - Double newline before list items for proper list rendering
	msg := message
	msg = strings.ReplaceAll(msg, "\n**", "\n\n**")
	msg = strings.ReplaceAll(msg, "\n- ", "\n\n- ")
	// Clean up any triple+ newlines from double-replacing
	for strings.Contains(msg, "\n\n\n") {
		msg = strings.ReplaceAll(msg, "\n\n\n", "\n\n")
	}

	payload := map[string]any{
		"title":    title,
		"message":  msg,
		"priority": priority,
		"extras": map[string]any{
			"client::display": map[string]string{
				"contentType": "text/markdown",
			},
		},
	}
	body, _ := json.Marshal(payload)
	gotifyURL := strings.TrimRight(nc.GotifyURL, "/") + "/message?token=" + url.QueryEscape(nc.GotifyToken)
	resp, err := app.NotifyClient.Post(gotifyURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Gotify: send failed: %v", err)
		return
	}
	resp.Body.Close()
}

// sendPushover sends a Pushover push notification at normal priority.
func (app *App) sendPushover(agent NotificationAgent, title, message string) {
	nc := agent.Config
	if nc.PushoverUserKey == "" || nc.PushoverAppToken == "" {
		return
	}

	payload := map[string]any{
		"token":    nc.PushoverAppToken,
		"user":     nc.PushoverUserKey,
		"title":    title,
		"message":  message,
		"priority": 0, // normal priority
	}
	body, _ := json.Marshal(payload)
	resp, err := app.SafeClient.Post("https://api.pushover.net/1/messages.json", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Printf("Pushover: send failed: %v", err)
		return
	}
	resp.Body.Close()
}

// dispatchNotification fires the appropriate send function for one agent.
// discordMsg and gotifyMsg allow per-provider message formatting.
// discordWebhook overrides which webhook to use (for updates/changelog events).
func (app *App) dispatchNotification(agent NotificationAgent, title, discordMsg, gotifyMsg string, color int, discordWebhook string) {
	if !agent.Enabled {
		return
	}
	switch agent.Type {
	case "discord":
		webhook := discordWebhook
		if webhook == "" {
			webhook = agent.Config.DiscordWebhook
		}
		if webhook == "" {
			return
		}
		if err := app.sendDiscord(webhook, title, discordMsg, color); err != nil {
			log.Printf("Discord: send failed: %v", err)
		}
	case "gotify":
		// Map color to Gotify level
		level := "info"
		if color == 0xf85149 { // red = failure/critical
			level = "critical"
		} else if color == 0xd29922 { // amber = warning/cleanup
			level = "warning"
		}
		utils.SafeGo("notify-gotify", func() { app.sendGotify(agent, title, gotifyMsg, level) })
	case "pushover":
		utils.SafeGo("notify-pushover", func() { app.sendPushover(agent, title, discordMsg) })
	}
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
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnCleanup {
			continue
		}
		app.dispatchNotification(agent, title, description, description, 0xd29922, "")
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
	var title, description string

	if syncErr != nil {
		color = 0xf85149 // red
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
				if len(description) > 1800 { description += "\n- ..."; break }
				description += "\n- " + d
			}
		}
		if result.ScoresUpdated > 0 || result.ScoresZeroed > 0 {
			parts := []string{}
			if result.ScoresUpdated > 0 { parts = append(parts, fmt.Sprintf("%d updated", result.ScoresUpdated)) }
			if result.ScoresZeroed > 0 { parts = append(parts, fmt.Sprintf("%d reset to 0", result.ScoresZeroed)) }
			description += fmt.Sprintf("\n**Scores:** %s", strings.Join(parts, ", "))
			for _, d := range result.ScoreDetails {
				if len(description) > 1800 { description += "\n- ..."; break }
				description += "\n- " + d
			}
		}
		if result.QualityUpdated {
			description += "\n**Quality:** Profile quality items updated"
			for _, d := range result.QualityDetails {
				if len(description) > 1800 { description += "\n- ..."; break }
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
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if syncErr != nil && !agent.Events.OnSyncFailure {
			continue
		}
		if syncErr == nil && !agent.Events.OnSyncSuccess {
			continue
		}
		app.dispatchNotification(agent, fullTitle, description, description, color, "")
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
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnRepoUpdate {
			continue
		}
		// For Discord: use the updates webhook if set, fall back to main webhook
		discordWebhook := ""
		if agent.Type == "discord" {
			discordWebhook = agent.Config.DiscordWebhookUpdates
			if discordWebhook == "" {
				discordWebhook = agent.Config.DiscordWebhook
			}
		}
		app.dispatchNotification(agent, title, description, description, 0x58a6ff, discordWebhook)
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
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnChangelog {
			continue
		}
		// For Discord: use the updates webhook if set, fall back to main webhook
		discordWebhook := ""
		if agent.Type == "discord" {
			discordWebhook = agent.Config.DiscordWebhookUpdates
			if discordWebhook == "" {
				discordWebhook = agent.Config.DiscordWebhook
			}
		}
		// Gotify gets its own formatted message; Discord/Pushover get discordMsg
		msg := discordMsg
		if agent.Type == "gotify" {
			msg = gotifyMsg
		}
		app.dispatchNotification(agent, title, msg, gotifyMsg, 0xd29922, discordWebhook)
	}
	log.Printf("Changelog: notifications dispatched (week of %s)", section.Date)
}
