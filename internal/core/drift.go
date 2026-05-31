package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"clonarr/internal/arr"
)

// DriftRunner detects Arr-side drift: for each enabled auto-sync rule
// it fetches the current Arr quality profile, builds the target profile
// (what clonarr would push if it synced now), and diffs them field by
// field. Both shapes are the same arr.ArrQualityProfile struct so the
// diff is a direct field walk — no separate comparison engine needed.
//
// Concurrency: runs are serialised via mu so a manual /api/drift/check
// trigger and the scheduled tick can't double-fetch.
type DriftRunner struct {
	app *App
	mu  sync.Mutex
}

// ruleWork pairs a rule with its resolved Instance so per-rule code
// doesn't have to walk Config.Instances on every iteration. Promoted
// to package scope (was a local type) so the CF spec drift pass in
// cf_drift.go can share the eligibility filter without re-deriving it.
type ruleWork struct {
	rule     AutoSyncRule
	instance Instance
}

// NewDriftRunner constructs a DriftRunner bound to the given App.
func NewDriftRunner(app *App) *DriftRunner {
	return &DriftRunner{app: app}
}

// RunOnce walks every eligible auto-sync rule, fetches each rule's
// current Arr profile, diffs against the BuildArrProfile target, and
// persists the aggregate summary to DriftWatch.LastResult. Returns
// the per-rule results so the caller (e.g. /api/drift/check handler)
// can return them inline.
//
// Eligibility filter (kept narrow — drift is read-only so paused
// rules still count; user might want to know an Arr profile drifted
// even on a rule they manually disabled):
//   - rule.OrphanedAt == "" (soft-tombstoned rules skipped — Arr
//     profile is gone, nothing to compare)
//   - rule.ArrProfileID != 0 (rule never finished its first sync)
//   - instance referenced by rule.InstanceID still exists
//   - TRaSH profile referenced by rule.TrashProfileID still loaded
//
// Per-rule errors (Arr unreachable, build failure) collect into the
// aggregate Errors slice but do not abort the walk — one broken
// instance shouldn't hide drift on the rest.
func (d *DriftRunner) RunOnce(ctx context.Context) ([]DriftResult, error) {
	return d.runOnceInternal(ctx)
}

