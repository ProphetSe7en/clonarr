package core

import "strings"

// Per-profile quality sizes: deciding the three numbers for each quality in
// one profile.
//
// Radarr, and Sonarr up to v4, keep quality sizes in one shared list that
// applies to every profile. Sonarr v5 moved them onto each profile's items.
// Nothing in this file asks an instance its version: callers gate on the shape
// of the definition list (arr.QualityDefinitionsCarrySizes) and pass a nil map
// when sizes do not belong on the profile, which makes the request body
// identical to what the older versions have always received.

// QualitySizeTriple is one quality's three sizes. A nil field means "say
// nothing", which leaves whatever the instance already has in place. All three
// nil is the same as having no entry at all.
type QualitySizeTriple struct {
	Min       *float64
	Preferred *float64
	Max       *float64
}

// QualitySizeResolveInput is everything needed to decide one profile's sizes.
type QualitySizeResolveInput struct {
	// Source is the rule's QualitySizeSource. Empty means off.
	Source string
	// Overrides are the rule's per-quality numbers, keyed by quality name.
	Overrides map[string]QSOverride
	// AppType is "radarr" or "sonarr", used to pick the app's default set and
	// to scope user sets.
	AppType string
	// GuideSets are the guide's own size sets for this app
	// (AppData.QualitySizes).
	GuideSets []*TrashQualitySize
	// UserSets are the sets the user saved (Config.QualitySizeSets), already
	// or not yet filtered by app; this function filters by AppType itself.
	UserSets []QualitySizeSet
	// ProfileRef is the size set the profile itself points at, by trash_id,
	// for Source == QSSourceGuide. Empty when the profile carries no
	// reference, in which case the app's default set applies.
	ProfileRef string
}

// DefaultQualitySizeSetType is the guide's default set for an app, used when a
// profile following the guide carries no reference of its own.
//
// Sonarr's answer is series for every non-anime profile, stated by the guide
// author. Radarr's has not been stated; movie follows by the same logic and is
// the value here, but it is inference rather than an answer, so it is one line
// to change when upstream says.
func DefaultQualitySizeSetType(appType string) string {
	if appType == "radarr" {
		return "movie"
	}
	return "series"
}

