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

// NamingFieldLabel is the human label for a canonical naming field key, used in
// the auto-sync + drift notifications.
func NamingFieldLabel(field string) string {
	switch field {
	case "movieFile":
		return "Movie file"
	case "movieFolder":
		return "Movie folder"
	case "standardEpisode":
		return "Episode (Standard)"
	case "dailyEpisode":
		return "Episode (Daily)"
	case "animeEpisode":
		return "Episode (Anime)"
	case "seriesFolder":
		return "Series folder"
	case "seasonFolder":
		return "Season folder"
	case "specialsFolder":
		return "Specials folder"
	}
	return field
}
