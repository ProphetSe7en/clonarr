package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clonarr/internal/arr"
	"clonarr/internal/core"
)

// Per-profile quality sizes: the endpoints behind the profile editor's Quality
// size tab, plus CRUD for the size sets a user saves.
//
// Sonarr v5 moved quality sizes off the shared /qualitydefinition list and onto
// each quality profile. On those versions the instance-wide tab under Media
// Management has nothing to write, so the choice lives per profile instead.
// Everything here gates on the shape of the instance's definition list rather
// than on a version string or an app name.

// qsProfileRow is one quality as the tab shows it.
type qsProfileRow struct {
	Quality string `json:"quality"`
	// Group is the quality group this quality sits in, empty when it stands
	// on its own. Sizes belong to individual qualities, so a group is shown
	// for context and never as a source.
	Group   string `json:"group,omitempty"`
	Allowed bool   `json:"allowed"`
	// Current is what the instance has on this profile right now. Nil fields
	// mean the instance has no value, which its own UI shows as Unlimited.
	Current qsTriple `json:"current"`
	// Resolved is what clonarr would write on the next sync, and Source says
	// where that number came from so the user knows which lever to pull. The
	// attribution is computed here rather than inferred by the frontend.
	Resolved qsTriple `json:"resolved"`
	Source   string   `json:"source"` // "override", "set", or "" for nothing written
}

type qsTriple struct {
	Min       *float64 `json:"min"`
	Preferred *float64 `json:"preferred"`
	Max       *float64 `json:"max"`
}

// Capability cache. Answering "where do this instance's sizes live" costs a
// GET /qualitydefinition, and the editor may have to ask about more than one
// instance to find the right target, on every open. The sync engine never
// reads this: it already holds the live definition list at the moment it
// decides, so the authoritative check stays live and this only affects what
// the UI offers.
//
// A short TTL rather than invalidation on instance edit, because the thing
// that changes the answer is the user upgrading Sonarr, which clonarr never
// sees. Both stale directions are safe: a stale "shared list" hides the tab
// for a couple of minutes, and a stale "per profile" shows a tab whose writes
// the live gate still refuses.
type qsCapEntry struct {
	perProfile bool
	at         time.Time
}

var (
	qsCapCache sync.Map // instance ID -> qsCapEntry
	qsCapTTL   = 2 * time.Minute
)

func (s *Server) instanceKeepsSizesPerProfile(inst core.Instance) (bool, error) {
	if v, ok := qsCapCache.Load(inst.ID); ok {
		if e, ok := v.(qsCapEntry); ok && time.Since(e.at) < qsCapTTL {
			return e.perProfile, nil
		}
	}
	client := core.NewArrClientFor(inst, s.Core.HTTPClient)
	defs, err := client.ListQualityDefinitions()
	if err != nil {
		return false, err
	}
	// An empty list is not an answer: it means the read came back wrong, and
	// offering a tab that cannot write anything is the worse mistake.
	perProfile := len(defs) > 0 && !arr.QualityDefinitionsCarrySizes(defs)
	qsCapCache.Store(inst.ID, qsCapEntry{perProfile: perProfile, at: time.Now()})
	return perProfile, nil
}

