package api

import (
	"net/http"
	"sort"
	"strings"

	"clonarr/internal/core"
)

// handleCFSyncRules returns the per-instance per-CF state that drives
// the Sync Rules → Custom Formats sub-tab. One row per (instance,
// trashId) currently managed by any enabled rule on the instance,
// annotated with the four state signals the pill renderer needs:
// drift (from Instance.CFDriftFingerprints), update available (TRaSH
// upstream advanced since last sync), used-by-profiles (derived from
// rule + profile data so the row shows "Used by SQP-3, SQP-4"), and a
// short category bucket so the default "By Group" sort has something
// to group on.
//
// Pure derivation — no state writes — so calling this from a Check
// completion handler or from a tab-mount fetcher is cheap and safe to
// repeat. The frontend re-fetches after Apply / drift refresh.
//
// Path: GET /api/cf-sync-rules/{appType}
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

	// Per-CF category lookup. Walks ad.CFGroups to find which category
	// each trash id belongs to; CFs that live directly in
	// profile.FormatItems (Required CFs without a source cf-group) fall
	// back to the "Required" bucket; customs land in "Custom".
	cfCategory := func(tid string) string {
		if strings.HasPrefix(tid, "custom:") {
			return "Custom"
		}
		if appData != nil {
			for _, g := range appData.CFGroups {
				for _, cfEntry := range g.CustomFormats {
					if cfEntry.TrashID == tid {
						cat, _ := core.ParseCategoryPrefix(g.Name)
						if cat == "" {
							cat = "Other"
						}
						return cat
					}
				}
			}
		}
		return "Other"
	}

	type ProfileRef struct {
		ProfileName string `json:"profileName"`
		RuleID      string `json:"ruleId"`
	}
	type InstanceState struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Drift           bool   `json:"drift"`
		UpdateAvailable bool   `json:"updateAvailable"`
	}
	type CFRow struct {
		TrashID        string         `json:"trashId"`
		Name           string         `json:"name"`
		Category       string         `json:"category"`
		Instances      []InstanceState `json:"instances"`
		UsedByProfiles []ProfileRef   `json:"usedByProfiles"`
		UsedByRules    []string       `json:"usedByRules"`
	}

	// Build the row map keyed by trashId. Each row aggregates state
	// across every instance the CF is managed on, plus the usage list
	// derived from rules.
	rowsByTID := make(map[string]*CFRow)

	// Pre-build per-instance lookup for the name resolution helper.
	instByID := make(map[string]core.Instance, len(cfg.Instances))
	for _, inst := range cfg.Instances {
		if inst.Type != appType {
			continue
		}
		instByID[inst.ID] = inst
	}

	for _, rule := range cfg.AutoSync.Rules {
		if !rule.Enabled || rule.OrphanedAt != "" {
			continue
		}
		inst, ok := instByID[rule.InstanceID]
		if !ok {
			continue
		}

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

		// Effective CF set = TRaSH defaults + explicit opt-ins - opt-outs.
		// Matches the detection-side derivation in cf_drift.go.
		managedTIDs := core.ComputeTrashDefaults(profile, appData)
		for _, tid := range rule.SelectedCFs {
			managedTIDs[tid] = true
		}
		excluded := make(map[string]bool, len(rule.ExcludedCFs))
		for _, tid := range rule.ExcludedCFs {
			excluded[tid] = true
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
				row = &CFRow{
					TrashID:  tid,
					Name:     name,
					Category: cfCategory(tid),
				}
				rowsByTID[tid] = row
			}
			// Drift flag for this instance.
			drifted := false
			if _, ok := inst.CFDriftFingerprints[tid]; ok {
				drifted = true
			}
			// Add or update the instance entry.
			found := false
			for i := range row.Instances {
				if row.Instances[i].ID == inst.ID {
					row.Instances[i].Drift = row.Instances[i].Drift || drifted
					found = true
					break
				}
			}
			if !found {
				row.Instances = append(row.Instances, InstanceState{
					ID:    inst.ID,
					Name:  inst.Name,
					Drift: drifted,
				})
			}
			// Profile + rule usage. Dedupe by (profileName, ruleID).
			ref := ProfileRef{ProfileName: profile.Name, RuleID: rule.ID}
			dup := false
			for _, p := range row.UsedByProfiles {
				if p.RuleID == ref.RuleID && p.ProfileName == ref.ProfileName {
					dup = true
					break
				}
			}
			if !dup {
				row.UsedByProfiles = append(row.UsedByProfiles, ref)
			}
			ruleDup := false
			for _, rid := range row.UsedByRules {
				if rid == rule.ID {
					ruleDup = true
					break
				}
			}
			if !ruleDup {
				row.UsedByRules = append(row.UsedByRules, rule.ID)
			}
		}
	}

	// Flatten, sort by Category then Name so the default render order
	// matches the "By Group" sort the frontend exposes by default.
	out := make([]*CFRow, 0, len(rowsByTID))
	for _, row := range rowsByTID {
		sort.Slice(row.Instances, func(i, j int) bool { return row.Instances[i].Name < row.Instances[j].Name })
		sort.Slice(row.UsedByProfiles, func(i, j int) bool { return row.UsedByProfiles[i].ProfileName < row.UsedByProfiles[j].ProfileName })
		sort.Strings(row.UsedByRules)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Name < out[j].Name
	})

	writeJSON(w, map[string]any{
		"appType": appType,
		"items":   out,
	})
}
