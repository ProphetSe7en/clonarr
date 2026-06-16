package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"fmt"
	"testing"
)

func testNamingAppData() *core.AppData {
	return &core.AppData{Naming: &core.TrashNaming{
		File:   map[string]string{"plex": "MOVIEFILE-PLEX", "default": "MOVIEFILE-DEF"},
		Folder: map[string]string{"plex": "MOVIEFOLDER-PLEX"},
		Series: map[string]string{"plex": "SERIES-PLEX"},
		Season: map[string]string{"default": "SEASON-DEF"},
		Episodes: map[string]map[string]string{
			"standard": {"plex": "EP-STD-PLEX"},
			"daily":    {"plex": "EP-DAILY-PLEX"},
			"anime":    {"plex": "EP-ANIME-PLEX"},
		},
	}}
}

func TestResolveNamingField(t *testing.T) {
	ad := testNamingAppData()
	cases := []struct {
		instType, field, scheme, want string
		wantOK                        bool
	}{
		{"radarr", "movieFile", "plex", "MOVIEFILE-PLEX", true},
		{"radarr", "movieFolder", "plex", "MOVIEFOLDER-PLEX", true},
		{"sonarr", "standardEpisode", "plex", "EP-STD-PLEX", true},
		{"sonarr", "dailyEpisode", "plex", "EP-DAILY-PLEX", true},
		{"sonarr", "animeEpisode", "plex", "EP-ANIME-PLEX", true},
		{"sonarr", "seriesFolder", "plex", "SERIES-PLEX", true},
		{"sonarr", "seasonFolder", "default", "SEASON-DEF", true},
		{"radarr", "movieFile", "nope", "", false}, // unknown scheme
		{"sonarr", "standardEpisode", "nope", "", false},
	}
	for _, c := range cases {
		got, ok := resolveNamingField(ad, c.instType, c.field, c.scheme)
		if got != c.want || ok != c.wantOK {
			t.Errorf("resolve(%s,%s,%s) = (%q,%v), want (%q,%v)", c.instType, c.field, c.scheme, got, ok, c.want, c.wantOK)
		}
	}
	// Nil guards must not panic.
	if _, ok := resolveNamingField(nil, "radarr", "movieFile", "plex"); ok {
		t.Error("nil AppData should return ok=false")
	}
	if _, ok := resolveNamingField(&core.AppData{}, "radarr", "movieFile", "plex"); ok {
		t.Error("nil Naming should return ok=false")
	}
}

func TestApplyFieldsToArrNaming(t *testing.T) {
	cur := arr.ArrNamingConfig{"standardMovieFormat": "OLD", "someOtherKey": "keep"}
	applyFieldsToArrNaming("radarr", cur, map[string]string{"movieFile": "NEW", "movieFolder": ""})
	if cur["standardMovieFormat"] != "NEW" {
		t.Errorf("movieFile not applied: %v", cur["standardMovieFormat"])
	}
	if _, set := cur["movieFolderFormat"]; set {
		t.Error("empty movieFolder should be skipped, not written")
	}
	if cur["someOtherKey"] != "keep" {
		t.Error("unrelated key must be preserved")
	}
	if cur["renameMovies"] != true || cur["replaceIllegalCharacters"] != true {
		t.Error("rename flags not set for radarr")
	}
}

func TestExtractNamingPatterns(t *testing.T) {
	cur := arr.ArrNamingConfig{"standardMovieFormat": "F", "movieFolderFormat": "D", "unrelated": "x"}
	p := extractNamingPatterns("radarr", cur)
	if p["movieFile"] != "F" || p["movieFolder"] != "D" {
		t.Errorf("extract = %v", p)
	}
	if _, ok := p["unrelated"]; ok {
		t.Error("unrelated key leaked into patterns")
	}
}

func TestNamingFingerprint(t *testing.T) {
	a, b, c := namingFingerprint("x"), namingFingerprint("x"), namingFingerprint("y")
	if a != b {
		t.Error("fingerprint not stable for same input")
	}
	if a == c {
		t.Error("fingerprint collided for different input")
	}
	if len(a) != 16 {
		t.Errorf("fingerprint length = %d, want 16", len(a))
	}
}

func TestAppendNamingSnapshot_Caps(t *testing.T) {
	cfg := &core.Config{}
	for i := 0; i < 8; i++ {
		appendNamingSnapshot(cfg, "inst", map[string]string{"movieFile": fmt.Sprintf("v%d", i)}, "manual", fmt.Sprintf("t%d", i), 5)
	}
	got := cfg.NamingHistory["inst"]
	if len(got) != 5 {
		t.Fatalf("kept %d snapshots, want 5", len(got))
	}
	if got[len(got)-1].Naming["movieFile"] != "v7" {
		t.Errorf("newest snapshot = %q, want v7", got[len(got)-1].Naming["movieFile"])
	}
	if got[0].Naming["movieFile"] != "v3" {
		t.Errorf("oldest kept = %q, want v3 (8 added, keep 5)", got[0].Naming["movieFile"])
	}
}
