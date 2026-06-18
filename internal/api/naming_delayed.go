package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"log"
	"time"
)

// RunNamingDelayed is the naming side of "Wait before applying" (delayed mode).
// On each delayed-apply poll tick it checks every auto-sync-ON naming field:
//   - A field that needs syncing (guide update OR Arr drift) is first marked
//     pending (PendingSince) with a one-off "will apply in <delay>" notification.
//   - Once the delay has elapsed since PendingSince, the field is applied and the
//     normal naming notification fires (updated / drift corrected).
//   - A field back in sync clears its pending mark.
//
// No-op outside delayed mode. Drift detection is gated on the Arr-drift source the
// same way the auto-mode path is; guide updates always count. Mirrors the profile
// RunDelayedApply engine; runs from the same poll loop in main.go.
func (s *Server) RunNamingDelayed() {
	cfg := s.Core.Config.Get()
	if cfg.ProfileSync == nil ||
		cfg.ProfileSync.Mode != core.ProfileSyncModeDelayed ||
		cfg.ProfileSync.ApplyDelayMinutes <= 0 ||
		len(cfg.NamingAutoSync) == 0 {
		return
	}
	arrDriftOn := cfg.ProfileSync.Sources.ArrDrift
	delay := time.Duration(cfg.ProfileSync.ApplyDelayMinutes) * time.Minute
	now := time.Now().UTC()

	for instID, bindings := range cfg.NamingAutoSync {
		if len(bindings) == 0 {
			continue
		}
		inst, ok := s.Core.Config.GetInstance(instID)
		if !ok {
			continue
		}
		ad := s.Core.Trash.GetAppData(inst.Type)
		if ad == nil {
			continue
		}
		client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
		liveCfg, err := client.GetNaming()
		if err != nil {
			continue // unreachable — leave pending state alone, try next tick
		}
		live := extractNamingPatterns(inst.Type, liveCfg)

		setPending := map[string]string{} // field -> reason (newly pending)
		clearPending := []string{}        // fields back in sync
		dueFields := map[string]string{}  // field -> pattern (delay elapsed, apply now)
		dueSchemes := map[string]string{} // field -> scheme
		dueReason := map[string]string{}  // field -> "update" | "drift"

		for field, b := range bindings {
			pattern, ok := resolveNamingField(ad, inst.Type, field, b.Scheme)
			if !ok || pattern == "" {
				continue // guide has no pattern for this (field, scheme) — never guess
			}
			fp := namingFingerprint(pattern)
			guideUpdated := fp != b.LastFingerprint
			drifted := arrDriftOn && live[field] != pattern
			if !guideUpdated && !drifted {
				if b.PendingSince != "" {
					clearPending = append(clearPending, field)
				}
				continue
			}
			reason := "update"
			if !guideUpdated {
				reason = "drift"
			}
			if b.PendingSince == "" {
				setPending[field] = reason // first detection — start the timer
				continue
			}
			// Already pending — apply once the delay has elapsed.
			if ps, perr := time.Parse(time.RFC3339, b.PendingSince); perr == nil && !now.Before(ps.Add(delay)) {
				dueFields[field] = pattern
				dueSchemes[field] = b.Scheme
				dueReason[field] = reason
			}
		}

		// Persist pending start/clear.
		if len(setPending) > 0 || len(clearPending) > 0 {
			nowStr := now.Format(time.RFC3339)
			_ = s.Core.Config.Update(func(c *core.Config) {
				m := c.NamingAutoSync[instID]
				if m == nil {
					return
				}
				for field, reason := range setPending {
					if nb, ok := m[field]; ok {
						nb.PendingSince = nowStr
						nb.PendingReason = reason
						m[field] = nb
					}
				}
				for _, field := range clearPending {
					if nb, ok := m[field]; ok {
						nb.PendingSince = ""
						nb.PendingReason = ""
						m[field] = nb
					}
				}
			})
		}

		// One-off "will apply in <delay>" notification per newly-pending field.
		for field, reason := range setPending {
			s.Core.NotifyNamingPending(inst.Name, inst.Type, core.NamingFieldLabel(field), reason, cfg.ProfileSync.ApplyDelayMinutes)
		}

		// Apply fields whose delay has elapsed, then notify + clear pending.
		if len(dueFields) > 0 {
			_, prev, applyErr := s.applyNamingFields(inst, dueFields, dueSchemes, "auto-sync")
			if applyErr != nil {
				log.Printf("Naming delayed-apply [%s]: apply failed: %v", inst.Name, applyErr)
				s.Core.DebugLog.Logf(core.LogError, "Naming [%s]: delayed-apply failed: %v", inst.Name, applyErr)
				continue
			}
			nowStr := now.Format(time.RFC3339)
			_ = s.Core.Config.Update(func(c *core.Config) {
				m := c.NamingAutoSync[instID]
				if m == nil {
					return
				}
				for field, pattern := range dueFields {
					if nb, ok := m[field]; ok {
						nb.LastFingerprint = namingFingerprint(pattern)
						nb.LastAppliedAt = nowStr
						nb.PendingSince = ""
						nb.PendingReason = ""
						nb.LastError = ""
						m[field] = nb
					}
				}
			})
			var changes []core.NamingChange
			for field, pattern := range dueFields {
				changes = append(changes, core.NamingChange{
					FieldLabel: core.NamingFieldLabel(field),
					Scheme:     dueSchemes[field],
					OldPattern: prev[field],
					NewPattern: pattern,
					Reason:     dueReason[field],
				})
			}
			s.Core.NotifyNamingAutoSync(inst.Name, inst.Type, changes)
			s.Core.DebugLog.Logf(core.LogAutoSync, "Naming [%s]: delayed-apply applied %d field(s)", inst.Name, len(dueFields))
		}
	}
}
