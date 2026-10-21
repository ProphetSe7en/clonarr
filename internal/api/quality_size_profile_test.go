package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"clonarr/internal/arr"
	"clonarr/internal/core"
)

func qsItem(name string, allowed bool, min, pref, max *float64) arr.ArrQualityItem {
	return arr.ArrQualityItem{
		Quality:       &arr.ArrQualityRef{Name: name},
		Items:         []arr.ArrQualityItem{},
		Allowed:       allowed,
		MinSize:       min,
		PreferredSize: pref,
		MaxSize:       max,
	}
}

func f(v float64) *float64 { return &v }

// buildQSProfileRows is what backs the tab's table, and it has to work for a
// profile that exists and for the blank template the app offers for one that
// does not. Both come in as the same []ArrQualityItem, so one test covers it.
func TestBuildQSProfileRowsFlattensLeavesAndAttributes(t *testing.T) {
	items := []arr.ArrQualityItem{
		qsItem("SDTV", false, f(2), f(95), f(100)),
		{ID: 1000, Name: "WEB 1080p", Allowed: true, Items: []arr.ArrQualityItem{
			qsItem("WEBRip-1080p", true, f(4), f(95), f(130)),
			qsItem("WEBDL-1080p", true, f(4), f(95), f(130)),
		}},
		qsItem("Bluray-1080p", true, f(4), f(95), f(155)),
	}
	sizes := map[string]core.QualitySizeTriple{
		"WEBDL-1080p":  {Min: f(15), Preferred: f(995), Max: f(1000)},
		"Bluray-1080p": {Min: f(7), Preferred: f(40), Max: f(60)},
	}
	overrides := map[string]core.QSOverride{"Bluray-1080p": {Min: 7, Preferred: 40, Max: 60}}

	rows := buildQSProfileRows(items, sizes, overrides, nil)
	if len(rows) != 4 {
		t.Fatalf("want four leaves, got %d", len(rows))
	}
	by := map[string]qsProfileRow{}
	for _, r := range rows {
		by[r.Quality] = r
	}
	// A group row is not a row of its own; it is context on its members.
	if _, ok := by["WEB 1080p"]; ok {
		t.Error("the group row must not become a row")
	}
	if by["WEBDL-1080p"].Group != "WEB 1080p" {
		t.Errorf("group: got %q", by["WEBDL-1080p"].Group)
	}
	// Attribution is computed here, not inferred by the frontend.
	if by["WEBDL-1080p"].Source != "set" {
		t.Errorf("WEBDL-1080p: want source set, got %q", by["WEBDL-1080p"].Source)
	}
	if by["Bluray-1080p"].Source != "override" {
		t.Errorf("Bluray-1080p: want source override, got %q", by["Bluray-1080p"].Source)
	}
	// A quality the chosen set does not name gets no resolved value and no
	// source, which is how the tab says "left as is".
	if by["SDTV"].Source != "" || by["SDTV"].Resolved.Min != nil {
		t.Errorf("SDTV: want nothing written, got source %q", by["SDTV"].Source)
	}
	// The instance's own values come through for every row either way.
	if by["SDTV"].Current.Min == nil || *by["SDTV"].Current.Min != 2 {
		t.Errorf("SDTV current: want 2, got %v", by["SDTV"].Current.Min)
	}
	// Highest priority first, which is the order both Arr UIs show.
	if rows[0].Quality != "Bluray-1080p" || rows[len(rows)-1].Quality != "SDTV" {
		t.Errorf("order: got %s … %s", rows[0].Quality, rows[len(rows)-1].Quality)
	}
}

