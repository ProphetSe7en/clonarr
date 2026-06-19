package core

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// urlUserinfoRE matches the userinfo portion of any URL in arbitrary text.
// Git's stderr commonly echoes partial URLs ("https://user:token@host")
// without the path, so exact-string replacement of the configured remote
// isn't sufficient — we have to find any `://userinfo@host` pattern.
var urlUserinfoRE = regexp.MustCompile(`(https?|git\+ssh|ssh)://[^@\s/]+@`)

// ProfileSyncRunner polls the TRaSH-Guides upstream for new commits between
// scheduled Pull runs. Detection-only — never modifies the local clone, never
// touches Arr. Surfaces "TRaSH update available" badges on rules so users can
// trigger a manual Pull (or wait for the scheduled one) when they're ready.
//
// Current scope:
//   - Empty-clone safety guard (Trash.CurrentCommit() == "" → skip)
//   - git ls-remote against the configured TRaSH branch
//   - Compare local HEAD vs upstream HEAD; update ProfileSync persistence
//   - Detailed commit-range walk + file-to-rule mapping (detectPerRuleChanges)
//   - ExcludedCFs filter so opt-outs don't trigger notifications
//   - Per-rule notification firing for auto-sync-ON rules
//
type ProfileSyncRunner struct {
	app *App

	// refreshMu serialises concurrent Run calls so a manual refresh + scheduled
	// tick can't race on git ls-remote. Held only for the duration of one Run.
	refreshMu sync.Mutex

	// gitLsRemote is the upstream-HEAD lookup. Pluggable so tests can inject a
	// deterministic result without spawning git processes.
	gitLsRemote func(ctx context.Context, remoteURL, branch string) (string, error)
}

// NewProfileSyncRunner constructs an ProfileSyncRunner with the production
// git-ls-remote implementation. Tests should construct directly and override
// gitLsRemote.
func NewProfileSyncRunner(app *App) *ProfileSyncRunner {
	return &ProfileSyncRunner{
		app:         app,
		gitLsRemote: gitLsRemoteHead,
	}
}

// Run performs one upstream-HEAD check. Safe to call concurrently — internal
// mutex serialises actual work.
//
// Behaviour:
//   - If Trash.CurrentCommit() == "" (no local clone yet) → record paused state,
//     return without contacting remote. Single most-important safety guard
//     per the spec — without it, an empty-clone state would surface as
//     "every commit upstream is new" and spam notifications.
//   - If ProfileSync.Sources.TrashUpstream is false → return without contacting remote.
//   - Otherwise → git ls-remote, update ProfileSync with both heads + LastRun.
//
// Always returns nil on the "no work to do" cases (empty clone, disabled);
// returns an error only on actual remote failures so callers can decide
// whether to surface them.
func (uw *ProfileSyncRunner) Run(ctx context.Context) error {
	uw.refreshMu.Lock()
	defer uw.refreshMu.Unlock()

	cfg := uw.app.Config.Get()
	if cfg.ProfileSync == nil {
		return nil // pre-migration state; never happens after first Load()
	}

	// Sources gate — at least one detection source must be on. Manual Pull
	// (via api endpoint) bypasses this check and always does a full pull;
	// the scheduled run only acts on configured sources.
	if !cfg.ProfileSync.Sources.TrashUpstream && !cfg.ProfileSync.Sources.ArrDrift {
		return nil
	}

	// TRaSH-side detection: only runs when the TrashUpstream source is
	// enabled. Mode controls how findings are acted on. When the source
	// is off but ArrDrift is on, this whole block is skipped and we
	// fall through to the drift block below.
	var trashErr error
	if cfg.ProfileSync.Sources.TrashUpstream {
		switch cfg.ProfileSync.Mode {
		case ProfileSyncModeAuto:
			// Auto mode applies auto-sync-ON rules AND badges auto-sync-OFF
			// rules. Detection must run BEFORE the pull: it diffs
			// local-vs-upstream, so once the pull advances local to upstream
			// there is nothing left to detect. It writes per-rule
			// PendingChanges for every non-orphaned rule, including the
			// auto-sync-OFF ones that runPullAndSync's filterEligibleRulesForPull
			// skips. runPullAndSync then applies the ON rules and clears their
			// badge on success (UpdateAutoSyncRuleCommit); OFF rules keep their
			// "Updates" badge. fireAggregateNotify=false: the pull fires its own
			// NotifyRepoUpdate, and OnUpstreamAhead ("you should pull") is
			// nonsensical when we are about to auto-pull. Empty-clone is a valid
			// initial state: detection no-ops and CloneOrPull populates it.
			_ = uw.runDetectionOnly(ctx, false)
			trashErr = uw.runPullAndSync(ctx)
		default:
			// Notify-only / Delayed modes use the ls-remote-only path for
			// detection. The runner then fires notifications + per-rule
			// PendingChange entries without touching Arr.
			trashErr = uw.runDetectionOnly(ctx, true)
		}
	}

	// Arr-side drift detection: runs independently of TRaSH-side checks.
	// Same tick can do both when both sources are enabled. DriftRunner
	// handles its own per-rule fingerprint dedup + notification firing,
	// so calling it on every scheduler tick is fine — repeated drift
	// state doesn't re-notify.
	if cfg.ProfileSync.Sources.ArrDrift && uw.app.DriftRunner != nil {
		// Errors collect into DriftWatch.LastResult.Errors so they're
		// visible to the user via Settings without bubbling up here and
		// masking a successful TRaSH-side check.
		//
		// Apply-automatically mode also re-syncs auto-sync-ON rules that drifted
		// (RunOnceAutoApply), honouring the "when changes are found" setting for
		// Arr-side drift the same way it does for TRaSH updates. Notify / Delayed
		// detect only here (Delayed's apply runs via RunDelayedApply on its own
		// cadence); both leave Arr untouched on this tick.
		if cfg.ProfileSync.Mode == ProfileSyncModeAuto {
			_, _ = uw.app.DriftRunner.RunOnceAutoApply(ctx)
		} else {
			_, _ = uw.app.DriftRunner.RunOnce(ctx)
		}
	}

	// Naming drift+update detection runs independently of the profile/CF drift
	// gate above: UPDATE detection is gated by the TRaSH-updates source and DRIFT
	// by the Arr-drift source, the same split profile-sync uses. (#5: naming
	// updates used to be detected only inside the ArrDrift block, so Notify mode
	// on the schedule showed no marker or notification unless Arr-drift was also
	// enabled. The naming card Check always checked both and was unaffected.)
	if uw.app.DriftRunner != nil {
		nCheckDrift := cfg.ProfileSync.Sources.ArrDrift
		nCheckUpdate := cfg.ProfileSync.Sources.TrashUpstream
		if nCheckDrift || nCheckUpdate {
			if err := uw.app.DriftRunner.RunNamingDrift(nCheckDrift, nCheckUpdate); err != nil {
				log.Printf("profile-sync: naming detection failed: %v", err)
			}
		}
	}

	return trashErr
}