func (d *DriftRunner) runOnceInternal(ctx context.Context) ([]DriftResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	cfg := d.app.Config.Get()
	currentTrashCommit := ""
	if d.app.Trash != nil {
		currentTrashCommit = d.app.Trash.CurrentCommit()
	}
	// IDs of rules we couldn't reliably check (e.g. TRaSH-pending so the
	// target would be tainted). These get a stale-state clear after the
	// main walk — without it, drift state from a previous successful
	// pass would linger on a row whose pill has already moved to
	// "Updates" via the TRaSH-pending precedence.
	var indeterminateRules []string

	// Collect eligible rules + resolve their instances up front so the
	// hot loop below only touches the filtered set. ruleWork is
	// package-level (see drift_types.go) so the CF spec drift pass can
	// share the same view of "rules to walk this round" without having
	// to re-derive the eligibility filter.
	var work []ruleWork
	for _, r := range cfg.AutoSync.Rules {
		if r.OrphanedAt != "" || r.ArrProfileID == 0 {
			continue
		}
		// Imported-profile rules have no TRaSH profile to BuildArrProfile
		// against. The TrashProfileID lookup below would either fail
		// silently OR (worse) accidentally match the first profile with
		// TrashID == "" and produce a target built from the wrong base.
		// Drift for imported profiles needs a different target-build
		// path that's not implemented yet, so skip explicitly.
		if r.ProfileSource == "imported" {
			continue
		}
		// Skip rules with unsynced local edits — the diff between target
		// (built from current saved state) and Arr (still on last-synced
		// state) IS the user's pending edits, not external Arr drift.
		// Letting drift fire here would double-count: the row would show
		// "pending" badge (from UpdatedAt > LastSyncTime) AND drift
		// PendingChanges, and the Check toast would count the rule under
		// both "pending" and "Arr drift". Wait for the user to sync;
		// next drift pass on the now-clean rule reflects real Arr drift.
		if r.UpdatedAt != "" && (r.LastSyncTime == "" || r.UpdatedAt > r.LastSyncTime) {
			continue
		}
		// Rules with pending TRaSH updates can't be reliably drift-checked
		// — local TRaSH data has moved past what Arr was last synced
		// with, so target (built from CURRENT TRaSH) wouldn't match
		// what we actually pushed. Any diff would mostly reflect the
		// TRaSH update, not an Arr-side edit. Misclassifying that as
		// "Arr drift" is exactly what users hit (e.g. branch switch
		// to TRaSH dev → notification fires saying someone edited the
		// profile). Skip the per-rule check AND clear any stale drift
		// state so the row drops back to "Updates" pill cleanly. Once
		// the user syncs, the next pass runs normally against fresh
		// ground truth.
		if currentTrashCommit != "" && r.LastSyncCommit != "" && r.LastSyncCommit != currentTrashCommit {
			indeterminateRules = append(indeterminateRules, r.ID)
			continue
		}
		inst, ok := d.app.Config.GetInstance(r.InstanceID)
		if !ok {
			continue
		}
		work = append(work, ruleWork{rule: r, instance: inst})
	}

	// Per-instance Arr snapshot cache: many rules can share an instance,
	// so List* calls only happen once per instance per drift run.
	instCache := make(map[string]*arrSnapshot)
	fetchInst := func(inst Instance) *arrSnapshot {
		if s, ok := instCache[inst.ID]; ok {
			return s
		}
		s := &arrSnapshot{}
		client := arr.NewArrClient(inst.URL, inst.APIKey, d.app.HTTPClient)
		s.profiles, s.err = client.ListProfiles()
		if s.err == nil {
			s.cfs, s.err = client.ListCustomFormats()
		}
		if s.err == nil {
			s.qDefs, s.err = client.ListQualityDefinitions()
		}
		if s.err == nil && inst.Type == "radarr" {
			s.langs, s.err = client.ListLanguages()
		}
		instCache[inst.ID] = s
		return s
	}

	now := time.Now().UTC().Format(time.RFC3339)
	results := make([]DriftResult, 0, len(work))
	var errs []string
	driftCount := 0
	// Deferred-dispatch list: per-rule notification events stack up
	// during the walk and fire after Config.Update releases its lock.
	var pendingNotifs []*pendingDriftNotification

	for _, w := range work {
		if ctx.Err() != nil {
			break
		}
		snap := fetchInst(w.instance)
		if snap.err != nil {
			errs = append(errs, fmt.Sprintf("rule %s (%s): %v", w.rule.ID, w.instance.Name, snap.err))
			continue
		}

		// Find the current Arr profile by ID. Missing == soft-orphan
		// territory; the autosync engine handles that flagging path,
		// so drift just skips quietly here.
		var current *arr.ArrQualityProfile
		for i := range snap.profiles {
			if snap.profiles[i].ID == w.rule.ArrProfileID {
				current = &snap.profiles[i]
				break
			}
		}
		if current == nil {
			continue
		}

		appData := d.app.Trash.GetAppData(w.instance.Type)
		if appData == nil {
			errs = append(errs, fmt.Sprintf("rule %s: TRaSH app data unavailable for %s", w.rule.ID, w.instance.Type))
			continue
		}
		var profile *TrashQualityProfile
		for _, p := range appData.Profiles {
			if p.TrashID == w.rule.TrashProfileID {
				profile = p
				break
			}
		}
		if profile == nil {
			// TRaSH profile gone (upstream removed or local data stale)
			// — autosync engine surfaces this separately; drift skips.
			continue
		}

		// Reproduce sync.go's selectedCFs assembly so the target reflects
		// the rule's saved opt-ins / opt-outs vs current TRaSH defaults.
		nameToID := make(map[string]int, len(snap.cfs))
		for _, cf := range snap.cfs {
			nameToID[cf.Name] = cf.ID
		}
		selectedCFs := ComputeTrashDefaults(profile, appData)
		for _, id := range w.rule.SelectedCFs {
			selectedCFs[id] = true
		}
		for _, id := range w.rule.ExcludedCFs {
			delete(selectedCFs, id)
		}
		customCFs := d.app.CustomCFs.List(w.instance.Type)

		target, buildErr := BuildArrProfile(
			profile, appData, snap.qDefs, snap.langs, nameToID,
			selectedCFs, current.Name, w.rule.ScoreOverrides,
			customCFs, w.rule.QualityStructure,
		)
		if buildErr != nil {
			errs = append(errs, fmt.Sprintf("rule %s: build target: %v", w.rule.ID, buildErr))
			continue
		}
		// Mirror sync.go's post-BuildArrProfile override pass. Without
		// this, every rule that sets Language=English (etc) on a Radarr
		// profile flags false drift on every check because target is
		// built from TRaSH default while the live profile reflects the
		// override actually pushed.
		applyRuleOverridesToTarget(target, w.rule.Overrides, snap.langs)

		details := diffArrProfile(current, target, snap.cfs, &w.rule, selectedCFs, nameToID, customCFs, appData)
		result := DriftResult{
			RuleID:        w.rule.ID,
			CheckedAt:     now,
			DriftDetected: len(details) > 0,
			Details:       details,
		}
		if len(details) > 0 {
			result.DriftSummary = summariseDrift(details)
			driftCount++
		}
		results = append(results, result)

		// Per-rule persistence: update WatchState fingerprint, refresh
		// drift-sourced PendingChanges, and queue notification events
		// (fired AFTER the Config.Update closes so the lock isn't held
		// across network calls).
		fp := computeDriftFingerprint(details)
		notif := d.updateRuleDriftState(w.rule.ID, fp, now, details)
		if notif != nil {
			notif.InstanceName = w.instance.Name
			notif.ArrProfileName = current.Name
			notif.AppType = w.instance.Type
			notif.Summary = result.DriftSummary
			notif.Details = details
			pendingNotifs = append(pendingNotifs, notif)
		}
	}

	// Clear stale drift state on rules we couldn't reliably check this
	// pass — typically rules with pending TRaSH updates whose target
	// would be tainted by the new upstream data. Without this, drift
	// state from a previous (clean) pass lingers in pendingChanges +
	// LastDriftFingerprint, so the Quick action modal and the toast
	// counter both keep claiming drift on a row whose pill has dropped
	// to "Updates" via the higher-precedence TRaSH-pending check.
	//
	// No Reconciled notification fires here even when prevFP != "" —
	// the drift state is INDETERMINATE (we can't compute target reliably),
	// not reconciled. Firing "drift resolved" would be a lie. Once the
	// user syncs and LastSyncCommit catches up, the next pass runs the
	// full state machine and emits Reconciled if drift genuinely cleared.
	if len(indeterminateRules) > 0 {
		indetSet := make(map[string]bool, len(indeterminateRules))
		for _, id := range indeterminateRules {
			indetSet[id] = true
		}
		clearedFP := 0
		clearedPC := 0
		_ = d.app.Config.Update(func(c *Config) {
			for i := range c.AutoSync.Rules {
				r := &c.AutoSync.Rules[i]
				if !indetSet[r.ID] {
					continue
				}
				if r.WatchState != nil && r.WatchState.LastDriftFingerprint != "" {
					r.WatchState.LastDriftFingerprint = ""
					r.WatchState.LastDriftNotifiedAt = now
					clearedFP++
				}
				if len(r.PendingChanges) > 0 {
					before := len(r.PendingChanges)
					r.PendingChanges = dropDriftPendingChanges(r.PendingChanges)
					if len(r.PendingChanges) != before {
						clearedPC++
					}
				}
			}
		})
		if clearedFP > 0 || clearedPC > 0 {
			log.Printf("drift: cleared stale state on %d indeterminate rule(s) — %d fingerprint(s), %d pendingChanges set(s)",
				len(indeterminateRules), clearedFP, clearedPC)
		}
	}

	// CF spec drift pass — third detection channel after profile drift
	// (above) and TRaSH-upstream updates (ProfileSyncRunner). Diffs
	// every managed CF's live spec in Arr against the disk spec
	// (TRaSH-Guides or user custom). Re-uses the same instCache we
	// just populated so we never re-query Arr in this Check pass.
	cfPass := runCFSpecDriftPass(d, work, instCache)

	// instancesInScope = the set of instance IDs that had at least one
	// rule eligible this pass. Instances NOT in this set get their
	// CFDriftFingerprints map cleared at persistence so a user who
	// disabled every rule on an instance doesn't leave stale state
	// behind. Transient-fetch-error instances DO stay in scope (they
	// have rules + were in work; the per-tid merge already preserves
	// their existing fingerprints).
	instancesInScope := make(map[string]bool)
	for _, w := range work {
		instancesInScope[w.instance.ID] = true
	}

	// Persist DriftWatch.LastResult + the per-instance
	// CFDriftFingerprints map in a single Config.Update closure so a
	// reader can never see profile-drift and CF-drift state from
	// different passes.
	//
	// Per-tid race-aware merge: an Apply may have landed between
	// fetchInst (where snap.cfs was captured) and now. If the on-disk
	// fingerprint for a tid no longer matches our prior snapshot,
	// Apply cleared it AND fixed the live CF — our newFP for that tid
	// is stale data. Skip the overwrite so the user's intent (Apply)
	// wins. Without this guard, Apply's "back in sync" notification is
	// immediately followed by a stale "drifted" notification from the
	// pass that started before Apply ran.
	//
	// dropped tracks (instID, tid) pairs whose newFP we skipped due
	// to a concurrent write — used to filter the Detected event queue
	// below so we never dispatch notifications for tids the user
	// already resolved during this pass.
	dropped := make(map[string]map[string]bool)
	if updErr := d.app.Config.Update(func(c *Config) {
		if c.DriftWatch == nil {
			c.DriftWatch = &DriftWatch{}
		}
		c.DriftWatch.LastRun = now
		c.DriftWatch.LastResult = &DriftRunResult{
			DriftsDetected: driftCount,
			Errors:         errs,
		}
		for i := range c.Instances {
			// Out-of-scope instances (no eligible rules this pass): clear
			// any lingering fingerprints. Done before the in-scope merge
			// branch so a fresh-disabled instance gets cleaned up the
			// same Check tick the disable lands.
			if !instancesInScope[c.Instances[i].ID] {
				if len(c.Instances[i].CFDriftFingerprints) > 0 {
					c.Instances[i].CFDriftFingerprints = nil
				}
				continue
			}
			updated, touched := cfPass.FingerprintsByInstance[c.Instances[i].ID]
			if !touched {
				continue
			}
			prior := cfPass.PriorByInstance[c.Instances[i].ID]
			current := c.Instances[i].CFDriftFingerprints
			merged := make(map[string]string, len(updated))
			// Carry forward entries Apply cleared or wrote since pass
			// start — they're "intent-aware" relative to our stale view.
			for tid, fp := range current {
				if prior[tid] != fp {
					merged[tid] = fp
				}
			}
			// Apply our newly-computed fingerprints only where the
			// on-disk value still matches our snapshot. The prior may
			// be empty (we just detected drift on a clean CF) — that's
			// fine: current[tid]==prior[tid]=="" means no concurrent
			// write touched this slot.
			for tid, fp := range updated {
				if current[tid] != prior[tid] {
					if dropped[c.Instances[i].ID] == nil {
						dropped[c.Instances[i].ID] = make(map[string]bool)
					}
					dropped[c.Instances[i].ID][tid] = true
					continue
				}
				merged[tid] = fp
			}
			if len(merged) == 0 {
				c.Instances[i].CFDriftFingerprints = nil
				continue
			}
			c.Instances[i].CFDriftFingerprints = merged
		}
	}); updErr != nil {
		return results, fmt.Errorf("persist drift result: %w", updErr)
	}

	// Fire notifications outside the Config.Update closures — provider
	// HTTP calls can block (Discord/Gotify timeouts), so we deliberately
	// don't hold any locks across them. Order matches the per-rule walk
	// so log lines stay correlated. Manual and scheduled entry points
	// both reach here; per-agent event flags decide what actually sends.
	for _, n := range pendingNotifs {
		switch n.Event {
		case driftEventDetected:
			d.app.NotifyDriftDetected(n.summary())
		case driftEventReconciled:
			d.app.NotifyDriftReconciled(n.summary())
		}
	}

	// CF drift events are aggregated into one detected + one reconciled
	// dispatch per Check pass, matching the "3 custom formats drifted"
	// design instead of one ping per CF.
	//
	// Filter out events whose tid was dropped by the per-tid race-aware
	// merge above — those describe drift the user already resolved via
	// Apply during this pass, so notifying about them would contradict
	// Apply's own Reconciled message.
	var cfDetected, cfReconciled []*CFDriftEvent
	for _, e := range cfPass.Events {
		if dropped[e.InstanceID][e.TrashID] {
			continue
		}
		switch e.Event {
		case CFDriftDetected:
			cfDetected = append(cfDetected, e)
		case CFDriftReconciled:
			cfReconciled = append(cfReconciled, e)
		}
	}
	if len(cfDetected) > 0 {
		d.app.NotifyCFDriftDetected(cfDetected)
	}
	if len(cfReconciled) > 0 {
		d.app.NotifyCFDriftReconciled(cfReconciled)
	}

	return results, nil
}