// The blank-profile template the app offers for a profile that does not exist
// yet has every quality switched off, so its allowed flags say nothing about
// the guide profile the user is looking at. Without the override every row
// claimed "not allowed" about a profile that does allow some, and the default
// view, which lists what the profile allows, came out empty.
func TestBuildQSProfileRowsTakesAllowedFromTheGuideProfile(t *testing.T) {
	blank := []arr.ArrQualityItem{
		qsItem("SDTV", false, nil, nil, nil),
		{ID: 1000, Name: "WEB 1080p", Allowed: false, Items: []arr.ArrQualityItem{
			qsItem("WEBRip-1080p", false, nil, nil, nil),
			qsItem("WEBDL-1080p", false, nil, nil, nil),
		}},
	}
	rows := buildQSProfileRows(blank, nil, nil, map[string]bool{
		"WEBDL-1080p":  true,
		"WEBRip-1080p": true,
	})
	by := map[string]qsProfileRow{}
	for _, r := range rows {
		by[r.Quality] = r
	}
	for _, name := range []string{"WEBDL-1080p", "WEBRip-1080p"} {
		if !by[name].Allowed {
			t.Errorf("%s: the guide profile allows it, want allowed", name)
		}
	}
	if by["SDTV"].Allowed {
		t.Error("SDTV: the guide profile does not allow it, want not allowed")
	}
}

// A group's members inherit its allowed state, which is what the profile does.
func TestGuideAllowedQualitiesExpandsGroups(t *testing.T) {
	ad := &core.AppData{Profiles: []*core.TrashQualityProfile{{
		TrashID: "web-1080p",
		Items: []core.QualityItem{
			{Name: "SDTV", Allowed: false},
			{Name: "WEB 1080p", Allowed: true, Items: []string{"WEBDL-1080p", "WEBRip-1080p"}},
			{Name: "Bluray-1080p", Allowed: true},
		},
	}}}
	got := guideAllowedQualities(ad, "web-1080p")
	want := map[string]bool{"WEBDL-1080p": true, "WEBRip-1080p": true, "Bluray-1080p": true}
	if len(got) != len(want) {
		t.Fatalf("want %d allowed, got %d (%v)", len(want), len(got), got)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s: want allowed", name)
		}
	}
	// A profile the guide does not have must not be reported as "allows
	// nothing", which would hide every row. Nil means "no opinion".
	if guideAllowedQualities(ad, "not-a-profile") != nil {
		t.Error("unknown trash id: want nil")
	}
	if guideAllowedQualities(nil, "web-1080p") != nil {
		t.Error("no app data: want nil")
	}
}

// Deleting a set a rule points at used to be refused. It now goes through and
// the rule keeps writing the same numbers as its own per-quality values, which
// is the inverse of what "Save as set" did. The rule must not be left pointing
// at a set that no longer exists, because that resolves to nothing and would
// quietly stop it writing sizes.
func TestDeleteQualitySizeSetConvertsRulesToOwnValues(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.QualitySizeSets = []core.QualitySizeSet{{
		ID:      "set-1",
		Name:    "Min Test",
		AppType: "sonarr",
		Qualities: []core.QualitySizeEntry{
			{Quality: "WEBDL-1080p", Min: 7, Preferred: 40, Max: 60},
			{Quality: "WEBRip-1080p", Min: 15, Preferred: 995, Max: 1000},
		},
	}}
	cfg.Instances = []core.Instance{{ID: "inst-1", Name: "Sonarr v5", Type: "sonarr", URL: "http://x", APIKey: "k"}}
	cfg.AutoSync.Rules = []core.AutoSyncRule{
		{
			ID: "r-points-at-set", InstanceID: "inst-1", ArrProfileID: 4,
			QualitySizeSource: "custom:set-1",
			// An explicit value already beat the set, so it has to survive.
			QualitySizeOverrides: map[string]core.QSOverride{
				"WEBDL-1080p": {Min: 1, Preferred: 2, Max: 3},
			},
		},
		{ID: "r-elsewhere", InstanceID: "inst-1", ArrProfileID: 5, QualitySizeSource: "guide:abc"},
	}

	s, _ := setupConfigScheduleServer(t, cfg)

	if used := s.qsSetUsedBy("set-1"); len(used) != 1 || used[0] != "Sonarr v5" {
		t.Fatalf("qsSetUsedBy = %v, want [Sonarr v5]", used)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/quality-size-sets/set-1", nil)
	req.SetPathValue("id", "set-1")
	rec := httptest.NewRecorder()
	s.handleDeleteQualitySizeSet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}

	got := s.Core.Config.Get()
	if len(got.QualitySizeSets) != 0 {
		t.Errorf("the set is still there: %+v", got.QualitySizeSets)
	}
	var converted, untouched *core.AutoSyncRule
	for i := range got.AutoSync.Rules {
		switch got.AutoSync.Rules[i].ID {
		case "r-points-at-set":
			converted = &got.AutoSync.Rules[i]
		case "r-elsewhere":
			untouched = &got.AutoSync.Rules[i]
		}
	}
	if converted == nil || untouched == nil {
		t.Fatal("rules missing after delete")
	}
	// No guide data in this fixture, so there is no default set to land on and
	// the rule has to carry the lot. "off" and not "", or no partial writer
	// could tell it apart from a field that was never sent.
	if converted.QualitySizeSource != core.QSSourceOff {
		t.Errorf("source = %q, want %q", converted.QualitySizeSource, core.QSSourceOff)
	}
	if n := len(converted.QualitySizeOverrides); n != 2 {
		t.Fatalf("overrides = %d, want 2: %+v", n, converted.QualitySizeOverrides)
	}
	if ov := converted.QualitySizeOverrides["WEBRip-1080p"]; ov.Min != 15 || ov.Preferred != 995 || ov.Max != 1000 {
		t.Errorf("WEBRip-1080p = %+v, want the set's 15/995/1000", ov)
	}
	if ov := converted.QualitySizeOverrides["WEBDL-1080p"]; ov.Min != 1 || ov.Preferred != 2 || ov.Max != 3 {
		t.Errorf("WEBDL-1080p = %+v, want the rule's own 1/2/3 to win over the set", ov)
	}
	if untouched.QualitySizeSource != "guide:abc" || untouched.QualitySizeOverrides != nil {
		t.Errorf("a rule on another source was touched: %+v", untouched)
	}
}

