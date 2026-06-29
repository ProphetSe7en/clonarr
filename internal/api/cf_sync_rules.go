package api

import (
	"log"
	"net/http"
	"sort"
	"strings"

	"clonarr/internal/core"
)

// handleCFSyncRules returns the per-CF state the Custom Formats →
// Sync Rules sub-tab renders. Output is structured to mirror the
// Profile Sync Rules tab's per-instance cards: one block per Arr
// instance carrying that instance's CFs, each CF carrying which
// profiles (TRaSH name + Arr name + Arr ID) pull it in, the rule's
// auto-sync state, and the last successful sync timestamp.
//
// Path: GET /api/cf-sync-rules/{appType}
//
// Pure derivation — no state writes — so calling this from a Check
// completion handler or from a tab-mount fetcher is cheap and safe
// to repeat. The frontend re-fetches after Apply / drift refresh.
func (s *Server) handleCFSyncRules(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("appType")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, http.StatusBadRequest, "appType must be radarr or sonarr")
		return
	}

	cfg := s.Core.Config.Get()
	appData := s.Core.Trash.GetAppData(appType)
	customs := s.Core.CustomCFs.List(appType)
	customsByID := make(map[string]core.CustomCF, len(customs))
	for _, c := range customs {
		customsByID[c.ID] = c
	}

	// Build (instanceID, arrProfileID) → ArrProfileName lookup from
	// sync history. The rule itself carries Arr ID but not the name;
	// the name was captured at last sync. Without this the UI would
	// only show "ID 4" instead of "HD-1080p (Movies) (ID 4)".
	type historyKey struct {
		InstanceID   string
		ArrProfileID int
	}
	arrNameLookup := make(map[historyKey]string)
	for _, e := range cfg.SyncHistory {
		if e.ArrProfileName == "" {
			continue
		}
		k := historyKey{InstanceID: e.InstanceID, ArrProfileID: e.ArrProfileID}
		// SyncHistory is PREPENDED (newest-first; see
		// ConfigStore.UpsertSyncHistory at config.go:1245), so the
		// FIRST iteration of the slice carries the newest name. Take
		// the first match per (instance, arrProfileID) key and skip
		// later iterations — otherwise the OLDEST name overwrites
		// every newer one and a user who renamed their profile in
		// Arr's UI keeps seeing the pre-rename label.
		if _, seen := arrNameLookup[k]; seen {
			continue
		}
		arrNameLookup[k] = e.ArrProfileName
	}

	// Per-CF category resolver. Uses TRaSH cf-group bracket prefix
	// (e.g. "[Audio Formats] Default" → parent="Audio Formats",
	// child="Default") so the sidebar can mirror Browse's hierarchy.
	cfCategory := func(tid string) (parent string, child string) {
		if strings.HasPrefix(tid, "custom:") {
			return "Custom", ""
		}
		if appData != nil {
			for _, g := range appData.CFGroups {
				for _, cfEntry := range g.CustomFormats {
					if cfEntry.TrashID == tid {
						cat, short := core.ParseCategoryPrefix(g.Name)
						if cat == "" {
							cat = "Other"
						}
						return cat, short
					}
				}
			}
		}
		return "Other", ""
	}

	type CFRowProfile struct {
		TrashProfileName string `json:"trashProfileName"`
		ArrProfileName   string `json:"arrProfileName,omitempty"`
		ArrProfileID     int    `json:"arrProfileId"`
		RuleID           string `json:"ruleId"`
		AutoSyncEnabled  bool   `json:"autoSyncEnabled"`
		LastSync         string `json:"lastSync,omitempty"`
	}
	type CFRowInstance struct {
		ID              string         `json:"id"`
		Name            string         `json:"name"`
		Drift           bool           `json:"drift"`
		UpdateAvailable bool           `json:"updateAvailable"`
		// UpdateDetails carries the per-change human-readable strings
		// from rule.PendingChanges (source="trash"). Same format as
		// Profile Sync's "Upcoming changes on next pull" panel uses.
		UpdateDetails []string       `json:"updateDetails,omitempty"`
		Profiles      []CFRowProfile `json:"profiles"`
		// Managed classifies how clonarr relates to this CF on this
		// instance. Drives the Managed/Unmanaged filter in the In use
		// sub-tab:
		//   "in-profile"     - pushed by a sync rule's profile (default)
		//   "added-directly" - pushed via the "+" button on Browse or
		//                      the Sandbox Add picker, no profile uses it
		//   "unmanaged"      - exists on Arr but clonarr never pushed it
		//                      (manual user add or another tool)
		// Drift + UpdateAvailable fire for in-profile rows, for added-
		// directly rows whose source TRaSH CF is still in the catalog,
		// AND for unmanaged rows whose name matches a known TRaSH CF
		// (recognized unmanaged). Truly unrecognized unmanaged rows
		// have no target spec, so the flags stay false.
		Managed string `json:"managed"`
		// AddedAt is the RFC3339 timestamp recorded in PushedCFs when
		// the user pushed this CF via the "+ Add to Arr" flow. Only
		// set for added-directly rows; in-profile rows derive their
		// "Last sync" timestamp from their profile entries instead.
		AddedAt string `json:"addedAt,omitempty"`
	}
	type CFRow struct {
		TrashID     string           `json:"trashId"`
		Name        string           `json:"name"`
		Category    string           `json:"category"`
		Subcategory string           `json:"subcategory,omitempty"`
		Instances   []*CFRowInstance `json:"instances"`
	}

	rowsByTID := make(map[string]*CFRow)
	// Per-row helper: get-or-create the per-instance block.
	getInstanceBlock := func(row *CFRow, inst core.Instance) *CFRowInstance {
		for _, b := range row.Instances {
			if b.ID == inst.ID {
				return b
			}
		}
		block := &CFRowInstance{
			ID:      inst.ID,
			Name:    inst.Name,
			Drift:   false,
			Managed: "in-profile",
		}
		row.Instances = append(row.Instances, block)
		return block
	}

	// Per-app-type instance lookup. Filtering rules by instance type
	// happens here so cross-app data doesn't leak into a Radarr view.
	instByID := make(map[string]core.Instance, len(cfg.Instances))
	for _, inst := range cfg.Instances {
		if inst.Type != appType {
			continue
		}
		instByID[inst.ID] = inst
	}

	for _, rule := range cfg.AutoSync.Rules {
		// OrphanedAt hides a rule (Arr profile gone). Enabled=false
		// is NOT a filter — paused rules are still configured and
		// their CFs still belong to clonarr's saved spec; users need
		// to see them to act on drift.
		if rule.OrphanedAt != "" {
			continue
		}
		inst, ok := instByID[rule.InstanceID]
		if !ok {
			continue
		}

		// Find the TRaSH profile referenced by this rule.
		var profile *core.TrashQualityProfile
		if appData != nil {
			for _, p := range appData.Profiles {
				if p.TrashID == rule.TrashProfileID {
					profile = p
					break
				}
			}
		}
		if profile == nil {
			continue
		}

		// Effective CF set = TRaSH defaults + explicit opt-ins -
		// explicit opt-outs. Mirrors runCFSpecDriftPass exactly so
		// detection, view, and Apply share one "managed" definition.
		managedTIDs := core.ComputeTrashDefaults(profile, appData)
		for _, tid := range rule.SelectedCFs {
			managedTIDs[tid] = true
		}
		excluded := make(map[string]bool, len(rule.ExcludedCFs))
		for _, tid := range rule.ExcludedCFs {
			excluded[tid] = true
		}

		// Resolve the Arr profile name from history. Falls back to
		// the TRaSH profile name when the rule has never synced
		// (LastSync empty + no history entry).
		arrName := arrNameLookup[historyKey{InstanceID: rule.InstanceID, ArrProfileID: rule.ArrProfileID}]

		// Index this rule's TRaSH-source pending changes by trash_id
		// so each CF row can surface "Update available" + the
		// per-change human-readable details Profile Sync already
		// shows. AffectedID shapes (see watch.go):
		//   <tid>                — generic / fallback / rename-flag
		//   <tid>:+<condName>   — added condition
		//   <tid>:-<condName>   — removed condition
		//   <tid>:~<cond>:<fld> — changed condition field
		//   <tid>:<ctx>          — score change (ctx is "default" etc)
		// Splitting on the FIRST colon recovers the trash_id. Custom
		// CFs use "custom:" prefix but they never have upstream TRaSH
		// changes, so they're absent from this index — we only care
		// about TRaSH-source entries.
		updatesByTID := make(map[string][]string)
		for _, pc := range rule.PendingChanges {
			if pc.Source != "trash" {
				continue
			}
			if !strings.HasPrefix(pc.ChangeType, "cf-") {
				continue
			}
			pcTID := pc.AffectedID
			if i := strings.Index(pcTID, ":"); i > 0 {
				pcTID = pcTID[:i]
			}
			if pcTID == "" {
				continue
			}
			updatesByTID[pcTID] = append(updatesByTID[pcTID], pc.AffectedName)
		}

		for tid := range managedTIDs {
			if excluded[tid] {
				continue
			}
			row, ok := rowsByTID[tid]
			if !ok {
				name := tid
				if appData != nil {
					if cf, ok := appData.CustomFormats[tid]; ok && cf != nil {
						name = cf.Name
					}
				}
				if c, ok := customsByID[tid]; ok {
					name = c.Name
				}
				parent, child := cfCategory(tid)
				row = &CFRow{
					TrashID:     tid,
					Name:        name,
					Category:    parent,
					Subcategory: child,
				}
				rowsByTID[tid] = row
			}
			block := getInstanceBlock(row, inst)
			// Drift carries across all rules — one rule pulling a
			// CF that has drifted on the instance is enough to flag
			// the CF on that instance.
			if _, drifted := inst.CFDriftFingerprints[tid]; drifted {
				block.Drift = true
			}
			// Same union semantic for "Update available" — TRaSH
			// upstream changes on a CF affect every rule that
			// includes it on this instance. Dedup details across
			// rules so the expand-row doesn't show the same
			// "WEB Tier 01 - added X" line N times.
			if details, has := updatesByTID[tid]; has {
				block.UpdateAvailable = true
				seen := make(map[string]bool, len(block.UpdateDetails))
				for _, d := range block.UpdateDetails {
					seen[d] = true
				}
				for _, d := range details {
					if d == "" || seen[d] {
						continue
					}
					seen[d] = true
					block.UpdateDetails = append(block.UpdateDetails, d)
				}
			}
			// Each rule contributes one Profile entry per
			// (CF, instance) pair. Dedup by ruleID in case a CF is
			// reachable via multiple paths within the same rule
			// (e.g. SelectedCFs AND defaults).
			alreadySeen := false
			for _, p := range block.Profiles {
				if p.RuleID == rule.ID {
					alreadySeen = true
					break
				}
			}
			if !alreadySeen {
				block.Profiles = append(block.Profiles, CFRowProfile{
					TrashProfileName: profile.Name,
					ArrProfileName:   arrName,
					ArrProfileID:     rule.ArrProfileID,
					RuleID:           rule.ID,
					AutoSyncEnabled:  rule.Enabled,
					LastSync:         rule.LastSyncTime,
				})
			}
		}
	}

	// Global aggregation of TRaSH-source pending changes across ALL
	// rules, keyed by trash_id. Used by the second pass to surface
	// upstream-update state on recognized unmanaged CFs: if any rule
	// (anywhere) has a pendingChange for a CF, that CF has upstream
	// updates waiting. Edge case: a CF that is in NO rule on any
	// instance will not have global aggregate entries, so unmanaged-
	// recognized rows for such CFs miss the update flag. The user can
	// still see "TRaSH update available" via the sidebar pull badge,
	// which is global.
	globalUpdatesByTID := make(map[string][]string)
	for _, rule := range cfg.AutoSync.Rules {
		if rule.OrphanedAt != "" {
			continue
		}
		for _, pc := range rule.PendingChanges {
			if pc.Source != "trash" || !strings.HasPrefix(pc.ChangeType, "cf-") {
				continue
			}
			pcTID := pc.AffectedID
			if i := strings.Index(pcTID, ":"); i > 0 {
				pcTID = pcTID[:i]
			}
			if pcTID == "" || pc.AffectedName == "" {
				continue
			}
			// Dedup by detail string within a TID across rules; same
			// commit hitting two rules produces the same detail line.
			already := false
			for _, d := range globalUpdatesByTID[pcTID] {
				if d == pc.AffectedName {
					already = true
					break
				}
			}
			if !already {
				globalUpdatesByTID[pcTID] = append(globalUpdatesByTID[pcTID], pc.AffectedName)
			}
		}
	}

	// Second pass: enrich with Arr-side CFs to surface "added-directly"
	// (managed-via-PushedCFs but not in any sync rule profile) and
	// "unmanaged" (on Arr but never pushed by clonarr) rows.
	//
	// We fetch each instance's full CF list and classify everything
	// the sync-rule pass did not already touch. Per-instance fetches
	// are sequential, best-effort: if one instance is down we still
	// return what we have for the others.
	//
	// Lookup builders for fast cross-checks within the inner loop.
	// managedNamesByInst tracks already-handled (instance, name) so we
	// don't double-render the same CF as both managed and unmanaged.
	managedNamesByInst := make(map[string]map[string]bool, len(instByID))
	for _, row := range rowsByTID {
		for _, b := range row.Instances {
			set, ok := managedNamesByInst[b.ID]
			if !ok {
				set = make(map[string]bool)
				managedNamesByInst[b.ID] = set
			}
			set[strings.ToLower(row.Name)] = true
		}
	}
	// Name -> TRaSH CF lookup. Lets us group unmanaged Arr-side CFs
	// under their proper TRaSH category when the name matches a known
	// guide, instead of dumping every external CF into a single
	// "Unmanaged" bucket. Also enables spec-diff drift detection on
	// recognized unmanaged CFs (we have a target spec to compare).
	nameToTrashCF := make(map[string]*core.TrashCF)
	if appData != nil {
		for _, cf := range appData.CustomFormats {
			if cf == nil {
				continue
			}
			// Lowercased name key so the lookup is case-insensitive.
			// Some CFs share names across app types; appData is
			// already app-type-scoped so this is safe.
			nameToTrashCF[strings.ToLower(cf.Name)] = cf
		}
	}
	// Name -> user custom CF lookup. Same purpose as nameToTrashCF
	// but for the user's own custom catalog. Unmanaged Arr CFs that
	// match a user custom by name get filed under "Custom" with the
	// custom's own user-defined category.
	nameToCustomCF := make(map[string]core.CustomCF)
	for _, c := range customs {
		nameToCustomCF[strings.ToLower(c.Name)] = c
	}
	for _, inst := range instByID {
		// Empty/missing inst already filtered upstream; defensive check
		// for the URL field guards against a config row with the API
		// key set but URL never filled in (very rare edge).
		if inst.URL == "" {
			continue
		}
		client := core.NewArrClientFor(inst, s.Core.HTTPClient)
		arrCFs, err := client.ListCustomFormats()
		if err != nil {
			log.Printf("handleCFSyncRules: instance %s (%s) ListCustomFormats failed: %v - skipping Unmanaged enrichment for this instance",
				inst.ID, inst.Name, err)
			continue
		}
		// Index PushedCFs by lowercased name for cheap reverse lookup.
		pushedByName := make(map[string]core.PushedCFRecord, len(inst.PushedCFs))
		for _, p := range inst.PushedCFs {
			pushedByName[strings.ToLower(p.Name)] = p
		}
		managedNames := managedNamesByInst[inst.ID]
		if managedNames == nil {
			managedNames = map[string]bool{}
		}
		for _, arrCF := range arrCFs {
			lower := strings.ToLower(arrCF.Name)
			if managedNames[lower] {
				continue // already covered by a sync rule profile
			}
			pushed, isPushed := pushedByName[lower]
			// Synthesise a row key so multiple instances with the same
			// Arr-side CF can share a row (mirrors managed behaviour).
			// Recognized rows (added-directly OR unmanaged-but-matches-
			// catalog) use the source TRaSH or custom ID so they
			// overlap with managed rows for the same CF on other
			// instances. Truly unrecognized rows fall back to
			// arr:<lower-name>.
			var rowKey, category, subcategory string
			managedState := "unmanaged"
			var trashSourceCF *core.TrashCF
			if isPushed {
				managedState = "added-directly"
				if pushed.TrashID != "" {
					rowKey = pushed.TrashID
					category, subcategory = cfCategory(pushed.TrashID)
					if appData != nil {
						trashSourceCF = appData.CustomFormats[pushed.TrashID]
					}
				} else if pushed.CustomCFID != "" {
					rowKey = pushed.CustomCFID
					category = "Custom"
					if c, ok := customsByID[pushed.CustomCFID]; ok {
						subcategory = c.Category
					}
				} else {
					managedState = "unmanaged"
					rowKey = "arr:" + lower
					category = "Custom"
				}
			} else if cf, ok := nameToTrashCF[lower]; ok && cf != nil {
				// Recognized as a TRaSH guide CF by name.
				trashSourceCF = cf
				rowKey = cf.TrashID
				category, subcategory = cfCategory(cf.TrashID)
			} else if c, ok := nameToCustomCF[lower]; ok {
				// Recognized as one of the user's custom CFs by name.
				rowKey = c.ID
				category = "Custom"
				subcategory = c.Category
			} else {
				// Truly unrecognized: not in TRaSH catalog and not in
				// the user's custom CF catalog either. Treat as a
				// user-made custom that lives only on the Arr side
				// (created in Radarr/Sonarr's UI without being
				// imported into clonarr). Sits under the "Custom"
				// parent in the sidebar so it groups naturally with
				// the user's other customs.
				rowKey = "arr:" + lower
				category = "Custom"
			}
			row, ok := rowsByTID[rowKey]
			if !ok {
				row = &CFRow{
					TrashID:     rowKey,
					Name:        arrCF.Name,
					Category:    category,
					Subcategory: subcategory,
				}
				rowsByTID[rowKey] = row
			}
			block := &CFRowInstance{
				ID:      inst.ID,
				Name:    inst.Name,
				Drift:   false,
				Managed: managedState,
			}
			if managedState == "added-directly" {
				block.AddedAt = pushed.AddedAt
			}
			// Spec-diff drift detection for recognized CFs (either
			// pushed-with-known-TRaSH-source OR unmanaged-but-matches-
			// TRaSH-catalog-by-name). We have a target spec; diff the
			// Arr-side spec against it and flag drift if anything
			// differs. Strip TrashScores from the disk-side copy
			// because Arr's CF API never returns scores (they live on
			// profile.formatItems instead) - leaving them in produces
			// false-positive ScoreChange entries for every CF that has
			// trash_scores on disk. Mirrors the existing drift pass.
			if trashSourceCF != nil {
				diskCopy := *trashSourceCF
				diskCopy.TrashScores = nil
				arrCopy := arrCF
				arrAsTrash := core.ArrCFToTrashCFExported(&arrCopy)
				if diff := core.DiffCFSpecs(&diskCopy, arrAsTrash); diff.HasAny() {
					block.Drift = true
				}
				// Upstream-update flag from the global pending aggregate.
				// Only fires when at least one rule somewhere has flagged
				// upstream changes for this trash_id; see the caveat on
				// globalUpdatesByTID above.
				if details, has := globalUpdatesByTID[trashSourceCF.TrashID]; has {
					block.UpdateAvailable = true
					block.UpdateDetails = append(block.UpdateDetails, details...)
				}
			}
			row.Instances = append(row.Instances, block)
		}
	}

	// Stable sort: rows by category then name; instances by name;
	// profiles by Arr profile name then TRaSH name. Deterministic
	// output keeps Alpine x-for keys stable across re-fetches so
	// expanded rows don't jump.
	out := make([]*CFRow, 0, len(rowsByTID))
	for _, row := range rowsByTID {
		sort.Slice(row.Instances, func(i, j int) bool { return row.Instances[i].Name < row.Instances[j].Name })
		for _, b := range row.Instances {
			sort.Slice(b.Profiles, func(i, j int) bool {
				if b.Profiles[i].ArrProfileName != b.Profiles[j].ArrProfileName {
					return b.Profiles[i].ArrProfileName < b.Profiles[j].ArrProfileName
				}
				return b.Profiles[i].TrashProfileName < b.Profiles[j].TrashProfileName
			})
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].Subcategory != out[j].Subcategory {
			return out[i].Subcategory < out[j].Subcategory
		}
		return out[i].Name < out[j].Name
	})

	writeJSON(w, map[string]any{
		"appType": appType,
		"items":   out,
	})
}