// driftNotificationEvent identifies which lifecycle event to fire for a
// given rule after per-rule state has been persisted.
type driftNotificationEvent int

const (
	driftEventDetected   driftNotificationEvent = 1
	driftEventReconciled driftNotificationEvent = 2
)

// pendingDriftNotification is the deferred record of one fire-on-change
// event collected during the rule walk and dispatched after the
// Config.Update closure releases its lock.
type pendingDriftNotification struct {
	Event          driftNotificationEvent
	RuleID         string
	InstanceName   string
	ArrProfileName string
	AppType        string
	Summary        []string
	Details        []DriftDetail
}

func (p *pendingDriftNotification) summary() DriftChangeSummary {
	return DriftChangeSummary{
		RuleID:         p.RuleID,
		InstanceName:   p.InstanceName,
		ArrProfileName: p.ArrProfileName,
		AppType:        p.AppType,
		Summary:        p.Summary,
		Details:        p.Details,
	}
}

// updateRuleDriftState writes the per-rule WatchState fingerprint +
// refreshes drift-sourced PendingChanges for the given rule, and
// returns a pendingDriftNotification when the change crosses a
// notify boundary (new drift, changed drift, or reconciliation).
// Returns nil when the rule's state didn't meaningfully change since
// the previous run — keeps notifications dedup'd by fingerprint.
//
// Caller fills in the contextual fields (InstanceName, ArrProfileName,
// AppType, Summary, Details) before dispatch.
func (d *DriftRunner) updateRuleDriftState(ruleID, fingerprint, when string, details []DriftDetail) *pendingDriftNotification {
	var notif *pendingDriftNotification
	_ = d.app.Config.Update(func(c *Config) {
		for i := range c.AutoSync.Rules {
			r := &c.AutoSync.Rules[i]
			if r.ID != ruleID {
				continue
			}
			prevFP := ""
			if r.WatchState != nil {
				prevFP = r.WatchState.LastDriftFingerprint
			}
			// State transitions:
			//   prev empty + new empty   → no drift, no event, no PendingChanges
			//   prev empty + new present → first drift detection → detected event
			//   prev present + new differs → drift changed (e.g. more fields drifted) → detected event
			//   prev present + new same  → still drifting, already notified → no event
			//   prev present + new empty → drift reconciled → reconciled event
			switch {
			case prevFP == "" && fingerprint == "":
				// No drift, no work. Make sure no stale drift-source PendingChanges
				// linger from a previous detection that was reconciled before this
				// runner ran (e.g. user re-synced via Apply & Sync between checks).
				r.PendingChanges = dropDriftPendingChanges(r.PendingChanges)
			case prevFP == "" && fingerprint != "":
				if r.WatchState == nil {
					r.WatchState = &WatchState{}
				}
				r.WatchState.LastDriftFingerprint = fingerprint
				r.WatchState.LastDriftNotifiedAt = when
				r.PendingChanges = mergeDriftPendingChanges(r.PendingChanges, details, when)
				notif = &pendingDriftNotification{Event: driftEventDetected, RuleID: ruleID}
			case prevFP != "" && fingerprint != "" && prevFP != fingerprint:
				r.WatchState.LastDriftFingerprint = fingerprint
				r.WatchState.LastDriftNotifiedAt = when
				r.PendingChanges = mergeDriftPendingChanges(r.PendingChanges, details, when)
				notif = &pendingDriftNotification{Event: driftEventDetected, RuleID: ruleID}
			case prevFP != "" && fingerprint != "" && prevFP == fingerprint:
				// Same drift as before — refresh PendingChanges so the
				// timestamps don't go stale, but don't re-notify.
				r.PendingChanges = mergeDriftPendingChanges(r.PendingChanges, details, when)
			case prevFP != "" && fingerprint == "":
				r.WatchState.LastDriftFingerprint = ""
				r.WatchState.LastDriftNotifiedAt = when
				r.PendingChanges = dropDriftPendingChanges(r.PendingChanges)
				notif = &pendingDriftNotification{Event: driftEventReconciled, RuleID: ruleID}
			}
			return
		}
	})
	return notif
}

