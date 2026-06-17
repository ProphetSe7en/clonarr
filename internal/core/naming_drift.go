package core

import "clonarr/internal/arr"

// namingDriftEvent: a newly-detected naming drift on one instance, for notification.
type namingDriftEvent struct {
	InstanceID   string
	InstanceName string
	AppType      string
	Fields       []string // canonical field keys newly found drifted
}

// namingDriftPassResult carries what to persist + what to notify after the pass.
type namingDriftPassResult struct {
	driftFP   map[string]map[string]string // instID -> field -> current Arr pattern fp (drifted)
	updateFP  map[string]map[string]string // instID -> field -> guide pattern fp (update available)
	checkedOK map[string]bool              // instances fetched + compared this pass
	detected  []namingDriftEvent           // newly-drifted, for notification
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
	for instID, applied := range cfg.NamingApplied {
		if len(applied) == 0 {
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

		drift := map[string]string{}
		updates := map[string]string{}
		for field, rec := range applied {
			if rec.Fingerprint == "" {
				continue
			}
			arrKey, ok := NamingArrKey[field]
			if !ok {
				continue
			}
			// DRIFT — current Arr pattern differs from what clonarr applied.
			if cur, _ := current[arrKey].(string); cur != "" {
				if fp := NamingFingerprint(cur); fp != rec.Fingerprint {
					drift[field] = fp
				}
			}
			// UPDATE — the intended scheme's current guide pattern differs from
			// what clonarr applied (a guide update is available for this field).
			if rec.Scheme != "" {
				if gp, ok := ResolveNamingField(ad, inst.Type, field, rec.Scheme); ok && gp != "" {
					if fp := NamingFingerprint(gp); fp != rec.Fingerprint {
						updates[field] = fp
					}
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
		var newly []string
		for field, fp := range drift {
			if inst.NamingDriftFingerprints[field] != fp {
				newly = append(newly, field)
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
	}
	return res
}
