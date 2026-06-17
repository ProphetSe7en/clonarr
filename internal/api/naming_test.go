package api

import (
	"bytes"
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// --- phase 2 handler tests ---

func seedNamingInstance(t *testing.T, app *core.App, instType string) core.Instance {
	t.Helper()
	inst, err := app.Config.AddInstance(core.Instance{
		Name: "Test " + instType, Type: instType, URL: "http://arr.local:8989", APIKey: "k",
	})
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	return inst
}

func putNamingAutoSync(t *testing.T, server *Server, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/instances/"+id+"/naming/auto-sync", bytes.NewReader(data))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	server.handleSaveNamingAutoSync(w, req)
	return w
}

func TestHandleSaveNamingAutoSync_PersistsAndValidates(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	sonarr := seedNamingInstance(t, app, "sonarr")

	// Valid: a sonarr field bound to a scheme.
	if w := putNamingAutoSync(t, server, sonarr.ID, map[string]core.NamingFieldBinding{
		"standardEpisode": {Scheme: "plex"},
	}); w.Code != http.StatusOK {
		t.Fatalf("valid save: status = %d, want 200", w.Code)
	}
	stored := app.Config.Get().NamingAutoSync[sonarr.ID]
	if stored["standardEpisode"].Scheme != "plex" {
		t.Fatalf("binding not persisted: %#v", stored)
	}

	// Reject a field that is not auto-syncable for sonarr (specials is manual-only).
	if w := putNamingAutoSync(t, server, sonarr.ID, map[string]core.NamingFieldBinding{
		"specialsFolder": {Scheme: "plex"},
	}); w.Code != http.StatusBadRequest {
		t.Errorf("specialsFolder: status = %d, want 400", w.Code)
	}
	// Reject a radarr field on a sonarr instance.
	if w := putNamingAutoSync(t, server, sonarr.ID, map[string]core.NamingFieldBinding{
		"movieFile": {Scheme: "plex"},
	}); w.Code != http.StatusBadRequest {
		t.Errorf("cross-type field: status = %d, want 400", w.Code)
	}
	// Reject a binding with no scheme.
	if w := putNamingAutoSync(t, server, sonarr.ID, map[string]core.NamingFieldBinding{
		"seriesFolder": {Scheme: ""},
	}); w.Code != http.StatusBadRequest {
		t.Errorf("empty scheme: status = %d, want 400", w.Code)
	}
}

func TestHandleSaveNamingAutoSync_OptOutAndSchemeReset(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	radarr := seedNamingInstance(t, app, "radarr")

	// Pre-seed an applied binding (server-owned apply state present).
	app.Config.Update(func(c *core.Config) {
		c.NamingAutoSync = map[string]map[string]core.NamingFieldBinding{
			radarr.ID: {"movieFile": {Scheme: "plex", LastFingerprint: "abc123", LastAppliedAt: "t0"}},
		}
	})

	// Same scheme → apply state preserved.
	putNamingAutoSync(t, server, radarr.ID, map[string]core.NamingFieldBinding{
		"movieFile": {Scheme: "plex"},
	})
	if got := app.Config.Get().NamingAutoSync[radarr.ID]["movieFile"]; got.LastFingerprint != "abc123" {
		t.Errorf("same scheme should preserve fingerprint, got %q", got.LastFingerprint)
	}

	// Scheme change → apply state reset so the next tick re-applies.
	putNamingAutoSync(t, server, radarr.ID, map[string]core.NamingFieldBinding{
		"movieFile": {Scheme: "emby"},
	})
	if got := app.Config.Get().NamingAutoSync[radarr.ID]["movieFile"]; got.Scheme != "emby" || got.LastFingerprint != "" {
		t.Errorf("scheme change should reset fingerprint, got %#v", got)
	}

	// Empty body → opt out entirely (instance removed from the map).
	putNamingAutoSync(t, server, radarr.ID, map[string]core.NamingFieldBinding{})
	if _, ok := app.Config.Get().NamingAutoSync[radarr.ID]; ok {
		t.Error("empty body should remove the instance's bindings")
	}
}