// dropDriftPendingChanges returns the input slice minus every entry whose
// Source is "drift". Used on reconciliation to clear the drift backlog
// while leaving TRaSH-source entries untouched.
func dropDriftPendingChanges(in []PendingChange) []PendingChange {
	if len(in) == 0 {
		return in
	}
	out := in[:0:0]
	for _, pc := range in {
		if pc.Source == "drift" {
			continue
		}
		out = append(out, pc)
	}
	return out
}

// dropTrashPendingChanges is the mirror — drops everything that is NOT
// drift-sourced. Used by the autosync housekeeping pass to clear stale
// TRaSH-side entries on rules already at the current commit while
// preserving drift entries (which are governed by a separate fingerprint
// + lifecycle, not by upstream commit position).
func dropTrashPendingChanges(in []PendingChange) []PendingChange {
	if len(in) == 0 {
		return in
	}
	out := in[:0:0]
	for _, pc := range in {
		if pc.Source != "drift" {
			continue
		}
		out = append(out, pc)
	}
	return out
}

// mergeDriftPendingChanges produces PendingChange entries from drift
// details and union-merges them onto the rule's existing PendingChanges
// slice. Drift entries use Source="drift" + reuse the existing
// ChangeType vocabulary ("cf-modified" for score drift, "profile-modified"
// for setting drift, "qs-modified" for quality structure drift) so the
// existing UI rendering of PendingChanges (Profile updates tab) handles
// drift entries uniformly with TRaSH-side entries.
func mergeDriftPendingChanges(existing []PendingChange, details []DriftDetail, when string) []PendingChange {
	if len(details) == 0 {
		// All drift gone for this rule — drop drift-sourced entries.
		return dropDriftPendingChanges(existing)
	}
	// Build incoming list, then drop existing drift entries before
	// merging so per-aspect dedup is by (AffectedID, ChangeType) just
	// like TRaSH-side. dropDriftPendingChanges first because the merge
	// function does an "incoming wins" replace — without dropping
	// existing drift first, fields that USED to drift but no longer do
	// would linger forever.
	base := dropDriftPendingChanges(existing)
	incoming := make([]PendingChange, 0, len(details))
	for _, d := range details {
		incoming = append(incoming, driftDetailToPendingChange(d, when))
	}
	return MergePendingChanges(base, incoming)
}

