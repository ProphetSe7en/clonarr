package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// Shared naming-scheme helpers. They live in core (not api) because the drift
// runner — also in core — needs them, and core cannot import api. The api layer
// (manual apply, auto-sync loop) references these same helpers, so the canonical
// field → Arr-key mapping has a single source of truth (a divergence here would
// mean writing/reading the wrong naming field — high-stakes).

// NamingArrKey maps a canonical naming-field key → the Arr /config/naming key.
var NamingArrKey = map[string]string{
	"movieFile":       "standardMovieFormat",
	"movieFolder":     "movieFolderFormat",
	"standardEpisode": "standardEpisodeFormat",
	"dailyEpisode":    "dailyEpisodeFormat",
	"animeEpisode":    "animeEpisodeFormat",
	"seriesFolder":    "seriesFolderFormat",
	"seasonFolder":    "seasonFolderFormat",
	"specialsFolder":  "specialsFolderFormat",
}

// NamingFingerprint is a short, stable hash of a naming pattern. Used to tell
// "this pattern changed" from "no change" without storing the full string.
func NamingFingerprint(pattern string) string {
	sum := sha256.Sum256([]byte(pattern))
	return hex.EncodeToString(sum[:])[:16]
}

// ResolveNamingField returns the TRaSH pattern for one naming field under a given
// scheme. ok=false when TRaSH has no pattern for that (field, scheme). Lives in
// core so both the api apply paths and the core drift/update pass share it.
func ResolveNamingField(ad *AppData, instType, field, scheme string) (string, bool) {
	if ad == nil {
		return "", false
	}
	return ResolveNamingFieldFrom(ad.Naming, instType, field, scheme)
}

// ResolveNamingFieldFrom is ResolveNamingField against an explicit TrashNaming —
// used to resolve patterns from the UPSTREAM side-ref naming (read with
// ReadNamingFromRef, no working-tree pull) for the update check, not just the
// in-memory loaded guide.
func ResolveNamingFieldFrom(n *TrashNaming, instType, field, scheme string) (string, bool) {
	if n == nil {
		return "", false
	}
	switch field {
	case "movieFile":
		v, ok := n.File[scheme]
		return v, ok
	case "movieFolder":
		v, ok := n.Folder[scheme]
		return v, ok
	case "seriesFolder":
		v, ok := n.Series[scheme]
		return v, ok
	case "seasonFolder":
		v, ok := n.Season[scheme]
		return v, ok
	case "standardEpisode", "dailyEpisode", "animeEpisode":
		sub := map[string]string{"standardEpisode": "standard", "dailyEpisode": "daily", "animeEpisode": "anime"}[field]
		if ep := n.Episodes[sub]; ep != nil {
			v, ok := ep[scheme]
			return v, ok
		}
	}
	return "", false
}

// NamingFieldLabel is the human label for a canonical naming field key, used in
// the auto-sync + drift notifications. Matches Radarr/Sonarr's exact setting names
// (and the naming page's scheme-card headers) so the terminology is consistent.
func NamingFieldLabel(field string) string {
	switch field {
	case "movieFile":
		return "Standard Movie Format"
	case "movieFolder":
		return "Movie Folder Format"
	case "standardEpisode":
		return "Standard Episode Format"
	case "dailyEpisode":
		return "Daily Episode Format"
	case "animeEpisode":
		return "Anime Episode Format"
	case "seriesFolder":
		return "Series Folder Format"
	case "seasonFolder":
		return "Season Folder Format"
	case "specialsFolder":
		return "Specials Folder Format"
	}
	return field
}