// RunDetectionOnly is the public entry point for the detection-only path.
// Wraps the unexported helper so handlers can trigger a check on demand
// (e.g. the [Check] button in the sidebar footer) regardless of which
// scheduler Mode is active. Same mutex as Run() — concurrent calls serialise.
func (uw *ProfileSyncRunner) RunDetectionOnly(ctx context.Context) error {
	uw.refreshMu.Lock()
	defer uw.refreshMu.Unlock()
	return uw.runDetectionOnly(ctx, true)
}

// runDetectionOnly performs the ls-remote-only check. Empty-clone guard
// applies — comparing against an empty local HEAD would surface every
// upstream commit as "new" and spam the notification path.
func (uw *ProfileSyncRunner) runDetectionOnly(ctx context.Context, fireAggregateNotify bool) error {
	// Unconditional sweep of legacy "profile-modified" generic entries —
	// they're superseded by the granular profile-quality-* / profile-name
	// / profile-formatitem-* types. Runs every detection tick so a single
	// Check is enough to drop them, even when local==upstream and the
	// rest of detection short-circuits.
	_ = uw.app.Config.Update(func(c *Config) {
		for i := range c.AutoSync.Rules {
			r := &c.AutoSync.Rules[i]
			if len(r.PendingChanges) == 0 {
				continue
			}
			cleaned := r.PendingChanges[:0:0]
			changed := false
			for _, pc := range r.PendingChanges {
				if pc.ChangeType == "profile-modified" {
					changed = true
					continue
				}
				cleaned = append(cleaned, pc)
			}
			if changed {
				r.PendingChanges = cleaned
			}
		}
	})

	cfg := uw.app.Config.Get()

	// Empty-clone guard — DO NOT REMOVE. Comparing an empty local HEAD
	// against any non-empty upstream HEAD would surface every TRaSH
	// commit as "new" and fire NotifyUpstreamUpdate spuriously on the
	// very first detection tick for fresh containers. The runner's
	// notification dedup (priorUpstream != upstreamHead) doesn't help
	// here because the prior value is also empty on first run.
	localCommit := uw.app.Trash.CurrentCommit()
	if localCommit == "" {
		log.Printf("profile-sync: TRaSH clone not initialised — skipping detection (next Pull will populate it)")
		return nil
	}
	if !cfg.ProfileSync.Sources.TrashUpstream {
		return nil // ArrDrift-only path not yet implemented; nothing to do here
	}

	remote := cfg.TrashRepo.URL
	branch := cfg.TrashRepo.Branch
	if remote == "" {
		return fmt.Errorf("profile-sync: TRaSH repo URL not configured")
	}
	if branch == "" {
		branch = "master"
	}

	upstreamHead, err := uw.gitLsRemote(ctx, remote, branch)
	if err != nil {
		safeErr := fmt.Errorf("git ls-remote on branch %q failed: %w", branch, err)
		uw.recordError(localCommit, safeErr)
		return safeErr
	}

	// Capture the prior UpstreamHead INSIDE the Config.Update closure so
	// it reflects the actual state at write-time, not a potentially-stale
	// snapshot from cfg.Get() at the top of Run. Without this, a manual
	// Pull interleaving between our snapshot and our persist would let us
	// re-fire a notification even though Pull just synced everything.
	// This is the aggregate-level dedup against the persisted
	// UpstreamHead; per-rule WatchState fingerprints add finer-grained
	// dedup at the rule level (see detectPerRuleChanges).
	var priorUpstream string
	now := time.Now().UTC().Format(time.RFC3339)
	if updErr := uw.app.Config.Update(func(c *Config) {
		if c.ProfileSync == nil {
			return
		}
		// Snapshot under the same lock that's about to do the write —
		// no interleaving possible inside this closure.
		priorUpstream = c.ProfileSync.UpstreamHead
		c.ProfileSync.LastRun = now
		c.ProfileSync.LocalHead = localCommit
		c.ProfileSync.UpstreamHead = upstreamHead
	}); updErr != nil {
		return fmt.Errorf("profile-sync: persist result: %w", updErr)
	}

	// Length-normalised compare. localCommit is the stored 7-char short hash
	// (CurrentCommit), upstreamHead is the full 40-char ls-remote hash, and
	// existing installs may still have an older 9/10-char short hash persisted
	// from the earlier auto-length --short behavior. Truncating the longer side
	// to the shorter's length lets all these mixed forms compare equal for the
	// same commit instead of firing a false "upstream ahead" notification. This
	// is the same prefix-tolerant comparison as commitMatches.
	cmpLocal, cmpUpstream := localCommit, upstreamHead
	if len(cmpLocal) < len(cmpUpstream) {
		cmpUpstream = cmpUpstream[:len(cmpLocal)]
	} else if len(cmpUpstream) < len(cmpLocal) {
		cmpLocal = cmpLocal[:len(cmpUpstream)]
	}
	if cmpUpstream != cmpLocal {
		log.Printf("profile-sync: TRaSH upstream ahead — local=%s upstream=%s", shortHash(localCommit), shortHash(upstreamHead))
		// Fetch commit-range to a dedicated side-ref + walk the file
		// changes + emit per-rule PendingChanges + per-rule fingerprint
		// dedup. Best-effort: failures in this enrichment path fall back
		// to the aggregate notification so users still hear about
		// upstream activity.
		summary := uw.detectPerRuleChanges(ctx, cfg, remote, branch, localCommit, upstreamHead)

		// Clear, plain-language log of what detection found + what happens
		// next, driven by the active Mode. Replaces the bare "per-rule
		// mapping updated N rule(s)" technical line as the headline. In
		// delayed mode this is the user's confirmation that the auto-apply
		// is scheduled (the apply itself logs separately when it fires).
		if summary != nil && summary.AffectedRuleCount > 0 {
			names := affectedRuleNames(summary, 8)
			switch cfg.ProfileSync.Mode {
			case ProfileSyncModeDelayed:
				if cfg.ProfileSync.ApplyDelayMinutes > 0 {
					applyAt := time.Now().Add(time.Duration(cfg.ProfileSync.ApplyDelayMinutes) * time.Minute)
					log.Printf("profile-sync: TRaSH update affects %d profile(s): %s — will apply automatically around %s (%s delay)",
						summary.AffectedRuleCount, names, applyAt.Format("15:04"), humanizeMinutes(cfg.ProfileSync.ApplyDelayMinutes))
				} else {
					log.Printf("profile-sync: TRaSH update affects %d profile(s): %s — delayed mode but no delay set, will not auto-apply",
						summary.AffectedRuleCount, names)
				}
			case ProfileSyncModeNotify:
				log.Printf("profile-sync: TRaSH update affects %d profile(s): %s — notify-only, apply manually with Pull",
					summary.AffectedRuleCount, names)
			default:
				log.Printf("profile-sync: TRaSH update affects %d profile(s): %s",
					summary.AffectedRuleCount, names)
			}
		}

		// Notification. In notify / delayed / manual-Check paths
		// (fireAggregateNotify) the aggregate "TRaSH updates available"
		// message fires once per new upstream commit. In auto mode
		// (fireAggregateNotify is false) the pull below applies the
		// auto-sync-ON rules, so instead we notify only about auto-sync-OFF
		// rules that have a new change-set; they need a manual update. Both
		// reuse the OnUpstreamAhead event. summary may be nil if the per-rule
		// mapping failed, in which case the aggregate path falls back to a
		// commit-hash-only message.
		if fireAggregateNotify {
			if !commitMatches(priorUpstream, upstreamHead) {
				uw.app.NotifyUpstreamUpdate(localCommit, upstreamHead, summary)
			}
		} else if summary != nil && len(summary.ManualUpdateRules) > 0 {
			uw.app.NotifyManualUpdateNeeded(summary.ManualUpdateRules)
		}
	}
	return nil
}

