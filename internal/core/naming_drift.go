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
	fingerprints map[string]map[string]string // instID -> field -> current-pattern fp (drifted fields only)
	checkedOK    map[string]bool              // instances fetched + compared this pass
	detected     []namingDriftEvent
}

// runNamingDriftPass compares each auto-synced naming field's CURRENT Arr pattern
// against the fingerprint clonarr last applied (binding.LastFingerprint). A
// mismatch means naming was changed in Arr after clonarr set it = drift. It only
// FLAGS — it never rewrites Arr (re-applying is the user's Sync action). Per
// instance, mirroring the CF-spec drift pass. Runs inside DriftRunner.RunOnce, so
// it fires on both the scheduled check and the manual sidebar Check.
func runNamingDriftPass(d *DriftRunner) namingDriftPassResult {
	cfg := d.app.Config.Get()
	res := namingDriftPassResult{
		fingerprints: map[string]map[string]string{},
		checkedOK:    map[string]bool{},
	}
	for instID, bindings := range cfg.NamingAutoSync {
		if len(bindings) == 0 {
			continue
		}
		inst, ok := d.app.Config.GetInstance(instID)
		if !ok {
			continue
		}
		client := arr.NewArrClient(inst.URL, inst.APIKey, d.app.HTTPClient)
		current, err := client.GetNaming()
		if err != nil {
			// Unreachable: leave existing drift state alone (no false reconcile), skip.
			continue
		}
		res.checkedOK[instID] = true

		driftFP := map[string]string{}
		for field, b := range bindings {
			if b.LastFingerprint == "" {
				continue // never applied — nothing to compare against
			}
			arrKey, ok := NamingArrKey[field]
			if !ok {
				continue
			}
			cur, _ := current[arrKey].(string)
			if cur == "" {
				continue // field unset on the instance — treat as not drifted
			}
			curFP := NamingFingerprint(cur)
			if curFP != b.LastFingerprint {
				driftFP[field] = curFP
			}
		}
		if len(driftFP) > 0 {
			res.fingerprints[instID] = driftFP
		}

		// Newly-drifted vs the previously-stored fingerprints → notify (dedup so a
		// standing drift isn't re-notified on every check).
		var newly []string
		for field, fp := range driftFP {
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