// handleProfileQualitySizes returns everything the profile editor's Quality
// size tab needs in one call.
//
// Keyed on the instance and the target Arr profile rather than on a sync rule,
// which is what it used to be. A rule only exists after the first sync, so
// keying on one meant the tab could not appear until the user had already
// synced the profile once with no sizes, then gone back in to set them and
// synced again. Two passes for something that should take one.
//
//	app          required, "radarr" or "sonarr"
//	instanceId   optional, the instance the editor is pointed at
//	arrProfileId optional, 0 or absent for a profile that does not exist yet
//
// The instance is chosen here rather than by the caller, because the caller
// cannot know which of its instances keeps sizes per profile without asking,
// and in a mixed Sonarr 4 and 5 setup the editor is bound to whichever
// instance happens to come first in the config. Preference goes to the one the
// caller names; failing that, the first candidate that qualifies. The response
// always says which instance it answered for, so the tab can label itself
// instead of leaving the user to guess.
func (s *Server) handleProfileQualitySizes(w http.ResponseWriter, r *http.Request) {
	appType := r.URL.Query().Get("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be radarr or sonarr")
		return
	}
	preferred := r.URL.Query().Get("instanceId")
	arrProfileID, _ := strconv.Atoi(r.URL.Query().Get("arrProfileId"))
	// The guide profile being browsed, when there is one. Only used to mark
	// which qualities it allows while no profile exists on the instance yet.
	trashID := r.URL.Query().Get("trashId")
	// True when the editor is bound to a saved rule, which belongs to one
	// instance. Browsing a guide profile, or picking a target in the sync step
	// without saving, decides nothing yet.
	instanceFixed := r.URL.Query().Get("instanceFixed") == "1"

	cfg := s.Core.Config.Get()
	candidates := make([]core.Instance, 0, 2)
	for _, inst := range cfg.Instances {
		if inst.Type != appType {
			continue
		}
		if inst.ID == preferred {
			candidates = append([]core.Instance{inst}, candidates...)
			continue
		}
		candidates = append(candidates, inst)
	}

	// A saved rule writes to its own instance. If that one keeps its sizes in
	// the shared list there is nothing to set per profile, and answering for a
	// different instance would offer a setting this rule never writes. So a
	// decided instance is answered for as-is, and only an undecided preference
	// may be resolved past, which is what puts the tab in front of a Sonarr 5
	// user whose Sonarr 4 happens to come first in the config.
	if instanceFixed && preferred != "" && len(candidates) > 0 && candidates[0].ID == preferred {
		candidates = candidates[:1]
	}

	resp := map[string]any{
		"perProfileSizes": false,
		"source":          "",
		"overrides":       map[string]core.QSOverride{},
		"sets":            s.qsAvailableSets(appType),
		"defaultSetType":  core.DefaultQualitySizeSetType(appType),
		"rows":            []qsProfileRow{},
	}

	var target *core.Instance
	var lastErr error
	for i := range candidates {
		perProfile, err := s.instanceKeepsSizesPerProfile(candidates[i])
		if err != nil {
			lastErr = err
			continue
		}
		if perProfile {
			target = &candidates[i]
			break
		}
	}
	if target == nil {
		// Nothing to set here. Not an error: on Radarr and Sonarr 4 the sizes
		// live in one shared list and the instance-wide tab is the right
		// place, so the editor simply shows no Quality size tab.
		//
		// An instance we could not reach is a different answer. We do not
		// know what it keeps, and hiding the tab would assert that there is
		// nothing to set. In a mixed Sonarr 4 and 5 setup the editor is bound
		// to the reachable one, so nothing else on the screen would hint that
		// anything was down: the tab would simply be gone. Say so instead.
		if lastErr != nil && len(candidates) > 0 {
			resp["warning"] = "Could not reach " + candidates[0].Name + ": " + lastErr.Error()
			resp["unknown"] = true
		}
		writeJSON(w, resp)
		return
	}

	resp["perProfileSizes"] = true
	resp["instanceId"] = target.ID
	resp["instanceName"] = target.Name
	// True when the caller was pointed at a different instance, so the tab can
	// say whose numbers it is showing rather than quietly swapping them.
	resp["instanceSwitched"] = preferred != "" && preferred != target.ID

	client := core.NewArrClientFor(*target, s.Core.HTTPClient)

	// A profile id belongs to the instance it came from. When the caller
	// preferred one instance and the sizes live on another, the id names a
	// profile on the instance we are not reading, so it cannot be used here.
	// Keeping it meant looking up a Sonarr 4 profile id on the Sonarr 5
	// instance: a 404 at best, and the wrong profile's sizes when the two
	// happen to share an id, which small sequential ids do often.
	if arrProfileID > 0 && preferred != "" && preferred != target.ID {
		arrProfileID = 0
	}

	// Where the "on the instance" column comes from. An existing profile has
	// real values; one that does not exist yet gets the blank profile the app
	// offers as a starting point, which on these versions carries the app's
	// own default sizes and is the same source CreateProfile builds from.
	var items []arr.ArrQualityItem
	if arrProfileID > 0 {
		profile, err := client.GetProfile(arrProfileID)
		if err != nil {
			// The column is context, not the feature. A profile the instance
			// does not have is the same situation as one that does not exist
			// yet, so fall back to the template and say so rather than fail
			// the whole tab out of existence.
			arrProfileID = 0
			resp["warning"] = "Could not read that profile on " + target.Name +
				", so the \"on the instance\" column shows the defaults a new profile starts with."
		} else {
			items = profile.Items
		}
	}
	if arrProfileID == 0 {
		schema, err := client.GetProfileSchema()
		if err != nil {
			writeError(w, 502, "Failed to read the profile template: "+err.Error())
			return
		}
		items = schema.Items
		resp["fromSchema"] = true
	}

	// The rule's stored intent, when there is a rule yet.
	var ruleSource string
	var ruleOverrides map[string]core.QSOverride
	if rule := s.findRuleFor(target.ID, arrProfileID); rule != nil {
		ruleSource = rule.QualitySizeSource
		ruleOverrides = rule.QualitySizeOverrides
	}
	resp["source"] = ruleSource
	if ruleOverrides != nil {
		resp["overrides"] = ruleOverrides
	}

	var guideSets []*core.TrashQualitySize
	if ad := s.Core.Trash.GetAppData(target.Type); ad != nil {
		guideSets = ad.QualitySizes
	}
	sizes := core.ResolveQualitySizes(core.QualitySizeResolveInput{
		Source:    ruleSource,
		Overrides: ruleOverrides,
		AppType:   target.Type,
		GuideSets: guideSets,
		UserSets:  cfg.QualitySizeSets,
	})

	// The instance's blank-profile template has every quality switched off, so
	// its allowed flags say nothing about the profile the user is looking at.
	// Take them from the guide profile instead, or every row would claim "not
	// allowed" about a profile that does allow some.
	var allowedNames map[string]bool
	if arrProfileID == 0 {
		allowedNames = guideAllowedQualities(s.Core.Trash.GetAppData(target.Type), trashID)
	}

	resp["rows"] = buildQSProfileRows(items, sizes, ruleOverrides, allowedNames)
	writeJSON(w, resp)
}

