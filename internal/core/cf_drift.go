package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"clonarr/internal/arr"
)

// CF spec drift detection pass — the third pass run by DriftRunner
// after TRaSH-upstream detection (the ProfileSyncRunner channel) and
// profile-level drift (drift.go). Compares each Arr instance's live CF
// specs against the TRaSH-Guides or user-custom spec on disk and
// reports when someone (the user, another tool, or a CF rebuild done
// directly in Arr) has diverged the live CF from the spec clonarr
// pushed.
//
// Detection is derive-on-demand: no LastPushed snapshot persists in
// clonarr.json. The "before" side comes from `/data/trash-guides/` for
// TRaSH-managed CFs or from the in-memory CustomCFs registry for user
// custom CFs. The "after" side is the live ArrCF fetched via the
// shared instCache. DiffCFSpecs (cf_diff.go) does the comparison; this
// file owns the per-instance walk + fingerprint state + notification
// dispatch.
//
// Persistence: Instance.CFDriftFingerprints holds one sha256 entry per
// drifted CF per instance, with the entry absent meaning "not drifted".
// Identical fingerprint between Check passes is the same drift; a
// changed fingerprint means new or evolved drift (re-notify); a clear
// transition fires Reconciled. Notification dispatch reuses the
// existing OnDriftDetected per-agent flag per design decision §10.5.

// CFDriftEvent describes one CF-drift state transition that needs to
// notify an agent. Built during the pass, dispatched after the
// Config.Update closure releases its lock so provider HTTP calls
// (Discord, Gotify, NTFY) don't hold locks.
type CFDriftEvent struct {
	Event        CFDriftEventKind
	InstanceID   string
	InstanceName string
	AppType      string
	TrashID      string
	CFName       string
	Diff         *CFSpecDiff
}

type CFDriftEventKind int

const (
	CFDriftDetected CFDriftEventKind = iota
	CFDriftReconciled
)

// cfDriftPassResult collects the outcome of a single CF spec drift
// pass: per-instance per-CF fingerprint updates to persist, and the
// queued notification events. The DriftRunner threads these into its
// existing Config.Update / dispatch flow.
type cfDriftPassResult struct {
	// FingerprintsByInstance maps Instance.ID → trashID → fingerprint.
	// An empty string fingerprint means "drift cleared on this CF" and
	// asks the persistence step to delete the map entry.
	FingerprintsByInstance map[string]map[string]string
	// PriorByInstance is the per-instance snapshot of
	// CFDriftFingerprints taken at the START of this pass. The
	// persistence step compares each tid's current on-disk value
	// against this snapshot before writing: when the live value
	// diverges (an Apply landed mid-pass and cleared the fingerprint),
	// our stale newFP is discarded so we never resurrect drift state
	// the user just resolved.
	PriorByInstance map[string]map[string]string
	// Events queued for notification dispatch after persistence.
	Events []*CFDriftEvent
	// CFCount is the number of currently-drifted CFs across all
	// instances after this pass. Surfaces in the /api/drift/check
	// summary block.
	CFCount int
}

