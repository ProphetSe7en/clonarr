// Package core - customizations counter.
//
// ComputeRuleCustomizations diffs a saved sync rule against the TRaSH
// profile defaults it targets, returning a count breakdown matching the
// detail view's "Override mode · N changes" header. Pure read-only -
// touches no sync state, never calls Arr.
//
// Counted categories (matches frontend pdOverrideSummary):
//   - Quality: cutoffQuality override + QualityStructure leaf-diff against
//     profile.Items (when QualityStructure is set), OR len(QualityOverrides)
//     when only the legacy flat map is used
//   - ExtraCFs: ScoreOverrides entries whose trashID is NOT in the
//     profile's effective default CF set (FormatItems + default-on cf-groups)
//   - CustomScores: ScoreOverrides entries whose trashID IS in defaults
//     but whose score differs from the CF's TRaSH default for this profile's
//     scoreSet
//   - General: Overrides settings (language, upgradeAllowed, min/cutoff
//     scores) that are non-nil AND differ from profile defaults
//
// Categories deliberately NOT counted in this first version (consistent
// with the detail view's current behaviour; expand both views together
// in a later pass if needed):
//   - KeepArrCFIDs: Arr-only CF preservation markers
//   - Optional cf-groups: default-off groups the user toggled on
//
// ExcludedCFs ARE counted - the lock-icon UI lets the user opt out of
// required CFs, so the rule can carry excludedCFs that materially affect
// what syncs. Without surfacing the count, a rule with N excluded CFs
// reads as "0 customizations" in the Sync Rules pill, which actively
// misleads.

package core

import "strings"

// RuleCustomizations is the per-rule breakdown returned by
// ComputeRuleCustomizations and exposed via the API endpoint.
type RuleCustomizations struct {
	Quality      int `json:"quality"`
	ExtraCFs     int `json:"extraCFs"`
	CustomScores int `json:"customScores"`
	General      int `json:"general"`
	ExcludedCFs  int `json:"excludedCFs"`
	Total        int `json:"total"`
	// GeneralLabels names which general settings differ from the profile
	// default (e.g. "Upgrades", "Min score"), so the Sync Rules tooltip can
	// show "General: Upgrades, Min score" instead of just "General: 2".
	GeneralLabels []string `json:"generalLabels,omitempty"`
}