// driftDetailToPendingChange converts one DriftDetail to a PendingChange
// using the existing TRaSH-side ChangeType vocabulary so the UI doesn't
// need to know about drift-specific change types.
func driftDetailToPendingChange(d DriftDetail, when string) PendingChange {
	switch d.Field {
	case "score":
		return PendingChange{
			Source:       "drift",
			DetectedAt:   when,
			ChangeType:   "cf-modified",
			AffectedID:   d.CFName, // CF name is the stable identifier for drift since trash_id may be unknown
			AffectedName: d.CFName,
		}
	case "quality":
		return PendingChange{
			Source:       "drift",
			DetectedAt:   when,
			ChangeType:   "qs-modified",
			AffectedID:   "quality:" + d.CFName,
			AffectedName: d.CFName,
		}
	default:
		return PendingChange{
			Source:       "drift",
			DetectedAt:   when,
			ChangeType:   "profile-modified",
			AffectedID:   "setting:" + d.Field,
			AffectedName: d.Field,
		}
	}
}

// computeDriftFingerprint returns a stable SHA fragment over the sorted
// (Field, CFName, Current, Target) tuples in the drift details. Empty
// input returns "" so a "no drift" check is just an empty-string check.
// 12-char truncation matches ComputeUpstreamFingerprint so collision
// behaviour is the same across both fingerprint types.
func computeDriftFingerprint(details []DriftDetail) string {
	if len(details) == 0 {
		return ""
	}
	parts := make([]string, len(details))
	for i, d := range details {
		parts[i] = fmt.Sprintf("%s|%s|%v|%v", d.Field, d.CFName, d.Current, d.Target)
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:])[:12]
}