// A deleted set should leave the rule on the app default plus only what
// disagreed with it. Converting every quality instead overrode every row, so
// the source picker had no visible effect and read as frozen.
func TestQSSetDeletionIntentPrefersTheDefaultPlusDeviations(t *testing.T) {
	def := &core.TrashQualitySize{
		TrashID: "series-id",
		Type:    "series",
		Qualities: []core.QualitySizeEntry{
			{Quality: "WEBDL-1080p", Min: 15, Preferred: 995, Max: 1000},
			{Quality: "WEBRip-1080p", Min: 15, Preferred: 995, Max: 1000},
		},
	}
	// Saved from the series set with one value changed.
	deleted := []core.QualitySizeEntry{
		{Quality: "WEBDL-1080p", Min: 7, Preferred: 40, Max: 60},
		{Quality: "WEBRip-1080p", Min: 15, Preferred: 995, Max: 1000},
	}
	src, ovs := qsSetDeletionIntent(deleted, def)
	if src != core.QSSourceSetPfx+"series-id" {
		t.Errorf("source = %q, want the default set", src)
	}
	if len(ovs) != 1 {
		t.Fatalf("overrides = %+v, want only the one that differs", ovs)
	}
	if ov := ovs["WEBDL-1080p"]; ov.Min != 7 || ov.Preferred != 40 || ov.Max != 60 {
		t.Errorf("WEBDL-1080p = %+v, want 7/40/60", ov)
	}
}

// Landing on the default set is only safe when the deleted set covered
// everything the default covers. Otherwise the default starts writing sizes
// for qualities this rule never wrote, and there is no way to say "leave this
// one alone" on top of a set.
func TestQSSetDeletionIntentWillNotIntroduceWrites(t *testing.T) {
	def := &core.TrashQualitySize{
		TrashID: "series-id",
		Type:    "series",
		Qualities: []core.QualitySizeEntry{
			{Quality: "WEBDL-1080p", Min: 15, Preferred: 995, Max: 1000},
			{Quality: "Bluray-1080p", Min: 50, Preferred: 995, Max: 1000},
		},
	}
	// Covers one quality the default covers, but not Bluray-1080p.
	deleted := []core.QualitySizeEntry{{Quality: "WEBDL-1080p", Min: 7, Preferred: 40, Max: 60}}
	src, ovs := qsSetDeletionIntent(deleted, def)
	if src != core.QSSourceOff {
		t.Errorf("source = %q, want off: landing on the default would start writing Bluray-1080p", src)
	}
	if len(ovs) != 1 || ovs["WEBDL-1080p"].Min != 7 {
		t.Errorf("overrides = %+v, want the deleted set's own value carried", ovs)
	}
}