// ComputeRuleCustomizations returns the per-rule customization breakdown.
// Caller is responsible for resolving `profile` from the rule's
// TrashProfileID against the active TRaSH snapshot; passing nil for either
// profile or ad results in an empty (all-zero) breakdown.
//
// `customCFIDs` is the set of user-created custom CF trash IDs (the
// `custom:<id>` registry). Used to filter out dangling references in
// ScoreOverrides - when a user deletes a custom CF, its entry can
// linger on rules until the next successful sync's CleanupDangling
// pass purges it. The detail view hides those orphans; the list pill
// must do the same to keep counts consistent.
// `appType` is "radarr" or "sonarr"; used to gate the Sonarr-doesn't-
// have-language defensive check.
func ComputeRuleCustomizations(rule *AutoSyncRule, profile *TrashQualityProfile, ad *AppData, customCFIDs map[string]bool, appType string) RuleCustomizations {
	var out RuleCustomizations
	if rule == nil || profile == nil {
		return out
	}

	// Effective default CF set - what TRaSH would include in the profile
	// without any user customization. FormatItems + default-on groups.
	defaultCFs := ComputeTrashDefaults(profile, ad)

	// Profile-eligible CF set - every CF that lives in any cf-group
	// whose quality_profiles.include lists this profile, plus
	// formatItems. Used to classify an opt-in as "true Additional"
	// (CF lives in a group OUTSIDE the profile's scope, e.g. HDR
	// Formats for WEB-1080p) vs "profile opt-in" (CF lives in a
	// default-off group that IS in the profile's scope, e.g. Streaming
	// Services UK for WEB-1080p - the group is offered for opt-in by
	// the profile design, so opting in isn't an "Additional CF").
	// Matches frontend pdAllCustomizations's inProfileGroups filter.
	// Without this distinction, opting into a profile-eligible default-
	// off group inflated the ExtraCFs count vs the editor's view.
	profileEligibleCFs := make(map[string]bool)
	for _, tid := range profile.FormatItems {
		profileEligibleCFs[tid] = true
	}
	if ad != nil {
		for _, g := range ad.CFGroups {
			if _, ok := g.QualityProfiles.Include[profile.Name]; !ok {
				continue
			}
			for _, cfEntry := range g.CustomFormats {
				profileEligibleCFs[cfEntry.TrashID] = true
			}
		}
	}

	// Score-set context. Profiles with a TrashScoreSet override pull from
	// that set; everything else uses "default". Matches sync engine.
	scoreSet := profile.TrashScoreSet
	if scoreSet == "" {
		scoreSet = "default"
	}

	// Collect added + overridden as SETS so each tid can belong to
	// BOTH categories (a CF the user picked from Additional CF AND
	// gave a custom score to) without inflating the total. Backend
	// total uses the union count, matching the editor's
	// pdOverrideSummary which counts customizations = unique tids.
	// ExtraCFs / CustomScores remain as cardinalities of the
	// individual sets so the tooltip can render the breakdown.
	added := make(map[string]bool)
	overridden := make(map[string]bool)

	// 1a. SelectedCFs - pure Additional CF opt-ins. Skips CFs in any
	//     profile-eligible group (those are profile opt-ins, not
	//     Additional) and dangling custom: orphans.
	for _, tid := range rule.SelectedCFs {
		if profileEligibleCFs[tid] {
			continue
		}
		if strings.HasPrefix(tid, "custom:") {
			if customCFIDs == nil || !customCFIDs[tid] {
				continue
			}
		}
		added[tid] = true
	}

	// 1b. ScoreOverrides - populates BOTH sets where appropriate:
	//     - non-profile-eligible CFs with a score override also count
	//       as Additional (the override is itself an opt-in signal even
	//       without a SelectedCFs entry), and as a CustomScore when
	//       the saved score differs from the CF's TRaSH default.
	//     - profile-eligible CFs only count as CustomScore (when the
	//       score differs from default).
	isCustomScore := func(tid string, userScore int) bool {
		if ad == nil {
			return true
		}
		cf, ok := ad.CustomFormats[tid]
		if !ok {
			return true
		}
		defScore, hasDef := cf.TrashScores[scoreSet]
		if !hasDef {
			defScore, hasDef = cf.TrashScores["default"]
		}
		return !hasDef || userScore != defScore
	}

	for tid, userScore := range rule.ScoreOverrides {
		// Hide dangling custom: orphans - referenced CF was deleted
		// from the registry but the rule's override hasn't been cleaned
		// yet. Editor mirrors this hide via pdAllCustomizations lookup.
		if strings.HasPrefix(tid, "custom:") {
			if customCFIDs == nil || !customCFIDs[tid] {
				continue
			}
		}

		if !profileEligibleCFs[tid] {
			// Truly Additional. The score override itself is an opt-in
			// signal, so add to "added" even if SelectedCFs didn't list
			// it. Then layer the CustomScore on top when applicable -
			// previously the loop continued here and silently dropped
			// the score-change tracking for Additional+score combos.
			added[tid] = true
			if isCustomScore(tid, userScore) {
				overridden[tid] = true
			}
			continue
		}

		// Profile-eligible CF: a non-default score is a CustomScore.
		// (default-off opt-in groups land here too - overriding the
		// recommended score for an opt-in is still a CustomScore.)
		if isCustomScore(tid, userScore) {
			overridden[tid] = true
		}
	}

	out.ExtraCFs = len(added)
	out.CustomScores = len(overridden)

	// 2. Quality changes.
	//
	//    a) Cutoff override - counts when Overrides.CutoffQuality is set
	//       AND differs from profile.Cutoff.
	if rule.Overrides != nil && rule.Overrides.CutoffQuality != nil {
		if *rule.Overrides.CutoffQuality != profile.Cutoff {
			out.Quality++
		}
	}
	//    b) Quality items diff - leaf-flatten both sides and count
	//       differing or removed entries. Matches pdQualityItemsChangeCount.
	if len(rule.QualityStructure) > 0 {
		orig := flattenQualityLeaves(profile.Items)
		cur := flattenQualityLeaves(rule.QualityStructure)
		for name, allowed := range cur {
			if oa, ok := orig[name]; !ok || oa != allowed {
				out.Quality++
			}
		}
		for name := range orig {
			if _, ok := cur[name]; !ok {
				out.Quality++
			}
		}
	} else if len(rule.QualityOverrides) > 0 {
		out.Quality += len(rule.QualityOverrides)
	}

	// 3. General settings overrides. Each Overrides field is *T (nil =
	//    not overridden). Count when non-nil AND differs from default.
	//    Language is gated to Radarr to match pdGeneralChangeCount's
	//    behaviour - Sonarr profiles don't expose a Language editor so a
	//    Sonarr rule with Language set is almost certainly a hand-edited
	//    config; counting it would be unhelpful.
	if rule.Overrides != nil {
		ov := rule.Overrides
		addGeneral := func(label string) {
			out.General++
			out.GeneralLabels = append(out.GeneralLabels, label)
		}
		if appType == "radarr" && ov.Language != nil && *ov.Language != profile.Language {
			addGeneral("Language")
		}
		if ov.UpgradeAllowed != nil && *ov.UpgradeAllowed != profile.UpgradeAllowed {
			if *ov.UpgradeAllowed {
				addGeneral("Upgrades on")
			} else {
				addGeneral("Upgrades off")
			}
		}
		if ov.MinFormatScore != nil && *ov.MinFormatScore != profile.MinFormatScore {
			addGeneral("Min score")
		}
		if ov.MinUpgradeFormatScore != nil && *ov.MinUpgradeFormatScore != profile.MinUpgradeFormatScore {
			addGeneral("Min upgrade")
		}
		if ov.CutoffFormatScore != nil && *ov.CutoffFormatScore != profile.CutoffFormatScore {
			addGeneral("Cutoff score")
		}
	}

	// 4. Excluded CFs - opt-outs from the profile's effective default
	//    set. Only count exclusions that actually subtract something -
	//    if the trashId isn't in defaultCFs, the entry is dead state
	//    (CF moved out of defaults upstream; backend cleans on next
	//    sync). Covers BOTH excluded required CFs (always in defaults
	//    via FormatItems / required-in-active-groups) AND opt-outs
	//    from default-on optional CFs.
	for _, tid := range rule.ExcludedCFs {
		if defaultCFs[tid] {
			out.ExcludedCFs++
		}
	}

	// Total uses the UNION of added + overridden CFs (matching the
	// editor's pdOverrideSummary `customizations` field), plus the
	// independent quality / general / excluded counts. Summing
	// ExtraCFs + CustomScores directly would double-count a CF that is
	// both Additional AND has a custom score - the very case that made
	// the Sync Rules column read "4 changes" for a rule the editor
	// summarised as "2 customizations + 2 custom scores".
	cfUnion := make(map[string]bool, len(added)+len(overridden))
	for tid := range added {
		cfUnion[tid] = true
	}
	for tid := range overridden {
		cfUnion[tid] = true
	}
	out.Total = out.Quality + len(cfUnion) + out.General + out.ExcludedCFs
	return out
}

// flattenQualityLeaves converts a quality-structure slice into a leaf-
// level {leafName: allowed} map. Group items push their `allowed` down
// to each leaf member, so a group toggled on with three child qualities
// produces three map entries all set to true.
func flattenQualityLeaves(items []QualityItem) map[string]bool {
	out := make(map[string]bool)
	for _, it := range items {
		if len(it.Items) > 0 {
			for _, leaf := range it.Items {
				out[leaf] = it.Allowed
			}
		} else {
			out[it.Name] = it.Allowed
		}
	}
	return out
}