// runCFSpecDriftPass walks every instance touched by the profile drift
// pass and, for each managed CF on that instance, diffs the disk spec
// against the live Arr spec. Builds the cfDriftPassResult that the
// caller persists + dispatches alongside profile drift.
//
// Managed CFs are the union of every eligible rule's effective CF set
// on each instance — derived from rule.SelectedCFs + the profile's
// default-on cf-groups, mirroring what BuildArrProfile would push on a
// sync. CFs the rule explicitly excluded via rule.ExcludedCFs are
// skipped (the user opted out of drift coverage by opting out of the
// sync itself).
//
// A CF the user has not yet pushed to Arr (no live entry for the CF
// name) is silently skipped — the very next sync will create it, and
// flagging "drift" before the entity exists would be misleading.
func runCFSpecDriftPass(d *DriftRunner, work []ruleWork, instCache map[string]*arrSnapshot) *cfDriftPassResult {
	out := &cfDriftPassResult{
		FingerprintsByInstance: make(map[string]map[string]string),
		PriorByInstance:        make(map[string]map[string]string),
	}

	// Group rules by instance so we walk each instance exactly once.
	rulesByInst := make(map[string][]ruleWork)
	for _, w := range work {
		rulesByInst[w.instance.ID] = append(rulesByInst[w.instance.ID], w)
	}

	cfg := d.app.Config.Get()
	// Quick lookup: instance ID → Instance struct so we can read the
	// existing CFDriftFingerprints map without holding the Config lock.
	instByID := make(map[string]Instance, len(cfg.Instances))
	for _, inst := range cfg.Instances {
		instByID[inst.ID] = inst
	}

	for instID, rules := range rulesByInst {
		inst, ok := instByID[instID]
		if !ok {
			continue
		}
		snap, ok := instCache[instID]
		if !ok {
			continue
		}
		// snap.err may be non-nil from a LATER fetch in the chain (e.g.
		// Radarr's ListLanguages) while snap.cfs is fully populated.
		// Profile-drift surfaces the instance-level error separately;
		// CF drift just needs the CF slice to do its work, so gate on
		// the slice rather than the overall error.
		if snap.cfs == nil {
			continue
		}

		// Live CF lookup by name. Live ArrCFs do not carry trash_id;
		// the matching is name-based because that's what Arr itself
		// uses to identify a CF entity.
		liveByName := make(map[string]*arr.ArrCF, len(snap.cfs))
		for i := range snap.cfs {
			liveByName[snap.cfs[i].Name] = &snap.cfs[i]
		}

		appData := d.app.Trash.GetAppData(inst.Type)
		if appData == nil {
			continue
		}
		customCFs := d.app.CustomCFs.List(inst.Type)
		customsByID := make(map[string]CustomCF, len(customCFs))
		for _, c := range customCFs {
			customsByID[c.ID] = c
		}

		// Aggregate the set of CF trash ids that any rule on this
		// instance considers "managed". Rule-level opt-outs (excludedCFs)
		// short-circuit so a user who explicitly removed a required CF
		// from the sync set isn't pestered about drift on it.
		managedTIDs := make(map[string]bool)
		excludedTIDs := make(map[string]bool)
		for _, w := range rules {
			rule := w.rule
			// Find the profile referenced by this rule so we can derive
			// its effective CF set the same way BuildArrProfile would.
			var profile *TrashQualityProfile
			for _, p := range appData.Profiles {
				if p.TrashID == rule.TrashProfileID {
					profile = p
					break
				}
			}
			if profile == nil {
				continue
			}
			defaults := ComputeTrashDefaults(profile, appData)
			for tid := range defaults {
				managedTIDs[tid] = true
			}
			for _, tid := range rule.SelectedCFs {
				managedTIDs[tid] = true
			}
			for _, tid := range rule.ExcludedCFs {
				excludedTIDs[tid] = true
			}
		}

		// Build the per-instance fingerprint delta. priorFP carries the
		// fingerprint from the LAST pass (if any); newFP is what this
		// pass computes. The state machine compares them per CF.
		//
		// Capture the prior snapshot into the result so the persistence
		// closure can detect mid-pass Apply mutations: if a tid's
		// on-disk value differs from this snapshot at write time,
		// Apply (or another writer) intervened and our newFP entry is
		// already stale relative to the user's intent.
		priorFP := inst.CFDriftFingerprints
		if len(priorFP) > 0 {
			snapshot := make(map[string]string, len(priorFP))
			for k, v := range priorFP {
				snapshot[k] = v
			}
			out.PriorByInstance[instID] = snapshot
		}
		newFP := make(map[string]string)

		for tid := range managedTIDs {
			if excludedTIDs[tid] {
				continue
			}

			diskSpec, cfName, ok := resolveCFDiskSpec(tid, appData, customsByID)
			if !ok {
				continue
			}

			live, ok := liveByName[cfName]
			if !ok {
				// CF managed by rule but not yet in Arr; the next sync
				// will push it, no drift to flag.
				continue
			}

			liveAsTrashCF := arrCFToTrashCF(live)
			// Strip TrashScores from the disk-side spec before diffing.
			// arrCFToTrashCF leaves TrashScores nil (Arr's CF API never
			// returns scores — they live on profile.formatItems instead).
			// Without stripping, every CF that has trash_scores on disk
			// produces N ScoreChange entries pointing at "missing" scores
			// → fingerprint non-empty → drift fires on virtually every
			// CF the first Check after enabling drift detection. The
			// score channel is profile-drift's responsibility; this pass
			// only covers spec content (name, includeCustomFormatWhenRenaming,
			// and per-condition fields).
			diskSpecForDiff := *diskSpec
			diskSpecForDiff.TrashScores = nil
			diff := DiffCFSpecs(&diskSpecForDiff, liveAsTrashCF)
			if !diff.HasAny() {
				continue
			}

			fp := computeCFSpecDiffFingerprint(diff)
			newFP[tid] = fp

			prev := priorFP[tid]
			switch {
			case prev == "":
				// Newly detected drift.
				out.Events = append(out.Events, &CFDriftEvent{
					Event:        CFDriftDetected,
					InstanceID:   inst.ID,
					InstanceName: inst.Name,
					AppType:      inst.Type,
					TrashID:      tid,
					CFName:       cfName,
					Diff:         diff,
				})
			case prev != fp:
				// Drift changed shape — re-notify so the user knows the
				// state evolved (e.g. one condition resolved while a new
				// one appeared).
				out.Events = append(out.Events, &CFDriftEvent{
					Event:        CFDriftDetected,
					InstanceID:   inst.ID,
					InstanceName: inst.Name,
					AppType:      inst.Type,
					TrashID:      tid,
					CFName:       cfName,
					Diff:         diff,
				})
			}
		}

		// Reconciled transitions: any CF that had a fingerprint in the
		// prior pass but is missing from newFP cleared this round.
		// CFs that fell out of managedTIDs (rule disabled, opted out,
		// or CF removed from TRaSH data) get the same persistence-side
		// cleanup (they drop from FingerprintsByInstance) but DO NOT
		// fire Reconciled — they aren't "back in sync", they're "no
		// longer in scope". Emitting Reconciled there would say the
		// drift resolved when really clonarr just stopped looking.
		for tid, prev := range priorFP {
			if prev == "" {
				continue
			}
			if _, stillDrifted := newFP[tid]; stillDrifted {
				continue
			}
			if !managedTIDs[tid] || excludedTIDs[tid] {
				continue
			}
			cfName := tid
			if d, _, ok := resolveCFDiskSpec(tid, appData, customsByID); ok {
				cfName = d.Name
			}
			out.Events = append(out.Events, &CFDriftEvent{
				Event:        CFDriftReconciled,
				InstanceID:   inst.ID,
				InstanceName: inst.Name,
				AppType:      inst.Type,
				TrashID:      tid,
				CFName:       cfName,
				// Diff intentionally nil — there's nothing to render.
			})
		}

		if len(newFP) > 0 || len(priorFP) > 0 {
			out.FingerprintsByInstance[instID] = newFP
		}
		out.CFCount += len(newFP)
	}

	return out
}

