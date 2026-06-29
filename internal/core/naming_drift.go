package core

import (
	"context"
	"time"
)

// namingDriftField: one newly-drifted field, with the patterns so the notification
// can show what changed (Now vs what Sync would re-apply).
type namingDriftField struct {
	Field    string
	Current  string // the instance's current (drifted) pattern
	Expected string // the intended scheme's current guide pattern (what Sync re-applies)
}

// namingDriftEvent: a newly-detected naming drift on one instance, for notification.
type namingDriftEvent struct {
	InstanceID   string
	InstanceName string
	AppType      string
	Fields       []namingDriftField
}

// namingDriftPassResult carries what to persist + what to notify after the pass.
type namingDriftPassResult struct {
	driftFP         map[string]map[string]string // instID -> field -> current Arr pattern fp (drifted)
	updateFP        map[string]map[string]string // instID -> field -> guide pattern fp (update available)
	checkedOK       map[string]bool              // instances fetched + compared this pass
	detected        []namingDriftEvent           // newly-drifted only, for notification
	detectedUpdates []namingDriftEvent           // newly update-available only, for notification
	detectedBoth    []namingDriftEvent           // newly both drifted AND update-available (one combined notification)
}

// runNamingDriftPass checks, for every field clonarr has synced (Config.NamingApplied
// — manual OR auto), two things against what clonarr last applied:
//   - DRIFT: the instance's CURRENT Arr pattern differs from the applied one
//     (someone changed naming in Arr).
//   - UPDATE: the field's intended scheme has a NEWER pattern in the TRaSH guide
//     than what was applied (a guide update is available).
//
// It only FLAGS — never rewrites Arr (re-applying is the user's Sync / auto-sync).
// Per instance, mirroring the CF-spec drift pass.
//
// checkDrift / checkUpdate gate the two halves independently, mirroring how
// profile-sync gates its TRaSH-update half (TrashUpstream source) separately
// from its Arr-drift half (ArrDrift source). The live Arr value is captured
// regardless of the flags, since the update notification needs it for the
// "Now" line.
func runNamingDriftPass(d *DriftRunner, checkDrift, checkUpdate bool) namingDriftPassResult {
	cfg := d.app.Config.Get()
	res := namingDriftPassResult{
		driftFP:   map[string]map[string]string{},
		updateFP:  map[string]map[string]string{},
		checkedOK: map[string]bool{},
	}
	// An instance is in scope if clonarr has synced ANY naming field on it — either
	// an applied record (manual or auto) OR an auto-sync binding. Bindings are
	// included so fields auto-synced before the per-field applied-record existed
	// (or not yet re-applied) are still drift-checked, using the binding's
	// fingerprint/scheme as the applied baseline.
	instIDs := map[string]bool{}
	for instID := range cfg.NamingApplied {
		instIDs[instID] = true
	}
	for instID := range cfg.NamingAutoSync {
		instIDs[instID] = true
	}

	// UPDATE check resolves the intended scheme's pattern from the UPSTREAM
	// side-ref (no working-tree pull), so Notify/Delayed see guide updates at
	// check time the way profile-sync does. Fetch the side-ref once; read naming
	// per app type lazily below. On fetch/read failure we fall back to the loaded
	// guide per field, so a transient failure never produces false updates.
	branch := cfg.TrashRepo.Branch
	if branch == "" {
		branch = "master"
	}
	upstreamRef := ""
	if checkUpdate && cfg.TrashRepo.URL != "" {
		fctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := d.app.Trash.FetchUpstreamRefspec(fctx, cfg.TrashRepo.URL, branch); err == nil {
			upstreamRef = upstreamWatchRefPrefix + branch
		}
		cancel()
	}
	upstreamNaming := map[string]*TrashNaming{} // appType -> side-ref naming (lazy, cached)

	for instID := range instIDs {
		// Merge per-field {scheme, fingerprint}: applied record wins; else binding.
		type fieldState struct{ scheme, fingerprint string }
		merged := map[string]fieldState{}
		for field, b := range cfg.NamingAutoSync[instID] {
			if b.LastFingerprint != "" {
				merged[field] = fieldState{scheme: b.Scheme, fingerprint: b.LastFingerprint}
			}
		}
		for field, rec := range cfg.NamingApplied[instID] {
			if rec.Fingerprint != "" {
				merged[field] = fieldState{scheme: rec.Scheme, fingerprint: rec.Fingerprint}
			}
		}
		if len(merged) == 0 {
			continue
		}
		inst, ok := d.app.Config.GetInstance(instID)
		if !ok {
			continue
		}
		client := NewArrClientFor(inst, d.app.HTTPClient)
		current, err := client.GetNaming()
		if err != nil {
			// Unreachable: leave existing drift/update state alone (no false
			// reconcile), skip.
			continue
		}
		res.checkedOK[instID] = true
		ad := d.app.Trash.GetAppData(inst.Type)
		un, cached := upstreamNaming[inst.Type]
		if !cached {
			if upstreamRef != "" {
				un = d.app.Trash.ReadNamingFromRef(context.Background(), upstreamRef, inst.Type)
			}
			upstreamNaming[inst.Type] = un // cache (may be nil → loaded-guide fallback)
		}

		drift := map[string]string{}
		updates := map[string]string{}
		curPattern := map[string]string{}      // field -> current Arr pattern (drifted)
		expectedPattern := map[string]string{} // field -> scheme's current guide pattern
		for field, rec := range merged {
			arrKey, ok := NamingArrKey[field]
			if !ok {
				continue
			}
			// Resolve the intended scheme's current guide pattern once (used for the
			// update check + the drift notification's "what Sync would apply").
			expected := ""
			if rec.scheme != "" {
				// Prefer the upstream side-ref pattern (sees guide updates without a
				// pull); fall back to the loaded guide if the side-ref is unavailable.
				if un != nil {
					if gp, ok := ResolveNamingFieldFrom(un, inst.Type, field, rec.scheme); ok {
						expected = gp
					}
				} else if gp, ok := ResolveNamingField(ad, inst.Type, field, rec.scheme); ok {
					expected = gp
				}
			}
			expectedPattern[field] = expected
			// Capture the live Arr value regardless of which checks run — the
			// update notification's "Now" line needs it even in update-only mode.
			cur, _ := current[arrKey].(string)
			if cur != "" {
				curPattern[field] = cur
			}
			// DRIFT — current Arr pattern differs from what clonarr applied.
			if checkDrift && cur != "" {
				if fp := NamingFingerprint(cur); fp != rec.fingerprint {
					drift[field] = fp
				}
			}
			// UPDATE — the intended scheme's current guide pattern differs from
			// what clonarr applied (a guide update is available for this field).
			if checkUpdate && expected != "" {
				if fp := NamingFingerprint(expected); fp != rec.fingerprint {
					updates[field] = fp
				}
			}
		}
		if len(drift) > 0 {
			res.driftFP[instID] = drift
		}
		if len(updates) > 0 {
			res.updateFP[instID] = updates
		}

		// Categorize each newly-changed field, deduped per half against the stored
		// fingerprints so a standing state isn't re-notified. A field that is BOTH
		// drifted AND update-available goes to one combined notification rather than
		// a drift + an update message that would show the identical Now → guide diff
		// twice (the drift "Should be" and the update "New" both resolve to the same
		// current guide pattern).
		mkField := func(field string) namingDriftField {
			return namingDriftField{Field: field, Current: curPattern[field], Expected: expectedPattern[field]}
		}
		var newlyDrift, newlyUpd, newlyBoth []namingDriftField
		for field, dfp := range drift {
			driftNew := inst.NamingDriftFingerprints[field] != dfp
			if ufp, isUpdate := updates[field]; isUpdate {
				if driftNew || inst.NamingUpdateFingerprints[field] != ufp {
					newlyBoth = append(newlyBoth, mkField(field))
				}
				continue
			}
			if driftNew {
				newlyDrift = append(newlyDrift, mkField(field))
			}
		}
		for field, ufp := range updates {
			if _, isDrift := drift[field]; isDrift {
				continue // handled above as "both"
			}
			if inst.NamingUpdateFingerprints[field] != ufp {
				newlyUpd = append(newlyUpd, mkField(field))
			}
		}
		ev := func(fields []namingDriftField) namingDriftEvent {
			return namingDriftEvent{InstanceID: instID, InstanceName: inst.Name, AppType: inst.Type, Fields: fields}
		}
		if len(newlyDrift) > 0 {
			res.detected = append(res.detected, ev(newlyDrift))
		}
		if len(newlyUpd) > 0 {
			res.detectedUpdates = append(res.detectedUpdates, ev(newlyUpd))
		}
		if len(newlyBoth) > 0 {
			res.detectedBoth = append(res.detectedBoth, ev(newlyBoth))
		}
	}
	return res
}