// arrSnapshot is the per-instance fetch cache used inside one drift run.
type arrSnapshot struct {
	profiles []arr.ArrQualityProfile
	cfs      []arr.ArrCF
	qDefs    []arr.ArrQualityDefinition
	langs    []arr.ArrLanguage
	err      error
}

// applyRuleOverridesToTarget mutates the freshly-built target Arr
// profile to reflect the rule's saved overrides — mirrors the post-
// BuildArrProfile override block in sync.go's create path (lines
// 1092-1125 at time of writing). Drift detection needs this because
// the live Arr profile reflects post-override state; without applying
// overrides here, every rule with overrides would show false drift on
// every check (most commonly Language: English live vs Original target
// for Radarr rules).
func applyRuleOverridesToTarget(target *arr.ArrQualityProfile, overrides *SyncOverrides, langs []arr.ArrLanguage) {
	if overrides == nil || target == nil {
		return
	}
	if overrides.UpgradeAllowed != nil {
		target.UpgradeAllowed = *overrides.UpgradeAllowed
	}
	if overrides.MinFormatScore != nil {
		target.MinFormatScore = *overrides.MinFormatScore
	}
	if overrides.MinUpgradeFormatScore != nil {
		v := *overrides.MinUpgradeFormatScore
		if v < 1 {
			v = 1
		}
		target.MinUpgradeFormatScore = v
	}
	if overrides.CutoffFormatScore != nil {
		target.CutoffFormatScore = *overrides.CutoffFormatScore
	}
	if overrides.Language != nil {
		for i := range langs {
			if strings.EqualFold(langs[i].Name, *overrides.Language) {
				target.Language = &langs[i]
				break
			}
		}
	}
	if overrides.CutoffQuality != nil && *overrides.CutoffQuality != "__skip__" {
		if cid, err := resolveCutoff(*overrides.CutoffQuality, target.Items); err == nil {
			target.Cutoff = cid
		}
	}
}