// resolveCFDiskSpec returns the disk-side TrashCF for a given trash ID,
// trying TRaSH-Guides data first and falling back to the user's custom
// CF registry. Returns the CF's display name as a convenience for the
// caller's notification + reconciled-event paths. The bool is false
// when neither source has the id, which means a dangling reference
// (custom CF deleted but rule still references it) — drift skips it.
func resolveCFDiskSpec(tid string, appData *AppData, customsByID map[string]CustomCF) (*TrashCF, string, bool) {
	if appData != nil {
		if cf, ok := appData.CustomFormats[tid]; ok && cf != nil {
			return cf, cf.Name, true
		}
	}
	if c, ok := customsByID[tid]; ok {
		return customCFToTrashCF(c), c.Name, true
	}
	return nil, "", false
}

// customCFToTrashCF converts a user-managed CustomCF into the TrashCF
// shape DiffCFSpecs consumes. The Specifications field carries
// ArrSpecification (the format the user authored against), so each
// entry maps cleanly into CFSpecification with the raw Fields blob
// passed through.
func customCFToTrashCF(c CustomCF) *TrashCF {
	specs := make([]CFSpecification, 0, len(c.Specifications))
	for _, s := range c.Specifications {
		specs = append(specs, CFSpecification{
			Name:           s.Name,
			Implementation: s.Implementation,
			Negate:         s.Negate,
			Required:       s.Required,
			Fields:         s.Fields,
		})
	}
	return &TrashCF{
		Name:            c.Name,
		IncludeInRename: c.IncludeInRename,
		Specifications:  specs,
	}
}

