package api

import (
	"net/http"
	"sort"
	"time"

	"clonarr/internal/core"
)

// --- Profile Sync — config + telemetry endpoints ---

// handleGetProfileSync returns the current ProfileSync state (settings +
// runner telemetry). When the subsystem has never been initialised by
// migration (ProfileSync == nil), returns a sensible default shape so
// frontend can render consistently.
func (s *Server) handleGetProfileSync(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	if cfg.ProfileSync == nil {
		// Return all fields with zero values so frontend doesn't need
		// optional-chaining on every read. Matches the shape the
		// migration populates for existing configs.
		writeJSON(w, core.ProfileSync{})
		return
	}
	writeJSON(w, cfg.ProfileSync)
}

// handlePutProfileSync updates user-controlled settings: Sources, Mode,
// Interval/Specific (Schedule), ApplyInterval/ApplySpecific (Apply schedule).
// Runner-managed fields (LastRun, UpstreamHead, LocalHead, LastResult) are
// read-only via API to prevent drifting display state.
func (s *Server) handlePutProfileSync(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[struct {
		Interval          *string                  `json:"interval,omitempty"`
		Specific          *core.PullSchedule       `json:"specific,omitempty"`
		Sources           *core.ProfileSyncSources `json:"sources,omitempty"`
		Mode              *string                  `json:"mode,omitempty"`
		ApplyDelayMinutes *int                     `json:"applyDelayMinutes,omitempty"`
	}](w, r, 8192)
	if !ok {
		return
	}
	// Boundary validation — reject invalid values now rather than letting
	// the runner silently no-op on garbage Mode strings or trip on bad
	// schedule durations.
	if req.Mode != nil && *req.Mode != "" && !core.IsValidProfileSyncMode(*req.Mode) {
		writeError(w, 400, "invalid mode (must be empty, 'auto', 'notify', or 'delayed')")
		return
	}
	// Clamp apply delay to a sane range: minimum 1 minute (0 would mean
	// "apply immediately", which is just Auto mode), max 90 days. Negative
	// is nonsense.
	if req.ApplyDelayMinutes != nil {
		if *req.ApplyDelayMinutes < 0 {
			writeError(w, 400, "applyDelayMinutes must be >= 0")
			return
		}
		if *req.ApplyDelayMinutes > 129600 {
			writeError(w, 400, "applyDelayMinutes too large (max 90 days)")
			return
		}
	}
	if err := s.Core.Config.Update(func(cfg *core.Config) {
		if cfg.ProfileSync == nil {
			cfg.ProfileSync = &core.ProfileSync{}
		}
		if req.Interval != nil {
			cfg.ProfileSync.Interval = *req.Interval
		}
		if req.Specific != nil {
			sp := *req.Specific
			cfg.ProfileSync.Specific = &sp
		}
		if req.Sources != nil {
			cfg.ProfileSync.Sources = *req.Sources
		}
		if req.Mode != nil {
			cfg.ProfileSync.Mode = *req.Mode
		}
		if req.ApplyDelayMinutes != nil {
			cfg.ProfileSync.ApplyDelayMinutes = *req.ApplyDelayMinutes
		}
	}); err != nil {
		writeError(w, 500, "failed to save profile-sync settings")
		return
	}
	// Wake the profile-sync-watcher so cadence / sources / mode changes take
	// effect immediately rather than waiting for the 60s poll-tick fallback.
	select {
	case s.Core.PullUpdateCh <- "":
	default:
	}
	// Wake the delayed-apply-scheduler too — Mode + ApplyDelayMinutes changes
	// need to re-evaluate (or stop) the per-rule debounce immediately.
	select {
	case s.Core.ApplyUpdateCh <- struct{}{}:
	default:
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleProfileSyncCheck runs one detection-only tick synchronously. Bypasses
// the Mode dispatch (Auto/Notify/Delayed) so the user can trigger a check
// regardless of how scheduler is configured — useful for testing and for
// users on Auto mode who want a status snapshot without waiting for the
// next pull-and-sync cycle. Respects the Sources gate: if TrashUpstream is
// disabled, returns ok=false with a hint instead of pretending to work.
func (s *Server) handleProfileSyncCheck(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	if cfg.ProfileSync == nil || !cfg.ProfileSync.Sources.TrashUpstream {
		writeError(w, 400, "TRaSH-upstream source is disabled — enable it in Settings → Profile Sync → Sources")
		return
	}
	if err := s.Core.ProfileSyncRunner.RunDetectionOnly(r.Context()); err != nil {
		writeError(w, 500, "check failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDriftCheck runs one drift-detection pass synchronously over every
// eligible auto-sync rule. Bypasses the Sources gate so the user can
// trigger a manual check even when ArrDrift is disabled — useful while
// the scheduled drift path is still being designed and the only way to
// see drift results today is via this endpoint.
//
// Notification dispatch is identical to the scheduled drift run: both
// manual and scheduled entry points reach the same NotifyDriftDetected /
// NotifyDriftReconciled calls inside DriftRunner. Per-agent event flags
// (OnDriftDetected, OnDriftReconciled) decide whether anything actually
// sends.
//
// Returns the per-rule DriftResult list inline so the caller (frontend
// "Check drift now" button OR curl during development) gets immediate
// feedback. The aggregate also persists to DriftWatch.LastResult.
func (s *Server) handleDriftCheck(w http.ResponseWriter, r *http.Request) {
	if s.Core.DriftRunner == nil {
		writeError(w, 500, "drift runner not initialised")
		return
	}
	results, err := s.Core.DriftRunner.RunOnce(r.Context())
	if err != nil {
		writeError(w, 500, "drift check failed: "+err.Error())
		return
	}

	// Read per-instance CF drift fingerprints persisted by the pass we
	// just ran. The frontend renders a per-CF status pill (one entry
	// per drifted CF per instance) and the Check completion toast uses
	// the summary counts to compose a single one-line message instead
	// of N badges. Empty array (not null) so the frontend can detect
	// "no drift" cleanly without nil-guarding.
	cfg := s.Core.Config.Get()
	type cfDriftEntry struct {
		InstanceID   string `json:"instanceId"`
		InstanceName string `json:"instanceName"`
		AppType      string `json:"appType"`
		TrashID      string `json:"trashId"`
		Name         string `json:"name"`
	}
	cfDrift := []cfDriftEntry{}
	for _, inst := range cfg.Instances {
		if len(inst.CFDriftFingerprints) == 0 {
			continue
		}
		appData := s.Core.Trash.GetAppData(inst.Type)
		customs := s.Core.CustomCFs.List(inst.Type)
		customsByID := make(map[string]core.CustomCF, len(customs))
		for _, c := range customs {
			customsByID[c.ID] = c
		}
		// Stable order so the JSON output round-trips identically on
		// repeated GETs that didn't actually change state.
		tids := make([]string, 0, len(inst.CFDriftFingerprints))
		for tid := range inst.CFDriftFingerprints {
			tids = append(tids, tid)
		}
		sort.Strings(tids)
		for _, tid := range tids {
			name := tid
			if appData != nil {
				if cf, ok := appData.CustomFormats[tid]; ok && cf != nil {
					name = cf.Name
				}
			}
			if c, ok := customsByID[tid]; ok {
				name = c.Name
			}
			cfDrift = append(cfDrift, cfDriftEntry{
				InstanceID:   inst.ID,
				InstanceName: inst.Name,
				AppType:      inst.Type,
				TrashID:      tid,
				Name:         name,
			})
		}
	}

	// Aggregate counters for the Check completion toast. The frontend
	// composes the toast from the three channels — profile drift, CF
	// drift, TRaSH-Guides updates — and omits empty channels so the
	// message reads "Check complete. Everything is in sync." when all
	// three are clean.
	profilesDrifted := 0
	for _, dr := range results {
		if dr.DriftDetected {
			profilesDrifted++
		}
	}

	writeJSON(w, map[string]any{
		"checkedAt": time.Now().UTC().Format(time.RFC3339),
		"results":   results,
		"cfDrift":   cfDrift,
		"summary": map[string]int{
			"profilesDrifted": profilesDrifted,
			"cfsDrifted":      len(cfDrift),
		},
	})
}