// diffArrProfile walks every field on the Arr profile that drift cares
// about — top-level scalars (upgradeAllowed, cutoff, min/cutoff/upgrade
// scores, language), per-CF scores, and the quality-items structure —
// and emits one DriftDetail per divergence. Output order is stable so
// log diffs and UI rendering don't shuffle between runs.
//
// The rule arg + selectedCFs / nameToID / customCFs / appData let the
// per-CF score diff respect rule.Behavior (allow_custom / do_not_adjust)
// and rule.KeepArrCFIDs — without those, drift would fire on every
// Arr-only CF the user added manually and intentionally left alone via
// allow_custom mode.
func diffArrProfile(current, target *arr.ArrQualityProfile, cfs []arr.ArrCF, rule *AutoSyncRule, selectedCFs map[string]bool, nameToID map[string]int, customCFs []CustomCF, appData *AppData) []DriftDetail {
	var out []DriftDetail

	// Scalar settings.
	if current.UpgradeAllowed != target.UpgradeAllowed {
		out = append(out, DriftDetail{Field: "upgradeAllowed", Current: current.UpgradeAllowed, Target: target.UpgradeAllowed})
	}
	if current.Cutoff != target.Cutoff {
		curName := qualityIDName(current.Cutoff, current.Items)
		tgtName := qualityIDName(target.Cutoff, target.Items)
		out = append(out, DriftDetail{Field: "cutoff", Current: curName, Target: tgtName})
	}
	if current.MinFormatScore != target.MinFormatScore {
		out = append(out, DriftDetail{Field: "minFormatScore", Current: current.MinFormatScore, Target: target.MinFormatScore})
	}
	if current.CutoffFormatScore != target.CutoffFormatScore {
		out = append(out, DriftDetail{Field: "cutoffFormatScore", Current: current.CutoffFormatScore, Target: target.CutoffFormatScore})
	}
	if current.MinUpgradeFormatScore != target.MinUpgradeFormatScore {
		out = append(out, DriftDetail{Field: "minUpgradeFormatScore", Current: current.MinUpgradeFormatScore, Target: target.MinUpgradeFormatScore})
	}
	if !sameLanguage(current.Language, target.Language) {
		out = append(out, DriftDetail{Field: "language", Current: languageName(current.Language), Target: languageName(target.Language)})
	}

	// Per-CF scores. Both slices include every Arr CF (Arr stores 0 for
	// unscored), so a flat score compare keyed by Arr CF ID catches:
	//   - score changed (cur=50, tgt=100 → drift)
	//   - extra-in-arr (cur=50, tgt=0 → user scored a CF clonarr doesn't manage)
	//   - removed-in-arr (cur=0, tgt=50 → user zeroed a CF clonarr expects scored)
	idToName := make(map[int]string, len(cfs))
	for _, cf := range cfs {
		idToName[cf.ID] = cf.Name
	}
	tgtScores := make(map[int]int, len(target.FormatItems))
	for _, fi := range target.FormatItems {
		tgtScores[fi.Format] = fi.Score
	}
	// Build the set of Arr CF IDs clonarr considers managed for this
	// rule — every CF resolvable from selectedCFs by name. CFs outside
	// this set are "Arr-only" (user added them directly in Arr without
	// telling clonarr about them) and only count as drift when the
	// rule's Behavior would zero them on next sync.
	managedArrIDs := make(map[int]bool)
	if selectedCFs != nil && nameToID != nil {
		for trashID := range selectedCFs {
			name := ""
			if appData != nil && appData.CustomFormats != nil {
				if cf, ok := appData.CustomFormats[trashID]; ok && cf != nil {
					name = cf.Name
				}
			}
			if name == "" {
				for _, ccf := range customCFs {
					if ccf.ID == trashID {
						name = ccf.Name
						break
					}
				}
			}
			if name == "" {
				continue
			}
			if arrID, ok := nameToID[name]; ok {
				managedArrIDs[arrID] = true
			}
		}
	}
	// KeepArrCFIDs are explicit-preserve markers — never count as drift
	// even when defaults would zero them.
	keepArrIDs := make(map[int]bool, len(rule.KeepArrCFIDs))
	for _, id := range rule.KeepArrCFIDs {
		keepArrIDs[id] = true
	}
	// Behavior gate: when removeMode=allow_custom OR resetMode=do_not_adjust,
	// the sync engine deliberately preserves Arr-only CF scores. Drift
	// detector must mirror that or it produces a permanent false-positive
	// stream for every Arr-only CF the user added.
	preserveUnmanaged := false
	if rule.Behavior != nil {
		if rule.Behavior.RemoveMode == "allow_custom" || rule.Behavior.ResetMode == "do_not_adjust" {
			preserveUnmanaged = true
		}
	}

	var scoreDiffs []DriftDetail
	for _, ci := range current.FormatItems {
		ts := tgtScores[ci.Format]
		if ci.Score == ts {
			continue
		}
		// Unmanaged CFs: only drift when sync would actually change them.
		if !managedArrIDs[ci.Format] {
			if preserveUnmanaged || keepArrIDs[ci.Format] {
				continue
			}
		}
		scoreDiffs = append(scoreDiffs, DriftDetail{
			Field:   "score",
			CFName:  idToName[ci.Format],
			Current: ci.Score,
			Target:  ts,
		})
	}
	sort.Slice(scoreDiffs, func(i, j int) bool { return scoreDiffs[i].CFName < scoreDiffs[j].CFName })
	out = append(out, scoreDiffs...)

	// Quality items structure: compare allowed-set + group structure as
	// flat name → allowed maps. Order changes alone don't count as drift
	// (Arr's items array is order-sensitive for cutoff resolution, but
	// the sync engine already normalises that on push). Reorder-only
	// changes that don't flip any allowed flag are a no-op for the user.
	curAllowed := flattenAllowed(current.Items)
	tgtAllowed := flattenAllowed(target.Items)
	seen := make(map[string]bool, len(curAllowed)+len(tgtAllowed))
	for name, ca := range curAllowed {
		seen[name] = true
		ta, ok := tgtAllowed[name]
		if !ok {
			// Quality exists in current but not target — unusual, would mean
			// Arr added a quality unknown to clonarr's items. Flag as drift.
			out = append(out, DriftDetail{Field: "quality", CFName: name, Current: ca, Target: false})
			continue
		}
		if ca != ta {
			out = append(out, DriftDetail{Field: "quality", CFName: name, Current: ca, Target: ta})
		}
	}
	for name, ta := range tgtAllowed {
		if seen[name] {
			continue
		}
		out = append(out, DriftDetail{Field: "quality", CFName: name, Current: false, Target: ta})
	}

	return out
}

