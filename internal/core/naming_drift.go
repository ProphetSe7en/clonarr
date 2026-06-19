package core

import (
	"clonarr/internal/arr"
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
	detected        []namingDriftEvent           // newly-drifted, for notification
	detectedUpdates []namingDriftEvent           // newly update-available, for notification
}

// runNamingDriftPass checks, for every field clonarr has synced (Config.NamingApplied
// — manual OR auto), two things against what clonarr last applied:
//   - DRIFT: the instance's CURRENT Arr pattern differs from the applied one
//     (someone changed naming in Arr).
//   - UPDATE: the field's intended scheme has a NEWER pattern in the TRaSH guide
//     than what was applied (a guide update is available).
//
// It only FLAGS — never rewrites Arr (re-applying is the user's Sync / auto-sync).
// Per instance, mirroring the CF-spec drift pass. Runs inside DriftRunner.RunOnce,
// so it fires on both the scheduled check and the manual sidebar Check.
func runNamingDriftPass(d *DriftRunner) namingDriftPassResult {
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
	if cfg.TrashRepo.URL != "" {
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
		client := arr.NewArrClient(inst.URL, inst.APIKey, d.app.HTTPClient)
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
			// DRIFT — current Arr pattern differs from what clonarr applied.
			if cur, _ := current[arrKey].(string); cur != "" {
				curPattern[field] = cur
				if fp := NamingFingerprint(cur); fp != rec.fingerprint {
					drift[field] = fp
				}
			}
			// UPDATE — the intended scheme's current guide pattern differs from
			// what clonarr applied (a guide update is available for this field).
			if expected != "" {
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

		// Newly-drifted vs the previously-stored fingerprints → notify (dedup so a
		// standing drift isn't re-notified on every check).
		var newly []namingDriftField
		for field, fp := range drift {
			if inst.NamingDriftFingerprints[field] != fp {
				newly = append(newly, namingDriftField{Field: field, Current: curPattern[field], Expected: expectedPattern[field]})
			}
		}
		if len(newly) > 0 {
			res.detected = append(res.detected, namingDriftEvent{
				InstanceID:   instID,
				InstanceName: inst.Name,
				AppType:      inst.Type,
				Fields:       newly,
			})
		}

		// Newly update-available vs the previously-stored update fingerprints →
		// notify (same dedup so a standing update isn't re-notified every check).
		// Current = the applied/Arr pattern (old), Expected = the new guide pattern.
		var newlyUpd []namingDriftField
		for field, fp := range updates {
			if inst.NamingUpdateFingerprints[field] != fp {
				newlyUpd = append(newlyUpd, namingDriftField{Field: field, Current: curPattern[field], Expected: expectedPattern[field]})
			}
		}
		if len(newlyUpd) > 0 {
			res.detectedUpdates = append(res.detectedUpdates, namingDriftEvent{
				InstanceID:   instID,
				InstanceName: inst.Name,
				AppType:      inst.Type,
				Fields:       newlyUpd,
			})
		}
	}
	return res
}