// detectPerRuleChanges fetches the commit-range to a side-ref, walks the
// file changes, classifies them into trash_ids, and updates per-rule
// PendingChanges + WatchState for every sync rule whose profile is
// affected. Returns a summary (affected rule count + unique CF list)
// the caller can feed into the aggregate notification so users see what
// actually changed instead of bare commit hashes. Returns nil on any
// failure path so the caller falls back to the degraded notification.
func (uw *ProfileSyncRunner) detectPerRuleChanges(ctx context.Context, cfg Config, remote, branch, localCommit, upstreamHead string) *UpstreamChangeSummary {
	if err := uw.app.Trash.FetchUpstreamRefspec(ctx, remote, branch); err != nil {
		log.Printf("profile-sync: fetch upstream side-ref failed: %v", err)
		return nil
	}
	changedFiles, err := uw.app.Trash.ChangedFilesSinceLocal(ctx, branch)
	if err != nil {
		log.Printf("profile-sync: walk commit range failed: %v", err)
		return nil
	}
	if len(changedFiles) == 0 {
		return nil // upstream != local but no file diffs — unusual but harmless
	}

	// Bucket changed trash_ids by app-type. The filename slug (e.g.
	// `web-tier-01.json`) is NOT the trash_id — that's a 32-char hash
	// stored inside the JSON content. Resolve it by reading the file
	// from the upstream side-ref, falling back to HEAD for files that
	// were deleted upstream (file gone from side-ref but still in HEAD).
	//
	// For cf-group files we ALSO compute the old-vs-new member diff so
	// the per-rule mapping can emit one PendingChange per affected CF
	// (added to the group, default-flag flipped, etc.) instead of
	// silently skipping group-level changes.
	upstreamRef := upstreamWatchRefPrefix + branch
	changedByApp := make(map[string][]ClassifiedFile)
	groupDiffs := make(map[string]GroupDiff)       // group trash_id → member diff
	profileDiffs := make(map[string]ProfileDiff)   // profile trash_id → content diff
	cfDiffs := make(map[string]CFDiff)             // CF trash_id → content diff
	for _, path := range changedFiles {
		cf := ClassifyTrashFilePath(path)
		if cf.Kind == FileChangeOther || cf.Kind == FileChangeQualitySize {
			continue // QS handled by the quality-sizes auto-sync path; non-data files ignored
		}
		tid, _ := uw.app.Trash.ReadTrashIDFromRef(ctx, upstreamRef, path)
		if tid == "" {
			tid, _ = uw.app.Trash.ReadTrashIDFromRef(ctx, "HEAD", path)
		}
		if tid == "" {
			continue // file gone from both refs or malformed JSON — skip
		}
		cf.TrashID = tid
		if cf.Kind == FileChangeCF {
			oldCF, _ := uw.app.Trash.ReadCFFromRef(ctx, "HEAD", path)
			newCF, _ := uw.app.Trash.ReadCFFromRef(ctx, upstreamRef, path)
			cdiff := DiffCFSnapshots(oldCF, newCF)
			if !cdiff.IsEmpty() {
				cfDiffs[tid] = cdiff
			}
		}
		if cf.Kind == FileChangeCFGroup {
			oldMembers, _ := uw.app.Trash.ReadCFGroupMembersFromRef(ctx, "HEAD", path)
			newMembers, _ := uw.app.Trash.ReadCFGroupMembersFromRef(ctx, upstreamRef, path)
			diff := DiffCFGroupMembers(oldMembers, newMembers)
			if diff.IsEmpty() {
				continue // file changed but member list unchanged (e.g. comment/description tweak)
			}
			groupDiffs[tid] = diff
		}
		if cf.Kind == FileChangeQualityProfile {
			oldProf, _ := uw.app.Trash.ReadQualityProfileFromRef(ctx, "HEAD", path)
			newProf, _ := uw.app.Trash.ReadQualityProfileFromRef(ctx, upstreamRef, path)
			pdiff := DiffQualityProfile(oldProf, newProf)
			if pdiff.IsEmpty() {
				continue // file changed but no actionable aspect (e.g. trash_id + name only)
			}
			profileDiffs[tid] = pdiff
		}
		changedByApp[cf.AppType] = append(changedByApp[cf.AppType], cf)
	}
	if len(changedByApp) == 0 {
		return nil
	}

	// For each rule, compute the affected trash_id set + per-rule
	// fingerprint dedup. Updates land via a single Config.Update so
	// multiple rule changes batch into one disk write.
	snap := uw.app.Trash.Snapshot()
	if snap == nil {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	perRuleUpdates := uw.buildPerRuleUpdates(cfg, snap, changedByApp, groupDiffs, profileDiffs, cfDiffs, upstreamHead, now)
	if len(perRuleUpdates) == 0 {
		return nil
	}
	if err := uw.app.Config.Update(func(c *Config) {
		for i := range c.AutoSync.Rules {
			r := &c.AutoSync.Rules[i]
			upd, ok := perRuleUpdates[r.ID]
			if !ok {
				continue
			}
			// Race guard: if a concurrent Update / Sync ran between our
			// detection snapshot (read outside this closure) and this
			// persist, rule.LastSyncCommit may have advanced past the
			// upstream commit our pending entries describe. Re-checking
			// inside the closure (where we hold the config lock) prevents
			// resurrecting just-cleared PendingChanges. Length-normalised
			// compare for the same reason watch.go's gate uses it.
			if commitMatches(r.LastSyncCommit, upstreamHead) {
				continue
			}
			if r.WatchState == nil {
				r.WatchState = &WatchState{}
			}
			r.WatchState.LastUpstreamFingerprint = upd.fingerprint
			if upd.shouldNotify {
				r.WatchState.LastUpstreamNotifiedAt = now
			}
			r.PendingChanges = MergePendingChanges(r.PendingChanges, upd.pending)
			// Sweep out the legacy "profile-modified" generic entry —
			// superseded by granular profile-quality-* / profile-name /
			// profile-formatitem-* types the current detection emits.
			// One-shot cleanup; harmless once no legacy entries remain.
			if len(r.PendingChanges) > 0 {
				cleaned := r.PendingChanges[:0:0]
				for _, pc := range r.PendingChanges {
					if pc.ChangeType == "profile-modified" {
						continue
					}
					cleaned = append(cleaned, pc)
				}
				r.PendingChanges = cleaned
			}
		}
	}); err != nil {
		log.Printf("profile-sync: persist per-rule pending failed: %v", err)
	}
	log.Printf("profile-sync: per-rule mapping updated %d rule(s)", len(perRuleUpdates))

	// Build aggregate summary for the notification message. Deduplicates
	// CFs across rules (one CF affecting 12 rules → one line, not twelve).
	// Sort by app then name for stable ordering across runs.
	summary := &UpstreamChangeSummary{
		AffectedRuleCount: len(perRuleUpdates),
	}
	type itemKey struct{ appType, trashID string }
	seen := make(map[itemKey]bool)
	for _, app := range []string{"radarr", "sonarr"} {
		var ad *AppData
		var cfMap map[string]*TrashCF
		switch app {
		case "radarr":
			ad = &snap.Radarr
			cfMap = snap.Radarr.CustomFormats
		case "sonarr":
			ad = &snap.Sonarr
			cfMap = snap.Sonarr.CustomFormats
		}
		for _, cf := range changedByApp[app] {
			k := itemKey{app, cf.TrashID}
			if seen[k] {
				continue
			}
			// Only include in the summary if at least one rule has it
			// in scope — otherwise it's an upstream change that doesn't
			// matter to the user.
			if !anyRuleHasCF(perRuleUpdates, cf.TrashID) {
				continue
			}
			seen[k] = true
			switch cf.Kind {
			case FileChangeQualityProfile:
				name := cf.TrashID
				for _, p := range ad.Profiles {
					if p != nil && p.TrashID == cf.TrashID {
						name = p.Name
						break
					}
				}
				summary.AffectedProfiles = append(summary.AffectedProfiles, AffectedItem{
					Name:    name,
					AppType: app,
				})
				continue
			}
			name := cf.TrashID // CF / cf-group fallback when not in snapshot yet
			if entry, ok := cfMap[cf.TrashID]; ok && entry != nil {
				name = entry.Name
			} else {
				for _, g := range ad.CFGroups {
					if g != nil && g.TrashID == cf.TrashID {
						name = g.Name
						break
					}
				}
			}
			summary.AffectedCFs = append(summary.AffectedCFs, AffectedItem{
				Name:    name,
				AppType: app,
			})
		}
	}

	// Affected sync rules — resolve each affected rule to the profile name
	// the user recognises. Prefer the Arr profile name from sync history
	// (what the Sync Rules table shows), fall back to the TRaSH profile name
	// from the snapshot, then a generic label. Sorted by app + name for
	// stable notification ordering.
	instType := make(map[string]string, len(cfg.Instances))
	for _, in := range cfg.Instances {
		instType[in.ID] = in.Type
	}
	for ruleID := range perRuleUpdates {
		var rule *AutoSyncRule
		for i := range cfg.AutoSync.Rules {
			if cfg.AutoSync.Rules[i].ID == ruleID {
				rule = &cfg.AutoSync.Rules[i]
				break
			}
		}
		if rule == nil {
			continue
		}
		appType := instType[rule.InstanceID]
		name := ""
		// Arr profile name from sync history (matches the Sync Rules table).
		for _, h := range cfg.SyncHistory {
			if h.InstanceID == rule.InstanceID && h.ArrProfileID == rule.ArrProfileID && h.ArrProfileName != "" {
				name = h.ArrProfileName
				break
			}
		}
		// Fall back to the TRaSH profile name from the snapshot.
		if name == "" && rule.TrashProfileID != "" {
			var profiles []*TrashQualityProfile
			if appType == "sonarr" {
				profiles = snap.Sonarr.Profiles
			} else {
				profiles = snap.Radarr.Profiles
			}
			for _, p := range profiles {
				if p != nil && p.TrashID == rule.TrashProfileID {
					name = p.Name
					break
				}
			}
		}
		if name == "" {
			name = fmt.Sprintf("Profile #%d", rule.ArrProfileID)
		}
		summary.AffectedRules = append(summary.AffectedRules, AffectedItem{Name: name, AppType: appType})
		// Auto-mode manual-update list: rules with auto-sync OFF and a NEW
		// change-set (shouldNotify) are skipped by the apply pass, so the
		// caller notifies the user to apply them by hand. shouldNotify gives
		// per-rule dedup so repeated ticks with no new change stay silent.
		if !rule.Enabled && perRuleUpdates[ruleID].shouldNotify {
			summary.ManualUpdateRules = append(summary.ManualUpdateRules, AffectedItem{Name: name, AppType: appType})
		}
	}
	sortAffected := func(items []AffectedItem) {
		sort.Slice(items, func(i, j int) bool {
			if items[i].AppType != items[j].AppType {
				return items[i].AppType < items[j].AppType
			}
			return items[i].Name < items[j].Name
		})
	}
	sortAffected(summary.AffectedRules)
	sortAffected(summary.ManualUpdateRules)

	return summary
}

// anyRuleHasCF reports whether at least one perRuleUpdate's pending list
// contains the given trash_id. Used to filter the summary down to CFs the
// user actually cares about. Matches both direct AffectedID equality and
// the "trashID:context" prefix form used for cf-score entries.
func anyRuleHasCF(updates map[string]perRuleUpdate, trashID string) bool {
	prefix := trashID + ":"
	for _, upd := range updates {
		for _, pc := range upd.pending {
			if pc.AffectedID == trashID {
				return true
			}
			if strings.HasPrefix(pc.AffectedID, prefix) {
				return true
			}
		}
	}
	return false
}

// perRuleUpdate is the planned mutation for one rule, computed outside the
// Config.Update closure so the closure stays trivially short.
type perRuleUpdate struct {
	fingerprint  string
	shouldNotify bool
	pending      []PendingChange
}

func (uw *ProfileSyncRunner) buildPerRuleUpdates(cfg Config, snap *TrashData, changedByApp map[string][]ClassifiedFile, groupDiffs map[string]GroupDiff, profileDiffs map[string]ProfileDiff, cfDiffs map[string]CFDiff, upstreamHead, now string) map[string]perRuleUpdate {
	out := make(map[string]perRuleUpdate)
	for _, rule := range cfg.AutoSync.Rules {
		// Orphaned rules (profile deleted from Arr) skip — there's nothing
		// to sync to. Disabled rules ARE detected so the user still sees
		// "Outdated" badges and notifications; the Pull-time filter
		// (filterEligibleRulesForPull) handles the "don't auto-sync"
		// part separately.
		if rule.OrphanedAt != "" {
			continue
		}
		var inst Instance
		var found bool
		for _, i := range cfg.Instances {
			if i.ID == rule.InstanceID {
				inst, found = i, true
				break
			}
		}
		if !found {
			continue
		}
		changedForApp := changedByApp[inst.Type]
		if len(changedForApp) == 0 {
			continue
		}
		var ad *AppData
		switch inst.Type {
		case "radarr":
			ad = &snap.Radarr
		case "sonarr":
			ad = &snap.Sonarr
		default:
			continue
		}
		var profile *TrashQualityProfile
		for _, p := range ad.Profiles {
			if p != nil && p.TrashID == rule.TrashProfileID {
				profile = p
				break
			}
		}
		if profile == nil {
			continue
		}

		// Direct CF file changes — match against profile-eligible set.
		// Quality-profile JSON changes — match directly against the rule's
		// TrashProfileID (the profile JSON IS the rule's target).
		changedTrashIDs := make([]string, 0, len(changedForApp))
		profileFileChanged := false
		for _, cf := range changedForApp {
			if cf.Kind == FileChangeCF {
				changedTrashIDs = append(changedTrashIDs, cf.TrashID)
			} else if cf.Kind == FileChangeQualityProfile && cf.TrashID == rule.TrashProfileID {
				profileFileChanged = true
			}
		}
		affected := RuleAffectedTrashIDs(&rule, profile, ad, changedTrashIDs)

		// Build trash_id → CF name lookup from the snapshot so tooltips
		// show "WEB Tier 01" instead of an opaque 32-char hash. CFs that
		// aren't in the snapshot (e.g. brand-new ones added in this
		// commit, before Pull syncs them locally) fall back to empty
		// AffectedName — UI then renders the trash_id.
		nameByTrashID := make(map[string]string, len(ad.CustomFormats))
		for _, c := range ad.CustomFormats {
			nameByTrashID[c.TrashID] = c.Name
		}

		// Group-level changes — for each changed cf-group, check if this
		// profile uses it (via ad.CFGroups[i].QualityProfiles.Include),
		// then emit a PendingChange per member-diff entry. This is the
		// detection-side parity for the sync engine, which has always
		// understood cf-group structure.
		groupBased := buildGroupChangePending(rule, profile, ad, changedForApp, groupDiffs, nameByTrashID, upstreamHead, now)

		// Combine the streams. Up to three can apply to the same rule
		// when upstream touches CF file + cf-group file + profile file
		// in the same commit range. Merge by trash_id+changeType so we
		// don't double-emit the same logical change.
		pending := make([]PendingChange, 0, len(affected)+len(groupBased)+1)
		emitOne := func(changeType, affectedID, name string) {
			pending = append(pending, PendingChange{
				Source:       "trash",
				DetectedAt:   now,
				CommitHash:   upstreamHead,
				ChangeType:   changeType,
				AffectedID:   affectedID,
				AffectedName: name,
			})
		}
		for _, tid := range affected {
			cfName := nameByTrashID[tid]
			cdiff, hasDiff := cfDiffs[tid]
			if !hasDiff || cdiff.IsEmpty() {
				// File changed but no actionable per-aspect detail (parse
				// failed or only whitespace tweaked). Fall back to the
				// generic cf-modified signal so the user still sees the
				// CF flagged as out-of-sync.
				emitOne("cf-modified", tid, cfName)
				continue
			}
			if cdiff.NameChanged {
				display := cfName
				if display == "" {
					display = cdiff.NewName
				}
				emitOne("cf-name", tid, display+" — renamed: "+cdiff.OldName+" → "+cdiff.NewName)
			}
			// Score-context relevance gate: only emit cf-score entries
			// for the ONE context this rule's profile actually resolves
			// to. Without this, every score-context change on a CF would
			// flood the rule's pendingChanges even when the rule's
			// scoring uses a different context that didn't move.
			// Example: SQP-4 has scoreCtx="sqp-4-ma-hybrid"; WEB Tier 01
			// changes "default" 1850→1900 but stays at 30 for SQP-4.
			// Without this filter the user saw "WEB Tier 01 will change"
			// in the modal, then Apply only changed Extras (the one CF
			// where scoreCtx actually moved).
			scoreCtx := profile.TrashScoreSet
			if scoreCtx == "" {
				scoreCtx = "default"
			}
			relevantCtx := "default"
			if cf, ok := ad.CustomFormats[tid]; ok && cf != nil {
				if _, has := cf.TrashScores[scoreCtx]; has {
					relevantCtx = scoreCtx
				}
			}
			// Rule-level score override pins the score regardless of
			// what TRaSH says — TRaSH score changes are no-ops for
			// the rule until the user clears the override.
			_, hasUserOverride := rule.ScoreOverrides[tid]
			for ctx, sc := range cdiff.ScoreChanges {
				if ctx != relevantCtx {
					continue
				}
				if hasUserOverride {
					continue
				}
				label := ctx
				if label == "default" {
					label = "default score"
				} else {
					label = ctx + " score"
				}
				emitOne("cf-score", tid+":"+ctx, cfName+" — "+label+": "+fmt.Sprintf("%d → %d", sc.Old, sc.New))
			}
			if cdiff.SpecsChanged {
				// Prefer per-condition bullets when the structured
				// diff parsed cleanly, so the Pending-updates panel
				// shows "+ added X / ~ Y changed" rather than the
				// bare "conditions changed" placeholder. Falls back
				// when SpecDiff is nil (parse failure or weird
				// upstream shape).
				if cdiff.SpecDiff != nil {
					for _, c := range cdiff.SpecDiff.AddedConditions {
						label := c.Name
						if c.Value != "" {
							label += ": " + c.Value
						}
						emitOne("cf-specs", tid+":+"+c.Name, cfName+" - added "+label)
					}
					for _, c := range cdiff.SpecDiff.RemovedConditions {
						emitOne("cf-specs", tid+":-"+c.Name, cfName+" - removed "+c.Name)
					}
					for _, c := range cdiff.SpecDiff.ChangedConditions {
						emitOne("cf-specs", tid+":~"+c.Name+":"+c.Field, cfName+" - "+c.Name+" "+c.Field+": "+c.Before+" -> "+c.After)
					}
				} else {
					emitOne("cf-specs", tid, cfName+" - conditions changed")
				}
			}
			if cdiff.RenameFlagChanged {
				state := "no"
				if cdiff.RenameFlagNow {
					state = "yes"
				}
				emitOne("cf-rename-flag", tid, cfName+" — include in file rename: "+state)
			}
		}
		if profileFileChanged {
			pdiff := profileDiffs[rule.TrashProfileID]
			emit := func(changeType, affectedID, name string) {
				pending = append(pending, PendingChange{
					Source:       "trash",
					DetectedAt:   now,
					CommitHash:   upstreamHead,
					ChangeType:   changeType,
					AffectedID:   affectedID,
					AffectedName: name,
				})
			}
			if pdiff.NameChanged {
				emit("profile-name", rule.TrashProfileID, "Renamed: "+pdiff.OldName+" → "+pdiff.NewName)
			}
			if pdiff.UpgradeAllowedChanged {
				state := "no longer allowed"
				if pdiff.UpgradeAllowedNow {
					state = "now allowed"
				}
				emit("profile-upgrade-allowed", rule.TrashProfileID, "Upgrades — "+state)
			}
			if pdiff.LanguageChanged {
				emit("profile-language", rule.TrashProfileID, "Language: "+pdiff.OldLanguage+" → "+pdiff.NewLanguage)
			}
			if pdiff.MinFormatScoreChanged {
				emit("profile-min-format-score", rule.TrashProfileID, fmt.Sprintf("Minimum Custom Format Score: %d → %d", pdiff.MinFormatScore.Old, pdiff.MinFormatScore.New))
			}
			if pdiff.CutoffFormatScoreChanged {
				emit("profile-cutoff-format-score", rule.TrashProfileID, fmt.Sprintf("Upgrade Until Custom Format Score: %d → %d", pdiff.CutoffFormatScore.Old, pdiff.CutoffFormatScore.New))
			}
			if pdiff.MinUpgradeFormatScoreChanged {
				emit("profile-min-upgrade-format-score", rule.TrashProfileID, fmt.Sprintf("Minimum Custom Format Score Increment: %d → %d", pdiff.MinUpgradeFormatScore.Old, pdiff.MinUpgradeFormatScore.New))
			}
			if pdiff.CutoffChanged {
				emit("profile-quality-cutoff", rule.TrashProfileID, "Cutoff: "+pdiff.OldCutoff+" → "+pdiff.NewCutoff)
			}
			for _, c := range pdiff.ItemsChanged {
				state := "no longer allowed"
				if c.NowAllowed {
					state = "now allowed"
				}
				emit("profile-quality-allowed", c.Name, c.Name+" — "+state)
			}
			for _, c := range pdiff.ItemsAdded {
				emit("profile-quality-added", c.Name, c.Name+" — new quality added")
			}
			for _, c := range pdiff.ItemsRemoved {
				emit("profile-quality-removed", c.Name, c.Name+" — quality removed")
			}
			for _, c := range pdiff.FIAdded {
				name := c.Name
				if cached := nameByTrashID[c.TrashID]; cached != "" {
					name = cached
				}
				emit("profile-formatitem-added", c.TrashID, name+" — added to profile")
			}
			for _, c := range pdiff.FIRemoved {
				name := c.Name
				if cached := nameByTrashID[c.TrashID]; cached != "" {
					name = cached
				}
				emit("profile-formatitem-removed", c.TrashID, name+" — removed from profile")
			}
		}
		// Dedup: if a CF appears in both streams (e.g. CF file changed AND
		// group entry changed in same commit range), keep the more
		// specific change-type from the group stream — it carries the
		// "added/removed/default-flipped" semantic that's more actionable.
		seen := make(map[string]bool, len(pending))
		for _, pc := range pending {
			seen[pc.AffectedID+"|"+pc.ChangeType] = true
		}
		for _, pc := range groupBased {
			if !seen[pc.AffectedID+"|"+pc.ChangeType] {
				pending = append(pending, pc)
			}
		}
		if len(pending) == 0 {
			continue
		}
		// Fingerprint covers ALL affected trash_ids so any subset change
		// produces a different fingerprint and re-fires notification.
		fpIDs := make([]string, 0, len(pending))
		for _, pc := range pending {
			fpIDs = append(fpIDs, pc.AffectedID+":"+pc.ChangeType)
		}
		fp := ComputeUpstreamFingerprint(fpIDs)
		var prior string
		if rule.WatchState != nil {
			prior = rule.WatchState.LastUpstreamFingerprint
		}
		shouldNotify := fp != prior
		out[rule.ID] = perRuleUpdate{
			fingerprint:  fp,
			shouldNotify: shouldNotify,
			pending:      pending,
		}
	}
	return out
}

// buildGroupChangePending emits one PendingChange per cf-group member-diff
// entry that affects the given rule. "Affects" means:
//   - The changed cf-group is included by this profile (via
//     ad.CFGroups[i].QualityProfiles.Include[profile.Name])
//   - The member-level diff carries semantics that change the rule's
//     effective CF set on next sync (Added with default-on/required,
//     Removed, default flag flipped)
//
// Skips RequiredOn/RequiredOff for now — those don't change which CFs
// are pushed to Arr, only whether the user can deselect them.
func buildGroupChangePending(rule AutoSyncRule, profile *TrashQualityProfile, ad *AppData, changedForApp []ClassifiedFile, groupDiffs map[string]GroupDiff, nameByTrashID map[string]string, upstreamHead, now string) []PendingChange {
	var out []PendingChange
	excluded := make(map[string]bool, len(rule.ExcludedCFs))
	for _, ex := range rule.ExcludedCFs {
		excluded[ex] = true
	}
	for _, cf := range changedForApp {
		if cf.Kind != FileChangeCFGroup {
			continue
		}
		diff, ok := groupDiffs[cf.TrashID]
		if !ok {
			continue
		}
		groupName, profileUses := lookupGroupForProfile(ad, cf.TrashID, profile)
		if !profileUses {
			continue
		}
		emit := func(m GroupCFMember, changeType, suffix string) {
			if excluded[m.TrashID] {
				return // user opted out of this CF — no signal
			}
			name := m.Name
			if cached := nameByTrashID[m.TrashID]; cached != "" {
				name = cached
			}
			if suffix != "" {
				name = name + " — " + suffix + " " + groupName
			}
			out = append(out, PendingChange{
				Source:       "trash",
				DetectedAt:   now,
				CommitHash:   upstreamHead,
				ChangeType:   changeType,
				AffectedID:   m.TrashID,
				AffectedName: name,
			})
		}
		for _, m := range diff.Added {
			// Only fire when the new entry will actually be pushed —
			// default-on or required-on entries get applied at sync;
			// default-off optional CFs need explicit user opt-in.
			if m.Default || m.Required {
				emit(m, "cf-group-added", "added to")
			}
		}
		for _, m := range diff.Removed {
			emit(m, "cf-group-removed", "removed from")
		}
		for _, m := range diff.DefaultOn {
			emit(m, "cf-group-default-on", "now default-on in")
		}
		for _, m := range diff.DefaultOff {
			emit(m, "cf-group-default-off", "no longer default-on in")
		}
	}
	return out
}

// lookupGroupForProfile finds the cf-group with the given trash_id in the
// snapshot and reports whether the profile uses it (via
// QualityProfiles.Include[profile.Name]). Returns the group's name as the
// first return so callers can use it in tooltip labels.
func lookupGroupForProfile(ad *AppData, groupTrashID string, profile *TrashQualityProfile) (string, bool) {
	if ad == nil || profile == nil {
		return "", false
	}
	for _, g := range ad.CFGroups {
		if g == nil || g.TrashID != groupTrashID {
			continue
		}
		if _, ok := g.QualityProfiles.Include[profile.Name]; ok {
			return g.Name, true
		}
		return g.Name, false
	}
	return "", false
}

// runPullAndSync dispatches to the canonical App.RunPullAndSync flow with
// the scheduled-pull source tag. All telemetry (DebugLog op-trace,
// SetPullError, ProfileSync state persistence, DiffPull detail lines,
// AfterPullCallback, AutoSyncAfterPull) is unified there — manual Pull
// (handleTrashPull) goes through the same method.
func (uw *ProfileSyncRunner) runPullAndSync(_ context.Context) error {
	log.Printf("profile-sync: scheduled pull-and-sync starting (mode=auto)")
	return uw.app.RunPullAndSync(SourceAutoPullInterval)
}

// recordError persists the failure so /api/watch/update can surface it,
// without contaminating UpstreamHead with stale data. The error must already
// be URL-redacted by the caller — runErr is logged verbatim.
func (uw *ProfileSyncRunner) recordError(localCommit string, runErr error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if err := uw.app.Config.Update(func(c *Config) {
		if c.ProfileSync == nil {
			return
		}
		c.ProfileSync.LastRun = now
		c.ProfileSync.LocalHead = localCommit
		// Leave UpstreamHead untouched — previous successful value is still
		// the best signal until the next successful run.
	}); err != nil {
		log.Printf("profile-sync: persist error-state failed: %v", err)
	}
	log.Printf("profile-sync: ls-remote failed: %v", runErr)
}

// gitLsRemoteHead is the production implementation. Resolves the HEAD commit
// of `branch` on `remoteURL` via `git ls-remote` without modifying any local
// state. Context applies a hard timeout so a stuck remote can't pin the
// watcher goroutine indefinitely.
//
// Hardening:
//   - `--` separator between options and the URL so a maliciously-crafted
//     `--upload-pack=...` URL is treated as a positional arg, not a flag.
//   - GIT_TERMINAL_PROMPT=0 + GIT_ASKPASS=/bin/true so a private repo with
//     missing/wrong credentials fails fast instead of blocking on a
//     stdin-credential prompt (would hang until the context timeout).
//   - Stderr captured separately and surfaced (URL-redacted) in errors so
//     flag-injection symptoms ("unknown option --upload-pack=...") become
//     visible.
//   - All error messages route through redactGitError so credentials
//     embedded in the URL (https://user:token@host/...) never reach logs
//     or the API response.
func gitLsRemoteHead(ctx context.Context, remoteURL, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// `--` separator: protects against URLs that start with `-` being
	// interpreted as git options. Without this, `--upload-pack=evil` as the
	// URL would invoke arbitrary commands on the local machine.
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--", remoteURL, "refs/heads/"+branch)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",     // never prompt on stdin for missing credentials
		"GIT_ASKPASS=/bin/true",     // any credential helper invocation returns empty without prompting
		"GIT_SSH_COMMAND=ssh -oBatchMode=yes -oStrictHostKeyChecking=accept-new", // SSH remotes also fail-fast
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", redactGitError(remoteURL, branch, stderr.String(), err)
	}
	// Output: "<sha>\trefs/heads/<branch>\n"
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", fmt.Errorf("git ls-remote returned empty output for branch %q", branch)
	}
	parts := strings.Fields(line)
	if len(parts) < 1 || len(parts[0]) < 7 {
		return "", fmt.Errorf("git ls-remote returned unexpected format: %q", line)
	}
	return parts[0], nil
}