// qualityIDName resolves a quality-or-group ID to its display name by
// walking the profile's items tree. Used for cutoff diff messages so
// the UI shows "cutoff: HDTV-1080p → Bluray-1080p" instead of bare IDs.
func qualityIDName(id int, items []arr.ArrQualityItem) string {
	for _, it := range items {
		if it.Quality != nil && it.Quality.ID == id {
			return it.Quality.Name
		}
		if it.ID != 0 && it.ID == id && it.Name != "" {
			return it.Name
		}
	}
	return fmt.Sprintf("id=%d", id)
}

func sameLanguage(a, b *arr.ArrLanguage) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.ID == b.ID
}

func languageName(l *arr.ArrLanguage) string {
	if l == nil {
		return "(none)"
	}
	if l.Name != "" {
		return l.Name
	}
	return fmt.Sprintf("id=%d", l.ID)
}

// flattenAllowed reduces an items tree to a flat quality-name → allowed
// map. Group rows propagate their allowed flag to each member quality so
// disabling a group reads the same as disabling each of its members
// (Arr enforces that for cutoff resolution anyway).
func flattenAllowed(items []arr.ArrQualityItem) map[string]bool {
	out := make(map[string]bool)
	for _, it := range items {
		if len(it.Items) > 0 {
			for _, m := range it.Items {
				name := ""
				if m.Quality != nil {
					name = m.Quality.Name
				} else if m.Name != "" {
					name = m.Name
				}
				if name == "" {
					continue
				}
				out[name] = it.Allowed
			}
			continue
		}
		name := ""
		if it.Quality != nil {
			name = it.Quality.Name
		} else if it.Name != "" {
			name = it.Name
		}
		if name == "" {
			continue
		}
		out[name] = it.Allowed
	}
	return out
}

// summariseDrift produces one or two short human-readable lines from
// the per-field details for the rule's drift card. Counts per category
// so a profile with many CF-score drifts reads "12 CF scores differ"
// rather than a 12-line wall.
func summariseDrift(details []DriftDetail) []string {
	scoreCount := 0
	qualityCount := 0
	settingsCount := 0
	for _, d := range details {
		switch d.Field {
		case "score":
			scoreCount++
		case "quality":
			qualityCount++
		default:
			settingsCount++
		}
	}
	var out []string
	if scoreCount > 0 {
		noun := "CF score differs"
		if scoreCount != 1 {
			noun = "CF scores differ"
		}
		out = append(out, fmt.Sprintf("%d %s from target", scoreCount, noun))
	}
	if settingsCount > 0 {
		noun := "profile setting"
		if settingsCount != 1 {
			noun = "profile settings"
		}
		out = append(out, fmt.Sprintf("%d %s differ", settingsCount, noun))
	}
	if qualityCount > 0 {
		noun := "quality item"
		if qualityCount != 1 {
			noun = "quality items"
		}
		out = append(out, fmt.Sprintf("%d %s differ", qualityCount, noun))
	}
	return out
}
