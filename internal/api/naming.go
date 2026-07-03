package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// upstreamNaming fetches an app type's CURRENT TRaSH naming from the upstream
// side-ref (git fetch + git show, NO working-tree pull). Apply paths use it so
// "Update all" / "Sync now" / delayed-apply write the current scheme pattern even
// in Notify/Delayed mode where the clone hasn't been pulled — otherwise they'd
// re-apply the stale loaded pattern (a no-op) and the update badge would never
// clear. Returns nil when the upstream is unreachable (callers fall back to the
// loaded guide).
func (s *Server) upstreamNaming(appType string) *core.TrashNaming {
	cfg := s.Core.Config.Get()
	if cfg.TrashRepo.URL == "" {
		return nil
	}
	branch := cfg.TrashRepo.Branch
	if branch == "" {
		branch = "master"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Core.Trash.FetchUpstreamRefspec(ctx, cfg.TrashRepo.URL, branch); err != nil {
		return nil
	}
	return s.Core.Trash.ReadNamingFromRef(ctx, core.UpstreamWatchRef(branch), appType)
}

// Naming auto-sync (#5) shared core: resolving a TRaSH scheme to a single naming
// field, applying fields to an instance (per-field; others preserved), recording
// the applied scheme/fingerprint + a per-field change-history event, and
// fingerprinting. Used by the manual apply handler, the scheduled AutoSyncNaming
// loop, Sync now / Update all, and restore — one apply path. Field keys are
// clonarr's canonical naming-field identifiers.

const namingChangeKeep = 10 // per-field change events kept (newest), for history + restore

// namingArrKey maps a canonical field key → the Arr /config/naming key.
// Single source of truth lives in core (the drift runner needs it too, and core
// cannot import api); aliased here so existing call sites keep the short name.
var namingArrKey = core.NamingArrKey

// namingFieldsForType lists the auto-syncable canonical field keys per app type.
func namingFieldsForType(instType string) []string {
	if instType == "radarr" {
		return []string{"movieFile", "movieFolder"}
	}
	return []string{"standardEpisode", "dailyEpisode", "animeEpisode", "seriesFolder", "seasonFolder"}
}

// resolveNamingField is an alias to the single source in core (the drift/update
// pass needs it too, and core cannot import api).
func resolveNamingField(ad *core.AppData, instType, field, scheme string) (string, bool) {
	return core.ResolveNamingField(ad, instType, field, scheme)
}

// namingFingerprint hashes an applied pattern (single source in core).
func namingFingerprint(pattern string) string { return core.NamingFingerprint(pattern) }

// applyFieldsToArrNaming sets the provided canonical fields onto a fetched Arr
// naming config in place — only those fields change; everything else the instance
// already had is preserved. Also enables renaming + illegal-char replacement for
// the app type, matching the manual apply's behaviour.
func applyFieldsToArrNaming(instType string, current arr.ArrNamingConfig, fields map[string]string) {
	if current == nil {
		return
	}
	if instType == "radarr" {
		current["renameMovies"] = true
	} else {
		current["renameEpisodes"] = true
	}
	current["replaceIllegalCharacters"] = true
	for field, pattern := range fields {
		if pattern == "" {
			continue
		}
		if key, ok := namingArrKey[field]; ok {
			current[key] = pattern
		}
	}
}

// extractNamingPatterns reads the current Arr naming config back into canonical
// field → pattern (the pre-apply state). Used as the `from` side of each history
// event and returned to callers as `prev`. Only the fields clonarr manages.
func extractNamingPatterns(instType string, current arr.ArrNamingConfig) map[string]string {
	out := map[string]string{}
	if current == nil {
		return out
	}
	fields := namingFieldsForType(instType)
	if instType != "radarr" {
		// sonarr-only; manual-set (not auto-syncable), captured for rollback completeness
		fields = append(fields, "specialsFolder")
	}
	for _, field := range fields {
		if key, ok := namingArrKey[field]; ok {
			if v, ok := current[key].(string); ok && v != "" {
				out[field] = v
			}
		}
	}
	return out
}

// applyNamingFields is the single apply path: fetch current naming, set the given
// fields, write back, then record per-field state. fields maps canonical field key
// → pattern. Returns the pre-apply patterns (canonical field → pattern) so callers
// can show what changed (e.g. the auto-sync old → new notification); empty on a
// failed read. schemes maps a canonical field key → the intended scheme key for
// that apply: a field present records that scheme on NamingApplied; a field absent
// (e.g. rollback restoring an old pattern) PRESERVES the existing scheme and only
// updates the fingerprint. Every actual change also appends a per-field history
// event {from, to, at, via} for the inline history + restore.
func (s *Server) applyNamingFields(inst core.Instance, fields map[string]string, schemes map[string]string, replacedBy string) (arr.ArrNamingConfig, map[string]string, error) {
	client := core.NewArrClientFor(inst, s.Core.HTTPClient)
	current, err := client.GetNaming()
	if err != nil {
		return nil, nil, err
	}
	prev := extractNamingPatterns(inst.Type, current)
	applyFieldsToArrNaming(inst.Type, current, fields)
	result, err := client.UpdateNaming(current)
	if err != nil {
		return nil, prev, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.Core.Config.Update(func(cfg *core.Config) {
		if cfg.NamingApplied == nil {
			cfg.NamingApplied = map[string]map[string]core.NamingAppliedRecord{}
		}
		if cfg.NamingApplied[inst.ID] == nil {
			cfg.NamingApplied[inst.ID] = map[string]core.NamingAppliedRecord{}
		}
		if cfg.NamingChanges == nil {
			cfg.NamingChanges = map[string]map[string][]core.NamingChangeEvent{}
		}
		if cfg.NamingChanges[inst.ID] == nil {
			cfg.NamingChanges[inst.ID] = map[string][]core.NamingChangeEvent{}
		}
		for field, pattern := range fields {
			if pattern == "" {
				continue // unset field wasn't applied — don't record it
			}
			// Applied record (drift + update detection).
			rec := cfg.NamingApplied[inst.ID][field]
			if sc, ok := schemes[field]; ok {
				rec.Scheme = sc // explicit (manual/auto/enable); absent => preserve (rollback)
			}
			rec.Fingerprint = namingFingerprint(pattern)
			rec.AppliedAt = now
			cfg.NamingApplied[inst.ID][field] = rec
			// History event — only when the pattern actually changed.
			if prev[field] != pattern {
				ev := core.NamingChangeEvent{At: now, From: prev[field], To: pattern, Via: replacedBy}
				list := append(cfg.NamingChanges[inst.ID][field], ev)
				if len(list) > namingChangeKeep {
					list = list[len(list)-namingChangeKeep:]
				}
				cfg.NamingChanges[inst.ID][field] = list
			}
		}
	})
	return result, prev, nil
}

// --- #5 phase 2: per-field auto-sync bindings, rollback history, scheduled loop ---

// handleGetNamingAutoSync returns the instance's field → binding map (empty when
// nothing is opted in — the default for everyone).
func (s *Server) handleGetNamingAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	out := map[string]core.NamingFieldBinding{}
	if cfg.NamingAutoSync != nil {
		if v, ok := cfg.NamingAutoSync[id]; ok {
			for field, b := range v {
				out[field] = b
			}
		}
	}
	writeJSON(w, out)
}

// handleNamingSync applies fields' intended schemes (the scheme's current guide
// pattern) to the instance now, and clears their drift/update flags.
//   - body {"fields":[...]} → apply exactly those fields (per-field "Sync now";
//     works for any synced field, manual or auto, via its applied/bound scheme).
//   - empty body → "Update all": every AUTO-SYNC-ON field that currently has a
//     drift or update flag (manual fields are excluded — they have no auto-scheme
//     to apply on a bulk action; sync them individually).
func (s *Server) handleNamingSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Fields []string `json:"fields"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req) // empty body is valid (Update all)

	cfg := s.Core.Config.Get()
	bindings := cfg.NamingAutoSync[id]
	applied := cfg.NamingApplied[id]
	ad := s.Core.Trash.GetAppData(inst.Type)
	// Resolve from the upstream side-ref (current scheme pattern, no pull) so an
	// available guide UPDATE is actually applied — not the stale loaded pattern.
	// Falls back to the loaded guide per field if the upstream is unreachable.
	un := s.upstreamNaming(inst.Type)

	targets := req.Fields
	if len(targets) == 0 {
		// Update all: auto-sync-ON fields flagged with drift or update.
		for field := range bindings {
			if inst.NamingDriftFingerprints[field] != "" || inst.NamingUpdateFingerprints[field] != "" {
				targets = append(targets, field)
			}
		}
	}

	fields := map[string]string{}
	schemes := map[string]string{}
	for _, field := range targets {
		scheme := bindings[field].Scheme
		if scheme == "" {
			scheme = applied[field].Scheme // manually-synced field
		}
		if scheme == "" {
			continue
		}
		pattern := ""
		if un != nil {
			if p, ok := core.ResolveNamingFieldFrom(un, inst.Type, field, scheme); ok {
				pattern = p
			}
		}
		if pattern == "" {
			if p, ok := resolveNamingField(ad, inst.Type, field, scheme); ok {
				pattern = p
			}
		}
		if pattern == "" {
			continue
		}
		fields[field] = pattern
		schemes[field] = scheme
	}
	if len(fields) == 0 {
		writeJSON(w, map[string]any{"status": "noop", "applied": 0})
		return
	}

	if _, _, err := s.applyNamingFields(inst, fields, schemes, "manual"); err != nil {
		writeError(w, 502, "Failed to apply naming: "+err.Error())
		return
	}
	// Re-run the naming check so drift/update flags reflect the new state (drift
	// clears; an update may remain if a field was synced to an older pattern).
	if s.Core.DriftRunner != nil {
		_ = s.Core.DriftRunner.RunNamingDriftOnce()
	}
	writeJSON(w, map[string]any{"status": "synced", "applied": len(fields)})
}

// handleNamingDriftCheck runs ONLY the naming drift+update pass (not CF/profile
// drift) — the naming-section "Check". Persists the per-instance flags; the UI
// reloads instances afterwards to read them.
func (s *Server) handleNamingDriftCheck(w http.ResponseWriter, r *http.Request) {
	if s.Core.DriftRunner == nil {
		writeError(w, 500, "drift runner not initialised")
		return
	}
	if err := s.Core.DriftRunner.RunNamingDriftOnce(); err != nil {
		writeError(w, 500, "naming check failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "checked"})
}

// handleGetNamingApplied returns what clonarr last applied per field (scheme +
// fingerprint + appliedAt), for any field it has synced (manual OR auto). The UI
// uses the scheme to label/Sync manually-synced fields and to compute the Expected
// pattern for the drift diff.
func (s *Server) handleGetNamingApplied(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	out := map[string]core.NamingAppliedRecord{}
	if cfg.NamingApplied != nil {
		if v, ok := cfg.NamingApplied[id]; ok {
			for field, rec := range v {
				out[field] = rec
			}
		}
	}
	writeJSON(w, out)
}

// handleSaveNamingAutoSync replaces the instance's per-field bindings. The client
// sends field → {scheme}; the server owns LastFingerprint/LastAppliedAt/LastError.
// An empty body clears all bindings for the instance (opt-out). Validates that
// every field is auto-syncable for the instance type, and preserves the apply
// state for fields whose scheme is unchanged (a scheme change resets it so the
// next tick re-applies). The map key being the field enforces one scheme per field.
func (s *Server) handleSaveNamingAutoSync(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var incoming map[string]core.NamingFieldBinding
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	allowed := map[string]bool{}
	for _, f := range namingFieldsForType(inst.Type) {
		allowed[f] = true
	}
	for field, b := range incoming {
		if !allowed[field] {
			writeError(w, 400, "Field not auto-syncable for this instance type: "+field)
			return
		}
		if b.Scheme == "" {
			writeError(w, 400, "Scheme is required for field: "+field)
			return
		}
	}

	// A field is "newly enabled or re-pointed" when it wasn't bound before or its
	// scheme changed — those get applied immediately below (enabling auto-sync
	// brings the field to the scheme now, then follows future updates). Fields
	// with an unchanged scheme keep their apply state and are left alone.
	existing := map[string]core.NamingFieldBinding{}
	if cur := s.Core.Config.Get().NamingAutoSync; cur != nil {
		existing = cur[id]
	}
	toApply := []string{}
	for field, b := range incoming {
		if old, ok := existing[field]; !ok || old.Scheme != b.Scheme {
			toApply = append(toApply, field)
		}
	}

	err := s.Core.Config.Update(func(cfg *core.Config) {
		if cfg.NamingAutoSync == nil {
			cfg.NamingAutoSync = make(map[string]map[string]core.NamingFieldBinding)
		}
		if len(incoming) == 0 {
			delete(cfg.NamingAutoSync, id)
			return
		}
		ex := cfg.NamingAutoSync[id]
		next := make(map[string]core.NamingFieldBinding, len(incoming))
		for field, b := range incoming {
			nb := core.NamingFieldBinding{Scheme: b.Scheme}
			if old, ok := ex[field]; ok && old.Scheme == b.Scheme {
				// Same scheme — keep the apply state so we don't re-apply needlessly.
				nb.LastFingerprint = old.LastFingerprint
				nb.LastAppliedAt = old.LastAppliedAt
			}
			next[field] = nb
		}
		cfg.NamingAutoSync[id] = next
	})
	if err != nil {
		writeError(w, 500, "Failed to save naming auto-sync settings")
		return
	}

	// Apply newly-enabled/re-pointed fields now. Enabling auto-sync that didn't
	// take effect until the next pull wouldn't be auto-sync — the field must
	// reach the chosen scheme immediately, then follow future guide changes.
	applied := 0
	var applyErrMsg string
	if len(toApply) > 0 && s.Core.Trash != nil {
		ad := s.Core.Trash.GetAppData(inst.Type)
		// Resolve the scheme's CURRENT pattern from the upstream side-ref (sees
		// guide updates without a working-tree pull), falling back to the loaded
		// guide when upstream is unreachable. Same as handleNamingSync, so enabling
		// auto-sync against a stale clone applies the current pattern, not the old.
		un := s.upstreamNaming(inst.Type)
		fields := map[string]string{}
		fp := map[string]string{}
		for _, field := range toApply {
			pattern := ""
			if un != nil {
				if p, ok := core.ResolveNamingFieldFrom(un, inst.Type, field, incoming[field].Scheme); ok {
					pattern = p
				}
			}
			if pattern == "" {
				if p, ok := resolveNamingField(ad, inst.Type, field, incoming[field].Scheme); ok {
					pattern = p
				}
			}
			if pattern == "" {
				continue // guide has no pattern for this (field, scheme) — never guess
			}
			fields[field] = pattern
			fp[field] = namingFingerprint(pattern)
		}
		if len(fields) > 0 {
			enSchemes := map[string]string{}
			for field := range fields {
				enSchemes[field] = incoming[field].Scheme
			}
			_, _, applyErr := s.applyNamingFields(inst, fields, enSchemes, "auto-sync")
			now := time.Now().UTC().Format(time.RFC3339)
			s.Core.Config.Update(func(c *core.Config) {
				m := c.NamingAutoSync[id]
				if m == nil {
					return
				}
				for field := range fields {
					nb, ok := m[field]
					if !ok {
						continue
					}
					if applyErr != nil {
						nb.LastError = applyErr.Error()
					} else {
						nb.LastFingerprint = fp[field]
						nb.LastAppliedAt = now
						nb.LastError = ""
					}
					m[field] = nb
				}
			})
			if applyErr != nil {
				applyErrMsg = applyErr.Error()
				s.Core.DebugLog.Logf(core.LogError, "Naming [%s]: apply-on-enable failed: %v", inst.Name, applyErr)
			} else {
				applied = len(fields)
			}
		}
	}
	writeJSON(w, map[string]any{"status": "saved", "applied": applied, "error": applyErrMsg})
}

// handleGetNamingChanges returns the per-field change history for an instance:
// field → []event (newest last). Empty when nothing has been synced.
func (s *Server) handleGetNamingChanges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	out := map[string][]core.NamingChangeEvent{}
	if cfg.NamingChanges != nil {
		if v, ok := cfg.NamingChanges[id]; ok {
			for field, events := range v {
				out[field] = events
			}
		}
	}
	writeJSON(w, out)
}

// handleRestoreNamingField restores ONE field to a prior pattern (from its history).
// Body: {"field": "movieFile", "to": "<pattern>"}. Applies through the shared path
// (so the restore is itself recorded as a history event) preserving the field's
// intended scheme, then re-checks so drift/update flags reflect the restored value.
func (s *Server) handleRestoreNamingField(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var req struct {
		Field string `json:"field"`
		To    string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if req.Field == "" || req.To == "" {
		writeError(w, 400, "field and to are required")
		return
	}
	if _, ok := core.NamingArrKey[req.Field]; !ok {
		writeError(w, 400, "Unknown naming field: "+req.Field)
		return
	}

	// No scheme passed → preserve the field's existing intended scheme; only the
	// pattern (and fingerprint) revert.
	if _, _, err := s.applyNamingFields(inst, map[string]string{req.Field: req.To}, nil, "rollback"); err != nil {
		log.Printf("Naming restore [%s/%s]: failed: %v", inst.Name, req.Field, err)
		s.Core.DebugLog.Logf(core.LogError, "Naming restore [%s/%s]: failed: %v", inst.Name, req.Field, err)
		writeError(w, 502, "Failed to restore naming on the instance")
		return
	}
	if s.Core.DriftRunner != nil {
		_ = s.Core.DriftRunner.RunNamingDriftOnce()
	}
	s.Core.DebugLog.Logf(core.LogAutoSync, "Naming restore [%s/%s]: restored", inst.Name, req.Field)
	writeJSON(w, map[string]string{"status": "restored"})
}

// AutoSyncNaming runs after a TRaSH pull (scheduled + manual; NOT at startup). For each instance with
// opted-in naming fields, it re-applies any bound field that no longer matches its
// scheme — either because the guide changed (update) or because the field was edited
// directly in Arr (drift). Guide updates always apply on this TRaSH-source path;
// drift correction is gated on the "Changes made directly in Radarr/Sonarr" source,
// mirroring profile-sync. Fields already matching their scheme are skipped (the
// common case, so most ticks are no-ops). Default-off: instances absent from
// NamingAutoSync do nothing.
func (s *Server) AutoSyncNaming() {
	cfg := s.Core.Config.Get()
	if len(cfg.NamingAutoSync) == 0 {
		return
	}
	arrDriftOn := cfg.ProfileSync != nil && cfg.ProfileSync.Sources.ArrDrift

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

		// Read the live Arr naming once so a bound field can be re-applied for
		// EITHER reason: the guide changed (update) OR it was edited directly in
		// Arr so it no longer matches the scheme (drift). Drift correction is
		// gated on arrDriftOn; guide updates always apply on this path.
		client := core.NewArrClientFor(inst, s.Core.HTTPClient)
		liveCfg, err := client.GetNaming()
		if err != nil {
			log.Printf("Auto-sync naming [%s]: read failed: %v", inst.Name, err)
			s.Core.DebugLog.Logf(core.LogError, "Naming [%s]: read failed: %v", inst.Name, err)
			continue
		}
		live := extractNamingPatterns(inst.Type, liveCfg)

		fieldsToApply := map[string]string{}
		newFingerprint := map[string]string{}
		reasons := map[string]string{} // field -> "update" | "drift" (for the notification wording)
		for field, b := range bindings {
			pattern, ok := resolveNamingField(ad, inst.Type, field, b.Scheme)
			if !ok || pattern == "" {
				continue // guide has no pattern for this (field, scheme) — never guess
			}
			fp := namingFingerprint(pattern)
			guideUpdated := fp != b.LastFingerprint         // guide pattern changed since last apply
			drifted := arrDriftOn && live[field] != pattern // Arr no longer matches the scheme
			if !guideUpdated && !drifted {
				continue // already matches the scheme — nothing to do
			}
			fieldsToApply[field] = pattern
			newFingerprint[field] = fp
			if guideUpdated {
				reasons[field] = "update"
			} else {
				reasons[field] = "drift"
			}
		}
		if len(fieldsToApply) == 0 {
			continue
		}

		schemes := map[string]string{}
		for field := range fieldsToApply {
			schemes[field] = bindings[field].Scheme
		}
		_, prev, err := s.applyNamingFields(inst, fieldsToApply, schemes, "auto-sync")
		if err != nil {
			log.Printf("Auto-sync naming [%s]: apply failed: %v", inst.Name, err)
			s.Core.DebugLog.Logf(core.LogError, "Naming [%s]: apply failed: %v", inst.Name, err)
			s.Core.Config.Update(func(c *core.Config) {
				m := c.NamingAutoSync[instID]
				if m == nil {
					return
				}
				for field := range fieldsToApply {
					if nb, ok := m[field]; ok {
						nb.LastError = err.Error()
						m[field] = nb
					}
				}
			})
			continue
		}

		// Record success: bump fingerprints, stamp time, clear any prior error.
		now := time.Now().UTC().Format(time.RFC3339)
		s.Core.Config.Update(func(c *core.Config) {
			m := c.NamingAutoSync[instID]
			if m == nil {
				return
			}
			for field, fp := range newFingerprint {
				if nb, ok := m[field]; ok {
					nb.LastFingerprint = fp
					nb.LastAppliedAt = now
					nb.LastError = ""
					m[field] = nb
				}
			}
		})
		log.Printf("Auto-sync naming [%s]: applied %d field(s)", inst.Name, len(fieldsToApply))
		s.Core.DebugLog.Logf(core.LogAutoSync, "Naming [%s]: applied %d field(s)", inst.Name, len(fieldsToApply))

		// Optional notification (off by default): old → new pattern per field.
		var changes []core.NamingChange
		for field, newPattern := range fieldsToApply {
			changes = append(changes, core.NamingChange{
				FieldLabel: core.NamingFieldLabel(field),
				Scheme:     bindings[field].Scheme,
				OldPattern: prev[field],
				NewPattern: newPattern,
				Reason:     reasons[field],
			})
		}
		s.Core.NotifyNamingAutoSync(inst.Name, inst.Type, changes)
	}
}