// computeCFSpecDiffFingerprint returns a stable sha256 hex digest of
// the CFSpecDiff for use as a state-transition key. Two diffs producing
// the same fingerprint represent the same drift; a different
// fingerprint means the drift evolved and the user should be
// re-notified.
//
// Canonical form: every slice is sorted by a deterministic key before
// JSON-marshaling, so the order of conditions Arr returned in doesn't
// produce a spurious fingerprint change between runs.
func computeCFSpecDiffFingerprint(diff *CFSpecDiff) string {
	if diff == nil {
		return ""
	}
	canonical := struct {
		Added    []ConditionRef    `json:"a,omitempty"`
		Removed  []ConditionRef    `json:"r,omitempty"`
		Changed  []ConditionChange `json:"c,omitempty"`
		Settings []SettingChange   `json:"s,omitempty"`
		Scores   []CFScoreChange   `json:"sc,omitempty"`
	}{
		Added:    append([]ConditionRef(nil), diff.AddedConditions...),
		Removed:  append([]ConditionRef(nil), diff.RemovedConditions...),
		Changed:  append([]ConditionChange(nil), diff.ChangedConditions...),
		Settings: append([]SettingChange(nil), diff.SettingsChanges...),
		Scores:   append([]CFScoreChange(nil), diff.ScoreChanges...),
	}
	sort.SliceStable(canonical.Added, func(i, j int) bool {
		return canonical.Added[i].Name+canonical.Added[i].Implementation <
			canonical.Added[j].Name+canonical.Added[j].Implementation
	})
	sort.SliceStable(canonical.Removed, func(i, j int) bool {
		return canonical.Removed[i].Name+canonical.Removed[i].Implementation <
			canonical.Removed[j].Name+canonical.Removed[j].Implementation
	})
	sort.SliceStable(canonical.Changed, func(i, j int) bool {
		ai := canonical.Changed[i]
		aj := canonical.Changed[j]
		return ai.Name+ai.Field < aj.Name+aj.Field
	})
	sort.SliceStable(canonical.Settings, func(i, j int) bool {
		return canonical.Settings[i].Field < canonical.Settings[j].Field
	})
	sort.SliceStable(canonical.Scores, func(i, j int) bool {
		return canonical.Scores[i].Context < canonical.Scores[j].Context
	})
	buf, err := json.Marshal(canonical)
	if err != nil {
		// Cannot marshal — return a deterministic non-empty marker so
		// the state machine treats subsequent passes as identical
		// rather than oscillating. Should never trip in practice; only
		// possible if the struct shape changes incompatibly.
		return "marshal-error"
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// NotifyCFDriftDetected fires the per-CF drift detected notification.
// Reuses the existing OnDriftDetected per-agent flag (design decision
// §10.5) so the user opts in to "Arr drift" coverage once and gets
// both profile + CF channels. Aggregation across a single Check pass
// happens at the dispatch level — see runOnceInternal for the call
// shape that collects N events into a single message per agent.
func (app *App) NotifyCFDriftDetected(events []*CFDriftEvent) {
	if len(events) == 0 {
		return
	}
	cfg := app.Config.Get()

	// Group by instance + app type so the message reads "3 custom
	// formats have drifted on Radarr (main)" rather than one line per
	// CF, matching profile drift's tone.
	type instGroup struct {
		instanceName string
		appType      string
		cfs          []string
	}
	groups := make(map[string]*instGroup)
	for _, e := range events {
		key := e.InstanceID
		g, ok := groups[key]
		if !ok {
			g = &instGroup{instanceName: e.InstanceName, appType: e.AppType}
			groups[key] = g
		}
		g.cfs = append(g.cfs, e.CFName)
	}

	var titleParts []string
	var descParts []string
	totalCFs := 0
	for _, g := range groups {
		sort.Strings(g.cfs)
		totalCFs += len(g.cfs)
		appLabel := "Radarr"
		if g.appType == "sonarr" {
			appLabel = "Sonarr"
		}
		titleParts = append(titleParts, fmt.Sprintf("%s (%s)", appLabel, g.instanceName))
		descParts = append(descParts, fmt.Sprintf("**%s (%s)** %d custom format%s:\n- %s",
			appLabel, g.instanceName, len(g.cfs),
			plural(len(g.cfs)),
			joinTrunc(g.cfs, "\n- ", 10)))
	}
	sort.Strings(titleParts)
	sort.Strings(descParts)

	title := fmt.Sprintf("Clonarr: %d custom format%s drifted in Arr",
		totalCFs, plural(totalCFs))
	description := ""
	for _, p := range descParts {
		if description != "" {
			description += "\n\n"
		}
		description += p
	}

	payload := NotificationPayload{
		Title:    title,
		Message:  description,
		Color:    0xf85149, // red, matches profile-drift detected
		Severity: NotificationSeverityWarning,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnDriftDetected {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
}

// NotifyCFDriftReconciled fires the cleared-drift companion to
// NotifyCFDriftDetected. Same per-agent gating (OnDriftReconciled) and
// the same per-instance aggregation so a single "3 custom formats are
// back in sync on Radarr (main)" message lands instead of one ping
// per CF.
func (app *App) NotifyCFDriftReconciled(events []*CFDriftEvent) {
	if len(events) == 0 {
		return
	}
	cfg := app.Config.Get()

	type instGroup struct {
		instanceName string
		appType      string
		cfs          []string
	}
	groups := make(map[string]*instGroup)
	for _, e := range events {
		key := e.InstanceID
		g, ok := groups[key]
		if !ok {
			g = &instGroup{instanceName: e.InstanceName, appType: e.AppType}
			groups[key] = g
		}
		g.cfs = append(g.cfs, e.CFName)
	}

	var descParts []string
	totalCFs := 0
	for _, g := range groups {
		sort.Strings(g.cfs)
		totalCFs += len(g.cfs)
		appLabel := "Radarr"
		if g.appType == "sonarr" {
			appLabel = "Sonarr"
		}
		descParts = append(descParts, fmt.Sprintf("**%s (%s)** %d custom format%s:\n- %s",
			appLabel, g.instanceName, len(g.cfs),
			plural(len(g.cfs)),
			joinTrunc(g.cfs, "\n- ", 10)))
	}
	sort.Strings(descParts)

	title := fmt.Sprintf("Clonarr: %d custom format%s back in sync",
		totalCFs, plural(totalCFs))
	description := ""
	for _, p := range descParts {
		if description != "" {
			description += "\n\n"
		}
		description += p
	}

	payload := NotificationPayload{
		Title:    title,
		Message:  description,
		Color:    0x3fb950, // green, matches profile-drift reconciled
		Severity: NotificationSeverityInfo,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnDriftReconciled {
			continue
		}
		app.dispatchNotification(agent, payload)
	}
}

// joinTrunc joins names with sep, capping at limit and appending
// " (+N more)" when more entries existed. Keeps the Discord embed
// readable when a single Check pass produces 20+ drifted CFs.
func joinTrunc(items []string, sep string, limit int) string {
	if len(items) <= limit {
		out := ""
		for i, s := range items {
			if i > 0 {
				out += sep
			}
			out += s
		}
		return out
	}
	visible := items[:limit]
	out := ""
	for i, s := range visible {
		if i > 0 {
			out += sep
		}
		out += s
	}
	return out + fmt.Sprintf("%s(+ %d more)", sep, len(items)-limit)
}
