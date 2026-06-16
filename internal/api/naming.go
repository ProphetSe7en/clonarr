package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Naming auto-sync (#5) shared core: resolving a TRaSH scheme to a single naming
// field, applying fields to an instance (per-field; others preserved), capturing
// a pre-apply snapshot for rollback, and fingerprinting. Used by both the manual
// apply handler and (later phases) the scheduled AutoSyncNaming loop, so there is
// one apply path. Field keys are clonarr's canonical naming-field identifiers.

const namingHistoryKeep = 5 // rollback snapshots kept per instance

// namingArrKey maps a canonical field key → the Arr /config/naming key.
var namingArrKey = map[string]string{
	"movieFile":       "standardMovieFormat",
	"movieFolder":     "movieFolderFormat",
	"standardEpisode": "standardEpisodeFormat",
	"dailyEpisode":    "dailyEpisodeFormat",
	"animeEpisode":    "animeEpisodeFormat",
	"seriesFolder":    "seriesFolderFormat",
	"seasonFolder":    "seasonFolderFormat",
	"specialsFolder":  "specialsFolderFormat",
}

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

// namingFingerprint is a short, stable hash of an applied pattern, stored on the
// binding so the loop can tell "TRaSH changed this field" from "no change".
func namingFingerprint(pattern string) string {
	sum := sha256.Sum256([]byte(pattern))
	return hex.EncodeToString(sum[:])[:16]
}

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
func (s *Server) applyNamingFields(inst core.Instance, fields map[string]string, replacedBy string, snapshot bool) (arr.ArrNamingConfig, error) {
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	current, err := client.GetNaming()
	if err != nil {
		return nil, err
	}
	// Capture the pre-apply state now (before we mutate current), but only
	// PERSIST it after the write succeeds — a failed UpdateNaming must not leave
	// an orphan rollback snapshot (matters once the loop drives this at volume).
	var prev map[string]string
	if snapshot {
		prev = extractNamingPatterns(inst.Type, current)
	}
	applyFieldsToArrNaming(inst.Type, current, fields)
	result, err := client.UpdateNaming(current)
	if err != nil {
		return nil, err
	}
	if snapshot {
		now := time.Now().UTC().Format(time.RFC3339)
		s.Core.Config.Update(func(cfg *core.Config) {
			appendNamingSnapshot(cfg, inst.ID, prev, replacedBy, now, namingHistoryKeep)
		})
	}
	return result, nil
}