// redactGitError builds an error message that includes git's stderr (for
// diagnostic value — e.g. "unknown option" surfaces flag-injection attempts,
// "authentication failed" tells the user what to fix) but strips any
// occurrence of the remote URL so embedded credentials never leak.
//
// Pass remoteURL so we can also redact userinfo from any parsed URL form
// (https://user:token@host/path → https://host/path). The redacted URL is
// returned alongside the stderr tail so logs still show *which kind* of
// remote failed without revealing credentials.
func redactGitError(remoteURL, branch, stderr string, err error) error {
	tail := strings.TrimSpace(stderr)
	if len(tail) > 240 {
		tail = tail[:240] + "..."
	}
	// Strip credentials from any URL we might emit.
	safeURL := redactURL(remoteURL)
	// Strip userinfo from any URL appearing in the stderr tail — git often
	// echoes partial URLs ("https://user:token@host" without path) on auth
	// failures, so exact-string replacement of the configured remote isn't
	// enough. The regex catches any `scheme://user@host` pattern and rewrites
	// it to `scheme://host`.
	tail = urlUserinfoRE.ReplaceAllStringFunc(tail, func(match string) string {
		// match = "https://user:token@" — keep scheme, drop userinfo + @
		schemeEnd := strings.Index(match, "://")
		return match[:schemeEnd+3]
	})
	if tail != "" {
		return fmt.Errorf("git ls-remote (%s, branch %q): %s: %w", safeURL, branch, tail, err)
	}
	return fmt.Errorf("git ls-remote (%s, branch %q): %w", safeURL, branch, err)
}

// redactURL strips userinfo (user:password) from a URL. Returns the URL
// unchanged if it doesn't parse — better to leak nothing than to leak parts.
// SSH URLs (git@host:path) have no userinfo to strip and are returned as-is.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = nil
	return u.String()
}

// shortHash returns the first 7 chars of a commit hash for logging — enough
// for human pattern-matching without flooding the log.
func shortHash(h string) string {
	if len(h) <= 7 {
		return h
	}
	return h[:7]
}

// commitMatches reports whether two commit-hash strings refer to the same
// git commit, tolerating mixed short (7-char) vs full (40-char) forms by
// truncating the longer side to the shorter's length. Used by both the
// runDetectionOnly gate and the per-rule persist race guard so they
// agree on equality. See also Trash.CurrentCommit() which keeps the
// 7-char form for UI compatibility.
func commitMatches(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	return a[:n] == b[:n]
}

// CommitMatches is the exported form of commitMatches for callers outside the
// core package (e.g. the api layer's sync-source labelling).
func CommitMatches(a, b string) bool { return commitMatches(a, b) }