// ResolveQualitySizes returns quality name → sizes for one profile, or nil
// when nothing should be written.
//
// Order per quality name, and nothing else:
//
//  1. An override on the rule.
//  2. The set the source names.
//  3. Absent from the map, so the field is omitted and the instance keeps its
//     own value.
//
// Resolution is per quality name and never per group. The guide's own sets
// disagree within groups (19 groups across the guide profiles hold members
// with different sizes), so taking a group row's value for its members would
// quietly flatten them. Callers apply this map at the leaves only.
//
// Nothing here clamps. Every guide entry sits at preferred 995 and max 1000,
// which are exactly Sonarr's thresholds, and v5 stores those verbatim. The
// shared-list sync path collapses a value at its limit to nil so the write
// matches what the Arr UI stores for Unlimited; reusing that here would nil
// every size the guide publishes.
func ResolveQualitySizes(in QualitySizeResolveInput) map[string]QualitySizeTriple {
	out := map[string]QualitySizeTriple{}

	// Step 2 first, so step 1 can overwrite it.
	//
	// Off is the absence of a set, not the absence of output. A per-quality
	// override is an explicit instruction to write that number, and the
	// resolution order only means something if it survives a source that
	// yields nothing: otherwise "override, then the set, then nothing" would
	// collapse to "the set, or nothing". It also makes the useful case
	// possible, which is setting two qualities by hand and leaving the other
	// twenty alone.
	//
	// The upgrade guarantee is untouched. A rule that existed before this
	// feature has no source and no overrides, so it still resolves to nil and
	// writes nothing. Overrides can only exist because someone typed them in.
	src := in.Source
	switch {
	case src == "" || src == QSSourceOff:
		// No set. Whatever the overrides say is the whole answer.
	case strings.HasPrefix(src, QSSourceCustomPfx):
		id := strings.TrimPrefix(src, QSSourceCustomPfx)
		for _, set := range in.UserSets {
			if set.ID == id && set.AppType == in.AppType {
				addSizeEntries(set.Qualities, out)
				break
			}
		}
	case strings.HasPrefix(src, QSSourceSetPfx):
		trashID := strings.TrimPrefix(src, QSSourceSetPfx)
		if set := findGuideSetByTrashID(in.GuideSets, trashID); set != nil {
			addSizeEntries(set.Qualities, out)
		}
	case src == QSSourceGuide:
		// The profile's own reference, and the app's default when it carries
		// none. A reference that resolves to nothing falls back to the
		// default rather than to silence: a profile pointing at a set the
		// guide has since removed is a guide-side problem, and leaving the
		// user with no sizes at all would be a worse answer than the default.
		var set *TrashQualitySize
		if in.ProfileRef != "" {
			set = findGuideSetByTrashID(in.GuideSets, in.ProfileRef)
		}
		if set == nil {
			set = findGuideSetByType(in.GuideSets, DefaultQualitySizeSetType(in.AppType))
		}
		if set != nil {
			addSizeEntries(set.Qualities, out)
		}
	default:
		// An unrecognised source writes nothing. A stale client or a
		// hand-edited config must not be able to push sizes by accident.
		return nil
	}

	// Step 1. An override is a whole triple, so it replaces rather than merges.
	for name, ov := range in.Overrides {
		min, pref, max := ov.Min, ov.Preferred, ov.Max
		out[name] = QualitySizeTriple{Min: &min, Preferred: &pref, Max: &max}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func addSizeEntries(entries []QualitySizeEntry, out map[string]QualitySizeTriple) {
	for _, e := range entries {
		min, pref, max := e.Min, e.Preferred, e.Max
		out[e.Quality] = QualitySizeTriple{Min: &min, Preferred: &pref, Max: &max}
	}
}

func findGuideSetByTrashID(sets []*TrashQualitySize, trashID string) *TrashQualitySize {
	if trashID == "" {
		return nil
	}
	for _, s := range sets {
		if s != nil && strings.EqualFold(s.TrashID, trashID) {
			return s
		}
	}
	return nil
}

func findGuideSetByType(sets []*TrashQualitySize, qsType string) *TrashQualitySize {
	if qsType == "" {
		return nil
	}
	for _, s := range sets {
		if s != nil && s.Type == qsType {
			return s
		}
	}
	return nil
}

// FillQualitySizeIntent completes a sync request's per-profile size intent and
// hands it the user's saved sets.
//
// A request that names a source is the profile editor applying a choice the
// user just made, which may not be on the rule yet, so it owns both fields:
// the overrides are taken exactly as sent, an empty map included, because that
// is how the editor says "I removed the last one". Every other caller sends no
// source and gets the rule's stored intent instead.
//
// This exists because the alternative was every entry point remembering to
// copy two fields, and a review found that six of the seven did not: Sync now,
// Update all, a rollback, a clone, the enable toggle and the rule dry-run all
// omitted them, so each of those syncs wrote no sizes while the rule said it
// should. Reconciling the two in one place means a new entry point cannot
// silently stop writing sizes.
// QualitySizeSetTitle gives a guide set type a display name. "series" and
// "movie" read better with a capital; the sqp sets are known by their own
// spelling.
func QualitySizeSetTitle(setType string) string {
	switch setType {
	case "sqp-streaming":
		return "SQP Streaming"
	case "sqp-uhd":
		return "SQP UHD"
	case "":
		return ""
	}
	return strings.ToUpper(setType[:1]) + setType[1:]
}

// QualitySizeSourceLabel names a stored source for display. Resolved when the
// sync runs rather than when someone reads the history, because the set it
// names can be deleted in between and a raw id tells a reader nothing.
func QualitySizeSourceLabel(source string, guideSets []*TrashQualitySize, userSets []QualitySizeSet) string {
	switch {
	case source == "" || source == QSSourceOff:
		return ""
	case strings.HasPrefix(source, QSSourceCustomPfx):
		id := strings.TrimPrefix(source, QSSourceCustomPfx)
		for _, set := range userSets {
			if set.ID == id {
				return set.Name
			}
		}
	case strings.HasPrefix(source, QSSourceSetPfx):
		if set := findGuideSetByTrashID(guideSets, strings.TrimPrefix(source, QSSourceSetPfx)); set != nil {
			return QualitySizeSetTitle(set.Type)
		}
	}
	return "a set that no longer exists"
}

func FillQualitySizeIntent(req *SyncRequest, rule *AutoSyncRule, sets []QualitySizeSet) {
	req.UserQualitySizeSets = sets
	if req.QualitySizeSource != "" || rule == nil {
		return
	}
	req.QualitySizeSource = rule.QualitySizeSource
	req.QualitySizeOverrides = rule.QualitySizeOverrides
}
