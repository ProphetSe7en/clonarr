package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// Naming auto-sync (#5) shared core: resolving a TRaSH scheme to a single naming
// field, applying fields to an instance (per-field; others preserved), capturing
// a pre-apply snapshot for rollback, and fingerprinting. Used by both the manual
// apply handler and (later phases) the scheduled AutoSyncNaming loop, so there is
// one apply path. Field keys are clonarr's canonical naming-field identifiers.

const namingHistoryKeep = 5 // rollback snapshots kept per instance

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

// resolveNamingField returns the TRaSH pattern for one naming field under a given
// scheme/preset. ok=false when TRaSH has no pattern for that (field, scheme) — the
// caller then skips it (never applies an empty/guessed pattern). Mirrors the
// field→TrashNaming mapping in handleApplyNaming.
func resolveNamingField(ad *core.AppData, instType, field, scheme string) (string, bool) {
	if ad == nil || ad.Naming == nil {
		return "", false
	}
	switch field {
	case "movieFile":
		v, ok := ad.Naming.File[scheme]
		return v, ok
	case "movieFolder":
		v, ok := ad.Naming.Folder[scheme]
		return v, ok
	case "seriesFolder":
		v, ok := ad.Naming.Series[scheme]
		return v, ok
	case "seasonFolder":
		v, ok := ad.Naming.Season[scheme]
		return v, ok
	case "standardEpisode", "dailyEpisode", "animeEpisode":
		sub := map[string]string{"standardEpisode": "standard", "dailyEpisode": "daily", "animeEpisode": "anime"}[field]
		if ep := ad.Naming.Episodes[sub]; ep != nil {
			v, ok := ep[scheme]
			return v, ok
		}
	}
	return "", false
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
// field → pattern, for the pre-apply rollback snapshot. Only the fields clonarr
// manages are captured.
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

// appendNamingSnapshot pushes a pre-apply snapshot onto cfg.NamingHistory[instID],
// newest last, capped to keep. Pure logic on *Config (no I/O, no clock) so it's
// unit-testable; callers wrap it in Config.Update and pass the timestamp.
func appendNamingSnapshot(cfg *core.Config, instID string, patterns map[string]string, replacedBy, takenAt string, keep int) {
	if cfg.NamingHistory == nil {
		cfg.NamingHistory = map[string][]core.NamingSnapshot{}
	}
	snap := core.NamingSnapshot{TakenAt: takenAt, Naming: patterns, ReplacedBy: replacedBy}
	list := append(cfg.NamingHistory[instID], snap)
	if keep > 0 && len(list) > keep {
		list = list[len(list)-keep:]
	}
	cfg.NamingHistory[instID] = list
}

// applyNamingFields is the single apply path: fetch current naming, optionally
// snapshot it (for rollback), set the given fields, write back. Used by manual
// apply and the scheduled loop. fields maps canonical field key → pattern.
// Returns the pre-apply patterns (canonical field → pattern) so callers can show
// what changed (e.g. the auto-sync old → new notification); empty on a failed read.
// schemes maps a canonical field key → the intended scheme key for that apply.
// A field present in `schemes` records that scheme on NamingApplied; a field
// absent (e.g. rollback restoring a custom pattern) PRESERVES the existing scheme
// and only updates the fingerprint.
func (s *Server) applyNamingFields(inst core.Instance, fields map[string]string, schemes map[string]string, replacedBy string, snapshot bool) (arr.ArrNamingConfig, map[string]string, error) {
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	current, err := client.GetNaming()
	if err != nil {
		return nil, nil, err
	}
	// Capture the pre-apply state now (before we mutate current). The rollback
	// snapshot is only PERSISTED after the write succeeds — a failed UpdateNaming
	// must not leave an orphan snapshot (matters once the loop drives this at volume).
	prev := extractNamingPatterns(inst.Type, current)
	applyFieldsToArrNaming(inst.Type, current, fields)
	result, err := client.UpdateNaming(current)
	if err != nil {
		return nil, prev, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.Core.Config.Update(func(cfg *core.Config) {
		if snapshot {
			appendNamingSnapshot(cfg, inst.ID, prev, replacedBy, now, namingHistoryKeep)
		}
		// Record what clonarr applied, per field, for drift + update detection.
		if cfg.NamingApplied == nil {
			cfg.NamingApplied = map[string]map[string]core.NamingAppliedRecord{}
		}
		if cfg.NamingApplied[inst.ID] == nil {
			cfg.NamingApplied[inst.ID] = map[string]core.NamingAppliedRecord{}
		}
		for field, pattern := range fields {
			if pattern == "" {
				continue // unset field wasn't applied — don't record it
			}
			rec := cfg.NamingApplied[inst.ID][field]
			if sc, ok := schemes[field]; ok {
				rec.Scheme = sc // explicit (manual/auto/enable); absent => preserve (rollback)
			}
			rec.Fingerprint = namingFingerprint(pattern)
			rec.AppliedAt = now
			cfg.NamingApplied[inst.ID][field] = rec
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
		fields := map[string]string{}
		fp := map[string]string{}
		for _, field := range toApply {
			pattern, ok := resolveNamingField(ad, inst.Type, field, incoming[field].Scheme)
			if !ok || pattern == "" {
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
			_, _, applyErr := s.applyNamingFields(inst, fields, enSchemes, "auto-sync", true)
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

// handleGetNamingHistory returns the instance's rollback snapshots, newest last.
func (s *Server) handleGetNamingHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg := s.Core.Config.Get()
	snaps := []core.NamingSnapshot{}
	if cfg.NamingHistory != nil {
		if v, ok := cfg.NamingHistory[id]; ok {
			snaps = append(snaps, v...)
		}
	}
	writeJSON(w, snaps)
}

// handleRestoreNaming restores a previous naming snapshot back to the instance.
// Body: {"index": N} selects a snapshot (default: most recent). The restore writes
// through the same apply path, so it takes its own snapshot first and is itself
// reversible. It does NOT change auto-sync bindings — restore is a value recovery,
// not an opt-out; disable auto-sync separately to stop following a scheme.
func (s *Server) handleRestoreNaming(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}

	cfg := s.Core.Config.Get()
	snaps := cfg.NamingHistory[id]
	if len(snaps) == 0 {
		writeError(w, 404, "No naming history for this instance")
		return
	}

	idx := len(snaps) - 1
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var req struct {
			Index *int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Index != nil {
			idx = *req.Index
		}
	}
	if idx < 0 || idx >= len(snaps) {
		writeError(w, 400, "Invalid snapshot index")
		return
	}

	snap := snaps[idx]
	if len(snap.Naming) == 0 {
		writeError(w, 400, "Selected snapshot has no naming to restore")
		return
	}

	// Rollback restores prior patterns; pass no schemes so each field's existing
	// intended scheme is preserved (only the applied fingerprint updates).
	if _, _, err := s.applyNamingFields(inst, snap.Naming, nil, "rollback", true); err != nil {
		log.Printf("Naming restore [%s]: failed: %v", inst.Name, err)
		s.Core.DebugLog.Logf(core.LogError, "Naming restore [%s]: failed: %v", inst.Name, err)
		writeError(w, 502, "Failed to restore naming on the instance")
		return
	}
	log.Printf("Naming restore [%s]: restored snapshot from %s", inst.Name, snap.TakenAt)
	s.Core.DebugLog.Logf(core.LogAutoSync, "Naming restore [%s]: restored snapshot from %s", inst.Name, snap.TakenAt)
	writeJSON(w, map[string]string{"status": "restored"})
}

// AutoSyncNaming runs after a TRaSH pull (scheduled + manual; NOT at startup). For each instance with
// opted-in naming fields, it re-applies any field whose TRaSH pattern has changed
// since the last apply (fingerprint differs) — so a field keeps following its
// scheme as the guide evolves. Fields whose pattern is unchanged are skipped (the
// common case, so most ticks are no-ops). It reacts only to guide changes; Arr-side
// drift is deferred. Default-off: instances absent from NamingAutoSync do nothing.
func (s *Server) AutoSyncNaming() {
	cfg := s.Core.Config.Get()
	if len(cfg.NamingAutoSync) == 0 {
		return
	}

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

		fieldsToApply := map[string]string{}
		newFingerprint := map[string]string{}
		for field, b := range bindings {
			pattern, ok := resolveNamingField(ad, inst.Type, field, b.Scheme)
			if !ok || pattern == "" {
				continue // guide has no pattern for this (field, scheme) — never guess
			}
			fp := namingFingerprint(pattern)
			if fp == b.LastFingerprint {
				continue // unchanged since last apply
			}
			fieldsToApply[field] = pattern
			newFingerprint[field] = fp
		}
		if len(fieldsToApply) == 0 {
			continue
		}

		schemes := map[string]string{}
		for field := range fieldsToApply {
			schemes[field] = bindings[field].Scheme
		}
		_, prev, err := s.applyNamingFields(inst, fieldsToApply, schemes, "auto-sync", true)
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
			})
		}
		s.Core.NotifyNamingAutoSync(inst.Name, changes)
	}
}