// A rerun from history reuses the source that sync used, which keeps a rule
// following the set it followed. When that set has been deleted since, the
// source resolves to nothing and the rerun would write no sizes while
// reporting success. The numbers the entry recorded are the way out, stored as
// a named set so the rule still reads as a choice rather than carrying an
// override on every quality.
func TestApplyQualitySizeFallbackStoresARestoredSet(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.QualitySizeSets = []core.QualitySizeSet{
		{ID: "alive", Name: "Still here", AppType: "sonarr"},
	}
	s, _ := setupConfigScheduleServer(t, cfg)

	// A source that still resolves is left alone: reusing it is the point.
	keep := &core.SyncRequest{
		QualitySizeSource:   "custom:alive",
		QualitySizeFallback: map[string]core.QSOverride{"SDTV": {Min: 1, Preferred: 2, Max: 3}},
	}
	s.applyQualitySizeFallback(keep, "sonarr")
	if keep.QualitySizeSource != "custom:alive" {
		t.Errorf("source = %q, want it untouched", keep.QualitySizeSource)
	}
	if n := len(s.Core.Config.Get().QualitySizeSets); n != 1 {
		t.Errorf("sets = %d, want no new one", n)
	}

	req := &core.SyncRequest{
		QualitySizeSource:       "custom:deleted-long-ago",
		QualitySizeFallbackName: "My sizes (restored 21/10/2026)",
		QualitySizeFallback: map[string]core.QSOverride{
			"WEBDL-1080p": {Min: 15, Preferred: 995, Max: 1000},
			"SDTV":        {Min: 2, Preferred: 95, Max: 100},
		},
	}
	s.applyQualitySizeFallback(req, "sonarr")

	sets := s.Core.Config.Get().QualitySizeSets
	if len(sets) != 2 {
		t.Fatalf("sets = %d, want the restored one stored: %+v", len(sets), sets)
	}
	stored := sets[1]
	if stored.Name != "My sizes (restored 21/10/2026)" || stored.AppType != "sonarr" {
		t.Errorf("stored = %+v", stored)
	}
	if len(stored.Qualities) != 2 {
		t.Fatalf("qualities = %d, want 2", len(stored.Qualities))
	}
	// Sorted, so the set reads the same every time it is written.
	if stored.Qualities[0].Quality != "SDTV" || stored.Qualities[1].Quality != "WEBDL-1080p" {
		t.Errorf("order = %q, %q", stored.Qualities[0].Quality, stored.Qualities[1].Quality)
	}
	if req.QualitySizeSource != "custom:"+stored.ID {
		t.Errorf("source = %q, want the new set", req.QualitySizeSource)
	}
	// The rule must not be left with an override on every quality, which is
	// what made the source picker read as broken.
	if len(req.QualitySizeOverrides) != 0 {
		t.Errorf("overrides = %+v, want none", req.QualitySizeOverrides)
	}

	// Rerunning the same entry reuses the set rather than stacking copies.
	again := &core.SyncRequest{
		QualitySizeSource:       "custom:deleted-long-ago",
		QualitySizeFallbackName: "My sizes (restored 21/10/2026)",
		QualitySizeFallback:     req.QualitySizeFallback,
	}
	s.applyQualitySizeFallback(again, "sonarr")
	if n := len(s.Core.Config.Get().QualitySizeSets); n != 2 {
		t.Errorf("sets = %d, want the same two", n)
	}
	if again.QualitySizeSource != "custom:"+stored.ID {
		t.Errorf("source = %q, want the set already stored", again.QualitySizeSource)
	}
}