// buildQSProfileRows flattens a profile's items into one row per quality.
//
// Group rows are not rows of their own: the app keeps sizes on individual
// qualities, so a group contributes its members and its name as context. A
// member inherits the group's allowed state, which is what the profile
// actually stores.
// guideAllowedQualities returns the quality names a guide profile allows, or
// nil when there is no such profile to read. Used only for a profile that does
// not exist on the instance yet.
func guideAllowedQualities(ad *core.AppData, trashID string) map[string]bool {
	if ad == nil || trashID == "" {
		return nil
	}
	var profile *core.TrashQualityProfile
	for _, p := range ad.Profiles {
		if p.TrashID == trashID {
			profile = p
			break
		}
	}
	if profile == nil {
		return nil
	}
	out := map[string]bool{}
	for _, it := range profile.Items {
		if !it.Allowed {
			continue
		}
		if len(it.Items) > 0 {
			// A member inherits the group's allowed state, which is what the
			// profile does.
			for _, member := range it.Items {
				out[member] = true
			}
			continue
		}
		out[it.Name] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// allowedNames, when not nil, replaces the items' own allowed flags.
func buildQSProfileRows(items []arr.ArrQualityItem, sizes map[string]core.QualitySizeTriple, overrides map[string]core.QSOverride, allowedNames map[string]bool) []qsProfileRow {
	rows := []qsProfileRow{}
	var walk func(its []arr.ArrQualityItem, group string)
	walk = func(its []arr.ArrQualityItem, group string) {
		for _, it := range its {
			if len(it.Items) > 0 {
				name := it.Name
				if name == "" && it.Quality != nil {
					name = it.Quality.Name
				}
				walk(it.Items, name)
				continue
			}
			name := it.Name
			if name == "" && it.Quality != nil {
				name = it.Quality.Name
			}
			if name == "" {
				continue
			}
			allowed := it.Allowed
			if allowedNames != nil {
				allowed = allowedNames[name]
			}
			row := qsProfileRow{
				Quality: name,
				Group:   group,
				Allowed: allowed,
				Current: qsTriple{Min: it.MinSize, Preferred: it.PreferredSize, Max: it.MaxSize},
			}
			if t, ok := sizes[name]; ok {
				row.Resolved = qsTriple{Min: t.Min, Preferred: t.Preferred, Max: t.Max}
				if _, isOverride := overrides[name]; isOverride {
					row.Source = "override"
				} else {
					row.Source = "set"
				}
			}
			rows = append(rows, row)
		}
	}
	walk(items, "")
	// The profile stores items lowest-priority first; the tab reads better
	// highest first, which is the order both Arr UIs show.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}

// qsSetOption is one entry in the source picker: a guide set or a user set,
// described the same way so one list can hold both.
type qsSetOption struct {
	Value  string `json:"value"` // the stored source string
	Name   string `json:"name"`
	Origin string `json:"origin"` // "guide" or "user"
	// ID is set for a user set, so the tab can offer to delete it.
	ID string `json:"id,omitempty"`
	// Entries are the set's own rows. Sent so the tab can resolve a source
	// the user has just picked but not yet saved. Without them the "will be
	// written" column could only show the saved rule's resolution, which is
	// the previous set's numbers under the new set's name. Six sets of at
	// most 21 rows, so the payload cost is nothing.
	Entries []core.QualitySizeEntry `json:"entries"`
	// UsedBy names the instances whose rules point at this set, for user sets
	// only. Reported so the picker can say a set cannot be deleted before
	// asking the user to confirm deleting it.
	UsedBy []string `json:"usedBy,omitempty"`
}

func (s *Server) qsAvailableSets(appType string) []qsSetOption {
	out := []qsSetOption{}
	if ad := s.Core.Trash.GetAppData(appType); ad != nil {
		for _, set := range ad.QualitySizes {
			if set == nil {
				continue
			}
			out = append(out, qsSetOption{
				Value:   core.QSSourceSetPfx + set.TrashID,
				Name:    set.Type,
				Origin:  "guide",
				Entries: set.Qualities,
			})
		}
		// movie and series first, then the sqp variants, then anime: the same
		// order the instance-wide tab uses, so the two screens read alike.
		order := map[string]int{"movie": 0, "series": 0, "sqp-streaming": 1, "sqp-uhd": 2, "anime": 3}
		sort.SliceStable(out, func(i, j int) bool {
			oi, oj := 99, 99
			if v, ok := order[out[i].Name]; ok {
				oi = v
			}
			if v, ok := order[out[j].Name]; ok {
				oj = v
			}
			return oi < oj
		})
	}
	for _, set := range s.Core.Config.Get().QualitySizeSets {
		if set.AppType != appType {
			continue
		}
		out = append(out, qsSetOption{
			Value:   core.QSSourceCustomPfx + set.ID,
			Name:    set.Name,
			Origin:  "user",
			ID:      set.ID,
			Entries: set.Qualities,
			UsedBy:  s.qsSetUsedBy(set.ID),
		})
	}
	return out
}

// --- User sets ---
//
// There is no list endpoint: the per-rule endpoint above already serves the
// sets the picker needs, and a second way to read the same data is one more
// thing to keep in step.

func (s *Server) handleSaveQualitySizeSet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<18)
	var set core.QualitySizeSet
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	set.Name = strings.TrimSpace(set.Name)
	if set.Name == "" {
		writeError(w, 400, "Give the set a name")
		return
	}
	if set.AppType != "radarr" && set.AppType != "sonarr" {
		writeError(w, 400, "A set belongs to either Radarr or Sonarr")
		return
	}
	// A set with no rows would resolve to nothing, which is indistinguishable
	// from off and would leave the user wondering why their choice does not
	// take. Refuse it instead.
	if len(set.Qualities) == 0 {
		writeError(w, 400, "A set needs at least one quality")
		return
	}
	// Checked here rather than at sync time. An unvalidated row reaches the
	// Arr API inside a profile write and fails the whole profile with a 400,
	// surfacing as a sync failure traced back to a set saved weeks earlier.
	lim := arr.QualitySizeLimitsFor(set.AppType)
	seen := map[string]bool{}
	for i := range set.Qualities {
		q := &set.Qualities[i]
		q.Quality = strings.TrimSpace(q.Quality)
		if q.Quality == "" {
			writeError(w, 400, "Every row needs a quality name")
			return
		}
		if seen[q.Quality] {
			writeError(w, 400, "The set lists "+q.Quality+" twice")
			return
		}
		seen[q.Quality] = true
		if q.Min < 0 || q.Preferred < 0 || q.Max < 0 {
			writeError(w, 400, q.Quality+": sizes cannot be negative")
			return
		}
		if q.Min > q.Max {
			writeError(w, 400, q.Quality+": the minimum is above the maximum")
			return
		}
		if q.Max > lim.Max || q.Preferred > lim.Preferred {
			writeError(w, 400, fmt.Sprintf("%s: %s accepts at most %g preferred and %g maximum", q.Quality, set.AppType, lim.Preferred, lim.Max))
			return
		}
	}

	// Checked before entering Update, which calls saveLocked unconditionally:
	// a return from inside the closure ends the closure, not the save, so
	// rejecting in there still rewrote clonarr.json for a request that
	// changed nothing. Names are what the picker shows, so a duplicate within
	// one app makes two entries that cannot be told apart.
	for _, existing := range s.Core.Config.Get().QualitySizeSets {
		if existing.ID != set.ID && existing.AppType == set.AppType &&
			strings.EqualFold(existing.Name, set.Name) {
			writeError(w, 409, "A set with that name already exists for this app")
			return
		}
	}

	if err := s.Core.Config.Update(func(cfg *core.Config) {
		if set.ID == "" {
			set.ID = core.GenerateID()
			cfg.QualitySizeSets = append(cfg.QualitySizeSets, set)
			return
		}
		for i := range cfg.QualitySizeSets {
			if cfg.QualitySizeSets[i].ID == set.ID {
				cfg.QualitySizeSets[i] = set
				return
			}
		}
		cfg.QualitySizeSets = append(cfg.QualitySizeSets, set)
	}); err != nil {
		writeError(w, 500, "Failed to save the set")
		return
	}
	writeJSON(w, set)
}

// qsSetTitle gives a guide set type a display name, matching what the profile
// editor's picker shows. Delegates so there is one spelling of these names.
func qsSetTitle(setType string) string { return core.QualitySizeSetTitle(setType) }

// applyQualitySizeFallback lets a rerun from history reproduce the sizes it
// recorded, for the case where the set it used has since been deleted. A no-op
// for every other request, since only a rerun carries a fallback.
//
// The numbers are stored as a named set rather than scattered across the rule
// as per-quality values. Both forms write the same sizes, but a rule pointing
// at a named set still reads as a choice, and the source picker keeps doing
// something; a rule with a value on every quality overrides the picker on
// every row and reads as broken. The name carries the date it came from, so it
// is obvious where it appeared from, and a repeated rerun reuses the same set
// rather than stacking copies.
func (s *Server) applyQualitySizeFallback(req *core.SyncRequest, appType string) {
	if len(req.QualitySizeFallback) == 0 {
		return
	}
	var guide []*core.TrashQualitySize
	if ad := s.Core.Trash.GetAppData(appType); ad != nil {
		guide = ad.QualitySizes
	}
	cfg := s.Core.Config.Get()
	if core.QualitySizeSourceResolves(req.QualitySizeSource, guide, cfg.QualitySizeSets) {
		return
	}

	name := strings.TrimSpace(req.QualitySizeFallbackName)
	if name == "" {
		name = "Restored sizes"
	}
	for _, set := range cfg.QualitySizeSets {
		if set.AppType == appType && strings.EqualFold(set.Name, name) {
			req.QualitySizeSource = core.QSSourceCustomPfx + set.ID
			return
		}
	}

	qualities := make([]core.QualitySizeEntry, 0, len(req.QualitySizeFallback))
	for quality, v := range req.QualitySizeFallback {
		qualities = append(qualities, core.QualitySizeEntry{
			Quality: quality, Min: v.Min, Preferred: v.Preferred, Max: v.Max,
		})
	}
	sort.Slice(qualities, func(i, j int) bool { return qualities[i].Quality < qualities[j].Quality })

	stored := core.QualitySizeSet{
		ID:        core.GenerateID(),
		Name:      name,
		AppType:   appType,
		Qualities: qualities,
	}
	if err := s.Core.Config.Update(func(c *core.Config) {
		c.QualitySizeSets = append(c.QualitySizeSets, stored)
	}); err != nil {
		// Leave the source alone rather than half-applying: it resolves to
		// nothing, so no sizes are written, which is the safe direction.
		s.Core.DebugLog.Logf(core.LogError, "rerun from history: could not store the restored size set: %v", err)
		return
	}
	req.QualitySizeSource = core.QSSourceCustomPfx + stored.ID
	s.Core.DebugLog.Logf(core.LogSync,
		"rerun from history: the size set it used is gone, stored its %d sizes as %q", len(qualities), name)
}

// qsHistoryLabel names a stored size source for a history entry, resolved now
// rather than when someone reads it: the set can be deleted in between, and an
// id tells a reader nothing.
func (s *Server) qsHistoryLabel(appType, source string) string {
	var guide []*core.TrashQualitySize
	if ad := s.Core.Trash.GetAppData(appType); ad != nil {
		guide = ad.QualitySizes
	}
	return core.QualitySizeSourceLabel(source, guide, s.Core.Config.Get().QualitySizeSets)
}

// qsGuideSetByType finds a guide size set by its type. core resolves sources
// by trash_id; this is the one place that needs the lookup the other way, to
// name and resolve the app's default set for a profile with no rule.
func qsGuideSetByType(sets []*core.TrashQualitySize, setType string) *core.TrashQualitySize {
	for _, set := range sets {
		if set != nil && strings.EqualFold(set.Type, setType) {
			return set
		}
	}
	return nil
}

// qsSetUsedBy names the instances whose rules point at this user set. A set a
// rule points at cannot be deleted out from under it: the rule would resolve
// to nothing and quietly stop writing sizes.
//
// Also reported on every set in the listing, so the picker can say a set is in
// use before asking the user to confirm deleting it rather than after.
func (s *Server) qsSetUsedBy(id string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, rule := range s.Core.Config.Get().AutoSync.Rules {
		if rule.QualitySizeSource != core.QSSourceCustomPfx+id {
			continue
		}
		name := rule.ID
		if inst, ok := s.Core.Config.GetInstance(rule.InstanceID); ok {
			name = inst.Name
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func (s *Server) handleDeleteQualitySizeSet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	usedBy := s.qsSetUsedBy(id)

	// A rule pointing at this set keeps writing the same numbers, as its own
	// per-quality values. Refusing the delete was the easy answer and the
	// wrong one: the user asked for the set to go, and the alternative to
	// converting is either breaking those rules silently or standing in the
	// way of a decision that is theirs. This is the exact inverse of "Save as
	// set", which folded per-quality values into a set in the first place.
	converted := 0
	landedOn := ""
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		var deleted core.QualitySizeSet
		for _, set := range cfg.QualitySizeSets {
			if set.ID == id {
				deleted = set
				break
			}
		}
		var def *core.TrashQualitySize
		if ad := s.Core.Trash.GetAppData(deleted.AppType); ad != nil {
			want := core.DefaultQualitySizeSetType(deleted.AppType)
			for _, g := range ad.QualitySizes {
				if g != nil && g.Type == want {
					def = g
					break
				}
			}
		}
		newSource, base := qsSetDeletionIntent(deleted.Qualities, def)
		landedOn = newSource

		src := core.QSSourceCustomPfx + id
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].QualitySizeSource != src {
				continue
			}
			merged := make(map[string]core.QSOverride, len(base)+len(cfg.AutoSync.Rules[i].QualitySizeOverrides))
			for name, ov := range base {
				merged[name] = ov
			}
			// An explicit per-quality value already beat the set, so it still
			// does. Order matters: the deviations go in first and the rule's
			// own values overwrite them.
			for name, ov := range cfg.AutoSync.Rules[i].QualitySizeOverrides {
				merged[name] = ov
			}
			cfg.AutoSync.Rules[i].QualitySizeSource = newSource
			cfg.AutoSync.Rules[i].QualitySizeOverrides = merged
			converted++
		}
		out := make([]core.QualitySizeSet, 0, len(cfg.QualitySizeSets))
		for _, set := range cfg.QualitySizeSets {
			if set.ID != id {
				out = append(out, set)
			}
		}
		cfg.QualitySizeSets = out
	}); err != nil {
		writeError(w, 500, "Failed to delete the set")
		return
	}
	writeJSON(w, map[string]any{
		"status":         "deleted",
		"convertedRules": converted,
		"usedBy":         usedBy,
		"landedOn":       landedOn,
	})
}

