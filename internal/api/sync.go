package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"

	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// --- Sync ---

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
	customCFs := s.Core.CustomCFs.List(inst.Type)
	lastSyncedCFs := s.Core.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	plan, err := core.BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, s.Core.HTTPClient)
	if err != nil {
		log.Printf("Dry-run error for %s: %v", inst.Name, err)
		writeError(w, 400, err.Error())
		return
	}

	behavior := core.ResolveSyncBehavior(req.Behavior)
	s.Core.DebugLog.Logf(core.LogSync, "Dry-run: %q → %s | %d selected CFs | overrides: %s | behavior: %s/%s/%s",
		plan.ProfileName, inst.Name, len(req.SelectedCFs),
		core.OverrideSummary(req.Overrides), behavior.AddMode, behavior.RemoveMode, behavior.ResetMode)
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

	// C5: Only one sync per instance at a time
	mu := s.Core.GetSyncMutex(inst.ID)
	if !mu.TryLock() {
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
			writeError(w, 404, "Imported profile not found")
			return
		}
		imported = &p
	}
	customCFs := s.Core.CustomCFs.List(inst.Type)
	lastSyncedCFs := s.Core.GetLastSyncedCFs(req.InstanceID, req.ArrProfileID, req.Behavior)
	behavior := core.ResolveSyncBehavior(req.Behavior)
	plan, err := core.BuildSyncPlan(ad, inst, req, imported, customCFs, lastSyncedCFs, s.Core.HTTPClient)
	if err != nil {
		log.Printf("Apply plan error for %s: %v", inst.Name, err)
		s.Core.DebugLog.Logf(core.LogError, "Apply plan error for %s: %v", inst.Name, err)
		writeError(w, 500, "Failed to build sync plan")
		return
	}

	result, err := core.ExecuteSyncPlan(ad, inst, req, plan, imported, customCFs, behavior, s.Core.HTTPClient)
	if err != nil {
		log.Printf("Apply exec error for %s: %v", inst.Name, err)
		s.Core.DebugLog.Logf(core.LogError, "Apply exec error for %s: %v", inst.Name, err)
		writeError(w, 500, "Failed to execute sync")
		return
	}

	s.Core.DebugLog.Logf(core.LogSync, "Apply: %q → %s | arrProfileId=%d | mode=%s | %d created, %d updated, %d scores | %d errors",
		plan.ProfileName, inst.Name, req.ArrProfileID, func() string {
			if req.ArrProfileID == 0 {
				return "create"
			}
			return "update"
		}(),
		result.CFsCreated, result.CFsUpdated, result.ScoresUpdated, len(result.Errors))
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
	// Merge: cfSetDetails (from set diff) + result.CFDetails (creates/updates)
	allCFDetails := append(cfSetDetails, result.CFDetails...)
	var changes *core.SyncChanges
	if len(allCFDetails) > 0 || len(result.ScoreDetails) > 0 ||
		len(result.QualityDetails) > 0 || len(result.SettingsDetails) > 0 {
		changes = &core.SyncChanges{
			CFDetails:       allCFDetails,
			ScoreDetails:    result.ScoreDetails,
			QualityDetails:  result.QualityDetails,
			SettingsDetails: result.SettingsDetails,
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
		ScoreOverrides:    req.ScoreOverrides,
		QualityOverrides:  req.QualityOverrides,
		QualityStructure:  req.QualityStructure,
		Overrides:         req.Overrides,
		Behavior:          req.Behavior,
		CFsCreated:        result.CFsCreated,
		CFsUpdated:        result.CFsUpdated,
		ScoresUpdated:     result.ScoresUpdated,
		LastSync:          now,
		Changes:           changes,
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
	// If a rule exists but source type changed (builder↔TRaSH), update it to match
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
		cfg.AutoSync.Rules = append(cfg.AutoSync.Rules, core.AutoSyncRule{
			ID:                core.GenerateID(),
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

func (s *Server) handleSyncHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing instance ID")
		return
	}
	// Clean up stale entries for this instance before returning (only if instance is reachable)
	inst, ok := s.Core.Config.GetInstance(id)
	if ok {
		client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
		profiles, err := client.ListProfiles()
		if err != nil {
			log.Printf("Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
			s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: skipping %s — instance not reachable: %v", inst.Name, err)
		} else if len(profiles) == 0 {
			log.Printf("Cleanup: skipping %s — returned 0 profiles (instance may still be starting)", inst.Name)
			s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: skipping %s — 0 profiles returned, likely still starting", inst.Name)
		} else {
			validIDs := make(map[int]bool)
			for _, p := range profiles {
				validIDs[p.ID] = true
			}
			var events []core.CleanupEvent
			s.Core.Config.Update(func(cfg *core.Config) {
				cleanedHistory := make([]core.SyncHistoryEntry, 0, len(cfg.SyncHistory))
				for _, h := range cfg.SyncHistory {
					if h.InstanceID == id && !validIDs[h.ArrProfileID] {
						log.Printf("Cleanup: removing stale sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
						s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: removing stale sync history for %q (Arr profile %d deleted from %s)", h.ProfileName, h.ArrProfileID, inst.Name)
						events = append(events, core.CleanupEvent{
							ProfileName:  h.ProfileName,
							InstanceName: inst.Name,
							ArrProfileID: h.ArrProfileID,
							Timestamp:    time.Now().Format(time.RFC3339),
						})
						continue
					}
					cleanedHistory = append(cleanedHistory, h)
				}
				cleanedRules := make([]core.AutoSyncRule, 0, len(cfg.AutoSync.Rules))
				for _, r := range cfg.AutoSync.Rules {
					if r.InstanceID == id && !validIDs[r.ArrProfileID] && r.ArrProfileID != 0 {
						log.Printf("Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, inst.Name)
						s.Core.DebugLog.Logf(core.LogAutoSync, "Cleanup: removing stale auto-sync rule %s (Arr profile %d deleted from %s)", r.ID, r.ArrProfileID, inst.Name)
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