// qsSetDeletionIntent works out what a rule pointing at a deleted set becomes.
// It returns the source to store and the per-quality values to carry, and it
// must leave the rule writing exactly the numbers it wrote before: no quality
// gains a size, and none loses one.
//
// Preferred answer is the app's default set plus only the qualities where the
// deleted set disagreed with it. Converting every quality instead left the
// rule with an override on all of them, which beat the set on every row, so
// the source picker had no visible effect and read as frozen.
//
// That is only safe when the deleted set covered every quality the default
// covers. Otherwise the default would start writing sizes for qualities this
// rule never wrote, which is the silent change the whole feature avoids, and
// there is no way to say "leave this one alone" on top of a set. In that case
// the rule goes to off and carries the lot.
func qsSetDeletionIntent(deleted []core.QualitySizeEntry, def *core.TrashQualitySize) (string, map[string]core.QSOverride) {
	all := make(map[string]core.QSOverride, len(deleted))
	for _, e := range deleted {
		all[e.Quality] = core.QSOverride{Min: e.Min, Preferred: e.Preferred, Max: e.Max}
	}
	if def == nil || len(def.Qualities) == 0 {
		return core.QSSourceOff, all
	}
	for _, d := range def.Qualities {
		if _, ok := all[d.Quality]; !ok {
			return core.QSSourceOff, all
		}
	}
	defByName := make(map[string]core.QualitySizeEntry, len(def.Qualities))
	for _, d := range def.Qualities {
		defByName[d.Quality] = d
	}
	deviations := map[string]core.QSOverride{}
	for name, ov := range all {
		d, ok := defByName[name]
		if !ok || d.Min != ov.Min || d.Preferred != ov.Preferred || d.Max != ov.Max {
			deviations[name] = ov
		}
	}
	return core.QSSourceSetPfx + def.TrashID, deviations
}
