package core

import (
	"encoding/json"
	"strings"
	"testing"

	"clonarr/internal/arr"
)

// The guide's own numbers, so the tests fail if the real values move.
func testGuideSets() []*TrashQualitySize {
	return []*TrashQualitySize{
		{
			TrashID: "bef99584217af744e404ed44a33af589",
			Type:    "series",
			Qualities: []QualitySizeEntry{
				{Quality: "WEBDL-1080p", Min: 15, Preferred: 995, Max: 1000},
				{Quality: "WEBRip-1080p", Min: 15, Preferred: 995, Max: 1000},
				{Quality: "Bluray-720p", Min: 17.1, Preferred: 995, Max: 1000},
			},
		},
		{
			TrashID: "387e6278d8e06083d813358762e0ac63",
			Type:    "anime",
			Qualities: []QualitySizeEntry{
				{Quality: "WEBDL-1080p", Min: 50.4, Preferred: 995, Max: 1000},
				{Quality: "WEBRip-1080p", Min: 15, Preferred: 995, Max: 1000},
				{Quality: "SDTV", Min: 2, Preferred: 100, Max: 100},
			},
		},
	}
}

func sizeDefs(withSizes bool, names ...string) []arr.ArrQualityDefinition {
	out := make([]arr.ArrQualityDefinition, 0, len(names))
	for i, n := range names {
		d := arr.ArrQualityDefinition{ID: i + 1}
		d.Quality.ID = i + 1
		d.Quality.Name = n
		if withSizes {
			min, pref, max := 1.0, 2.0, 3.0
			d.MinSize, d.PreferredSize, d.MaxSize = &min, &pref, &max
		}
		out = append(out, d)
	}
	return out
}

func TestResolveQualitySizesOffAndEmptyWriteNothing(t *testing.T) {
	for _, src := range []string{"", "off", "nonsense", "guide:deadbeef", "custom:missing"} {
		got := ResolveQualitySizes(QualitySizeResolveInput{
			Source:    src,
			AppType:   "sonarr",
			GuideSets: testGuideSets(),
		})
		if got != nil {
			t.Errorf("source %q: want nil, got %v", src, got)
		}
	}
}

func TestResolveQualitySizesNamedGuideSet(t *testing.T) {
	got := ResolveQualitySizes(QualitySizeResolveInput{
		Source:    "guide:387e6278d8e06083d813358762e0ac63",
		AppType:   "sonarr",
		GuideSets: testGuideSets(),
	})
	if len(got) != 3 {
		t.Fatalf("want 3 qualities, got %d", len(got))
	}
	// The anime set's WEBDL-1080p is 50.4, the series set's is 15. Picking the
	// wrong set is the single most likely bug here, so assert the value that
	// tells them apart.
	if *got["WEBDL-1080p"].Min != 50.4 {
		t.Errorf("WEBDL-1080p min: want 50.4 from the anime set, got %v", *got["WEBDL-1080p"].Min)
	}
}

func TestResolveQualitySizesGuideFallsBackToAppDefault(t *testing.T) {
	// No profile reference, which is every profile until upstream publishes
	// trash_quality_definition. The guide author's answer is that the default
	// applies, and for Sonarr that is series.
	got := ResolveQualitySizes(QualitySizeResolveInput{
		Source:    QSSourceGuide,
		AppType:   "sonarr",
		GuideSets: testGuideSets(),
	})
	if *got["WEBDL-1080p"].Min != 15 {
		t.Errorf("WEBDL-1080p min: want 15 from the series default, got %v", *got["WEBDL-1080p"].Min)
	}
	if _, ok := got["SDTV"]; ok {
		t.Error("SDTV is in the anime set only; the series default must not carry it")
	}
}

func TestResolveQualitySizesKeeps995And1000(t *testing.T) {
	// The v4 shared-list path collapses a value at its limit to nil so the
	// write matches what the Arr UI stores for Unlimited. Every guide entry
	// sits at exactly preferred 995 / max 1000, which are the Sonarr
	// thresholds, so reusing that collapse here would nil every published
	// size. v5 stores 995 and 1000 verbatim.
	got := ResolveQualitySizes(QualitySizeResolveInput{
		Source:    QSSourceGuide,
		AppType:   "sonarr",
		GuideSets: testGuideSets(),
	})
	tr := got["WEBDL-1080p"]
	if tr.Preferred == nil || *tr.Preferred != 995 {
		t.Errorf("preferred: want 995, got %v", tr.Preferred)
	}
	if tr.Max == nil || *tr.Max != 1000 {
		t.Errorf("max: want 1000, got %v", tr.Max)
	}
}

func TestResolveQualitySizesOverrideBeatsSet(t *testing.T) {
	got := ResolveQualitySizes(QualitySizeResolveInput{
		Source:    QSSourceGuide,
		AppType:   "sonarr",
		GuideSets: testGuideSets(),
		Overrides: map[string]QSOverride{
			"WEBDL-1080p": {Min: 7, Preferred: 40, Max: 60},
		},
	})
	if *got["WEBDL-1080p"].Min != 7 {
		t.Errorf("override min: want 7, got %v", *got["WEBDL-1080p"].Min)
	}
	// An override on one quality must not disturb its neighbours.
	if *got["WEBRip-1080p"].Min != 15 {
		t.Errorf("WEBRip-1080p min: want 15 from the set, got %v", *got["WEBRip-1080p"].Min)
	}
}

func TestResolveQualitySizesUserSetIsAppScoped(t *testing.T) {
	sets := []QualitySizeSet{
		{ID: "abc", Name: "Mine", AppType: "sonarr", Qualities: []QualitySizeEntry{
			{Quality: "WEBDL-1080p", Min: 3, Preferred: 4, Max: 5},
		}},
		{ID: "xyz", Name: "Radarr one", AppType: "radarr", Qualities: []QualitySizeEntry{
			{Quality: "WEBDL-1080p", Min: 99, Preferred: 99, Max: 99},
		}},
	}
	got := ResolveQualitySizes(QualitySizeResolveInput{
		Source: "custom:abc", AppType: "sonarr", UserSets: sets,
	})
	if *got["WEBDL-1080p"].Min != 3 {
		t.Errorf("want 3 from the sonarr set, got %v", *got["WEBDL-1080p"].Min)
	}
	// Asking a sonarr profile for a radarr set must find nothing rather than
	// write quality names that do not belong to the app.
	if ResolveQualitySizes(QualitySizeResolveInput{
		Source: "custom:xyz", AppType: "sonarr", UserSets: sets,
	}) != nil {
		t.Error("a radarr set must not resolve for a sonarr profile")
	}
}

// --- The gate, and the request body ---

func TestProfileQualitySizesGate(t *testing.T) {
	ad := &AppData{QualitySizes: testGuideSets()}
	req := SyncRequest{QualitySizeSource: QSSourceGuide}
	inst := Instance{Type: "sonarr"}

	// A definition list that still carries sizes means this instance keeps
	// them in one shared list, where the per-profile path has no business.
	if got := profileQualitySizes(req, ad, inst, sizeDefs(true, "WEBDL-1080p")); got != nil {
		t.Errorf("shared-list instance: want nil, got %v", got)
	}
	// An empty list means the read failed or the instance is in a state we do
	// not understand. Writing sizes on a guess is the worse mistake.
	if got := profileQualitySizes(req, ad, inst, nil); got != nil {
		t.Errorf("empty definition list: want nil, got %v", got)
	}
	// Sizes absent from the list means they live on the profiles.
	got := profileQualitySizes(req, ad, inst, sizeDefs(false, "WEBDL-1080p"))
	if got == nil || got["WEBDL-1080p"].Min == nil {
		t.Fatalf("per-profile instance: want sizes, got %v", got)
	}
}

func TestResolveQualityItemsWritesNoSizesWithNilMap(t *testing.T) {
	// The non-regression test, asserted on the request body rather than on
	// the outcome: "nothing happened" is exactly what a silent bug looks like.
	defs := sizeDefs(false, "WEBDL-1080p", "WEBRip-1080p")
	byName := map[string]*arr.ArrQualityDefinition{}
	for i := range defs {
		byName[defs[i].Quality.Name] = &defs[i]
	}
	f := func(v float64) *float64 { return &v }
	items := []QualityItem{
		// Sizes on the incoming item are deliberately ignored: the resolved
		// map is the only path to the body. Before this, an imported
		// profile's captured snapshot was written back on every sync.
		{Name: "WEBDL-1080p", Allowed: true, MinSize: f(42), MaxSize: f(43), PreferredSize: f(44)},
	}
	out, err := resolveQualityItems(items, byName, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"minSize", "maxSize", "preferredSize"} {
		if strings.Contains(string(body), field) {
			t.Errorf("body carries %s with a nil size map: %s", field, body)
		}
	}
}

func TestResolveQualityItemsAppliesSizesPerMemberNotPerGroup(t *testing.T) {
	// The guide's sets disagree inside groups: 19 groups across the guide
	// profiles hold members with different sizes, one holding 50.4 next to 15.
	// Taking the group row's value for everything under it would flatten them.
	defs := sizeDefs(false, "WEBDL-1080p", "WEBRip-1080p")
	byName := map[string]*arr.ArrQualityDefinition{}
	for i := range defs {
		byName[defs[i].Quality.Name] = &defs[i]
	}
	items := []QualityItem{
		{Name: "WEB 1080p", Allowed: true, Items: []string{"WEBDL-1080p", "WEBRip-1080p"}},
	}
	sizes := map[string]QualitySizeTriple{
		"WEBDL-1080p":  {Min: ptrF(50.4), Preferred: ptrF(995), Max: ptrF(1000)},
		"WEBRip-1080p": {Min: ptrF(15), Preferred: ptrF(995), Max: ptrF(1000)},
	}
	out, err := resolveQualityItems(items, byName, sizes)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || len(out[0].Items) != 2 {
		t.Fatalf("want one group with two members, got %+v", out)
	}
	if out[0].MinSize != nil || out[0].MaxSize != nil || out[0].PreferredSize != nil {
		t.Error("the group row must carry no size")
	}
	byMember := map[string]*float64{}
	for _, m := range out[0].Items {
		byMember[m.Quality.Name] = m.MinSize
	}
	if byMember["WEBDL-1080p"] == nil || *byMember["WEBDL-1080p"] != 50.4 {
		t.Errorf("WEBDL-1080p min: want 50.4, got %v", byMember["WEBDL-1080p"])
	}
	if byMember["WEBRip-1080p"] == nil || *byMember["WEBRip-1080p"] != 15 {
		t.Errorf("WEBRip-1080p min: want 15, got %v", byMember["WEBRip-1080p"])
	}
}

func TestResolveQualityItemsOmitsUncoveredQualities(t *testing.T) {
	// A set covering part of the profile is the normal case: the series set
	// covers 14 of a v5 profile's 22 qualities. The ones it leaves out must
	// stay out of the body so the instance keeps its own value.
	defs := sizeDefs(false, "WEBDL-1080p", "SDTV")
	byName := map[string]*arr.ArrQualityDefinition{}
	for i := range defs {
		byName[defs[i].Quality.Name] = &defs[i]
	}
	items := []QualityItem{
		{Name: "WEBDL-1080p", Allowed: true},
		{Name: "SDTV", Allowed: false},
	}
	sizes := map[string]QualitySizeTriple{
		"WEBDL-1080p": {Min: ptrF(15), Preferred: ptrF(995), Max: ptrF(1000)},
	}
	out, err := resolveQualityItems(items, byName, sizes)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range out {
		switch it.Quality.Name {
		case "WEBDL-1080p":
			if it.MinSize == nil || *it.MinSize != 15 {
				t.Errorf("WEBDL-1080p min: want 15, got %v", it.MinSize)
			}
		case "SDTV":
			if it.MinSize != nil || it.MaxSize != nil || it.PreferredSize != nil {
				t.Error("SDTV is not in the set, so it must carry no size")
			}
		}
	}
}

func TestApplyQualitySizeCopiesPointers(t *testing.T) {
	// One resolved map is applied to every profile in a Sync All run. Handing
	// out the same pointer would let one profile's later edit reach another's
	// request body.
	sizes := map[string]QualitySizeTriple{
		"WEBDL-1080p": {Min: ptrF(15), Preferred: ptrF(995), Max: ptrF(1000)},
	}
	var a, b arr.ArrQualityItem
	applyQualitySize(&a, sizes, "WEBDL-1080p")
	applyQualitySize(&b, sizes, "WEBDL-1080p")
	if a.MinSize == b.MinSize {
		t.Fatal("two items share one size pointer")
	}
	*a.MinSize = 99
	if *b.MinSize != 15 {
		t.Errorf("mutating one item changed the other: got %v, want 15", *b.MinSize)
	}
	if *sizes["WEBDL-1080p"].Min != 15 {
		t.Errorf("mutating an item changed the resolved map: got %v, want 15", *sizes["WEBDL-1080p"].Min)
	}
}

func ptrF(v float64) *float64 { return &v }

func TestConfigCloneIsolatesQualitySizeState(t *testing.T) {
	// The deep copy is where this feature already lost the size fields once:
	// cloneQualityItems copied the struct and shared the pointed-at values.
	// A set carries a slice and the rule overrides are a map, so both need
	// real copies. Assertions compare against literals, not against the
	// variable the original points at, which is what made an earlier version
	// of a test like this vacuous.
	cs := &ConfigStore{config: &Config{
		QualitySizeSets: []QualitySizeSet{
			{ID: "abc", Name: "Mine", AppType: "sonarr", Qualities: []QualitySizeEntry{
				{Quality: "WEBDL-1080p", Min: 15, Preferred: 995, Max: 1000},
			}},
		},
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:                "r1",
			QualitySizeSource: "guide:bef99584217af744e404ed44a33af589",
			QualitySizeOverrides: map[string]QSOverride{
				"WEBDL-1080p": {Min: 7, Preferred: 40, Max: 60},
			},
		}}},
	}}

	// Get returns a deep copy, which is what every reader of the config gets.
	clone := cs.Get()

	clone.QualitySizeSets[0].Qualities[0].Min = 99
	clone.QualitySizeSets[0].Name = "Changed"
	clone.AutoSync.Rules[0].QualitySizeOverrides["WEBDL-1080p"] = QSOverride{Min: 99}
	clone.AutoSync.Rules[0].QualitySizeSource = "off"

	if got := cs.config.QualitySizeSets[0].Qualities[0].Min; got != 15 {
		t.Errorf("set row min: want 15, got %v", got)
	}
	if got := cs.config.QualitySizeSets[0].Name; got != "Mine" {
		t.Errorf("set name: want Mine, got %q", got)
	}
	if got := cs.config.AutoSync.Rules[0].QualitySizeOverrides["WEBDL-1080p"].Min; got != 7 {
		t.Errorf("rule override min: want 7, got %v", got)
	}
	if got := cs.config.AutoSync.Rules[0].QualitySizeSource; got != "guide:bef99584217af744e404ed44a33af589" {
		t.Errorf("rule source: want the guide set, got %q", got)
	}
}

// --- The fixes a review found, each pinned so it cannot come back ---

func TestDiffQualitySizesDetectsASizeOnlyChange(t *testing.T) {
	// The bug this guards: nothing in the update path's "updated" computation
	// looked at sizes, so a size-only change produced no delta, the PUT was
	// never issued and the user was told "No changes". It appeared to work
	// only when the sync happened to change something else too.
	before := map[string]QualitySizeTriple{
		"WEBDL-1080p":  {Min: ptrF(4), Preferred: ptrF(95), Max: ptrF(130)},
		"WEBRip-1080p": {Min: ptrF(4), Preferred: ptrF(95), Max: ptrF(130)},
	}
	after := map[string]QualitySizeTriple{
		"WEBDL-1080p":  {Min: ptrF(15), Preferred: ptrF(995), Max: ptrF(1000)},
		"WEBRip-1080p": {Min: ptrF(4), Preferred: ptrF(95), Max: ptrF(130)},
	}
	got := diffQualitySizes(before, after)
	if len(got) != 1 {
		t.Fatalf("want one change, got %d: %v", len(got), got)
	}
	want := "WEBDL-1080p size: 4/95/130 → 15/995/1000"
	if got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
	if n := len(diffQualitySizes(before, before)); n != 0 {
		t.Errorf("identical snapshots must report nothing, got %d", n)
	}
}

func TestDiffQualitySizesTreatsNilAsItsOwnValue(t *testing.T) {
	// nil means the instance has no number there, which its own UI calls
	// Unlimited. Comparing nil as zero would miss the change from Unlimited
	// to a real cap, which is the one a user most wants to see.
	before := map[string]QualitySizeTriple{"SDTV": {Min: ptrF(2), Preferred: ptrF(95), Max: nil}}
	after := map[string]QualitySizeTriple{"SDTV": {Min: ptrF(2), Preferred: ptrF(95), Max: ptrF(100)}}
	got := diffQualitySizes(before, after)
	if len(got) != 1 || got[0] != "SDTV size: 2/95/unlimited → 2/95/100" {
		t.Errorf("got %v", got)
	}
	// And a quality only one side knows about is a structural change, which
	// the allowed and group comparisons already report.
	if n := len(diffQualitySizes(before, map[string]QualitySizeTriple{
		"SDTV":   {Min: ptrF(2), Preferred: ptrF(95), Max: nil},
		"Raw-HD": {Min: ptrF(1)},
	})); n != 0 {
		t.Errorf("a new quality must not count as a size change, got %d", n)
	}
}

func TestCollectQualitySizesSkipsGroupRows(t *testing.T) {
	size := func(v float64) *float64 { return &v }
	items := []arr.ArrQualityItem{
		{Quality: &arr.ArrQualityRef{Name: "WEBDL-1080p"}, MinSize: size(4)},
		{ID: 1000, Name: "WEB 720p", Items: []arr.ArrQualityItem{
			{Quality: &arr.ArrQualityRef{Name: "WEBRip-720p"}, MinSize: size(3)},
		}},
	}
	got := collectQualitySizes(items)
	if len(got) != 2 {
		t.Fatalf("want two leaves, got %d: %v", len(got), got)
	}
	if _, ok := got["WEB 720p"]; ok {
		t.Error("a group row carries no size and must not be compared")
	}
	if got["WEBRip-720p"].Min == nil || *got["WEBRip-720p"].Min != 3 {
		t.Errorf("group member: want 3, got %v", got["WEBRip-720p"].Min)
	}
}

func TestResolveQualitySizesOverridesSurviveAnOffSource(t *testing.T) {
	// Off means "take no set", not "write nothing". The resolution order
	// override-then-set-then-nothing only means something if an override
	// outlives a source that yields none, and it is what makes the useful
	// case possible: set two qualities by hand, leave the other twenty.
	for _, src := range []string{"", QSSourceOff} {
		got := ResolveQualitySizes(QualitySizeResolveInput{
			Source:    src,
			AppType:   "sonarr",
			GuideSets: testGuideSets(),
			Overrides: map[string]QSOverride{"SDTV": {Min: 1, Preferred: 20, Max: 30}},
		})
		if len(got) != 1 {
			t.Fatalf("source %q: want one entry, got %v", src, got)
		}
		if *got["SDTV"].Min != 1 {
			t.Errorf("source %q: want the override, got %v", src, *got["SDTV"].Min)
		}
	}
	// The upgrade guarantee is untouched: a rule from before this feature has
	// neither a source nor overrides, so it still writes nothing.
	if got := ResolveQualitySizes(QualitySizeResolveInput{AppType: "sonarr", GuideSets: testGuideSets()}); got != nil {
		t.Errorf("an untouched rule must write nothing, got %v", got)
	}
}

func TestFillQualitySizeIntentFallsBackToTheRule(t *testing.T) {
	// Six of seven apply entry points never copied these two fields onto the
	// request, so those syncs wrote no sizes while the rule said they should.
	// Reconciling it here means a new entry point cannot repeat that.
	rule := &AutoSyncRule{
		QualitySizeSource:    "guide:bef99584217af744e404ed44a33af589",
		QualitySizeOverrides: map[string]QSOverride{"SDTV": {Min: 1}},
	}
	sets := []QualitySizeSet{{ID: "abc", AppType: "sonarr"}}

	// A caller that sends nothing gets the rule's intent.
	req := SyncRequest{}
	FillQualitySizeIntent(&req, rule, sets)
	if req.QualitySizeSource != rule.QualitySizeSource {
		t.Errorf("source: got %q, want the rule's", req.QualitySizeSource)
	}
	if len(req.QualitySizeOverrides) != 1 {
		t.Errorf("overrides: want the rule's, got %v", req.QualitySizeOverrides)
	}
	if len(req.UserQualitySizeSets) != 1 {
		t.Error("saved sets must always be attached")
	}

	// The editor owns both fields when it names a source, so an empty
	// override map is honoured as "I removed the last one" rather than
	// silently replaced by the rule's.
	req2 := SyncRequest{QualitySizeSource: QSSourceOff, QualitySizeOverrides: map[string]QSOverride{}}
	FillQualitySizeIntent(&req2, rule, sets)
	if req2.QualitySizeSource != QSSourceOff {
		t.Errorf("an explicit off must win, got %q", req2.QualitySizeSource)
	}
	if len(req2.QualitySizeOverrides) != 0 {
		t.Errorf("an explicit empty map must win, got %v", req2.QualitySizeOverrides)
	}

	// No rule at all (an orphaned-history rerun) must not panic.
	req3 := SyncRequest{}
	FillQualitySizeIntent(&req3, nil, sets)
	if req3.QualitySizeSource != "" {
		t.Errorf("no rule: want no source, got %q", req3.QualitySizeSource)
	}
}

func TestDiffQualitySizesDoesNotInventClears(t *testing.T) {
	// Found by running a real sync: the chosen set covers part of the profile
	// by design, so the uncovered qualities come through with nil sizes.
	// preserveQualitySizes fills those from the instance, so nothing changes
	// there, but comparing the raw new value reported each of them as a change
	// to unlimited. Each phantom also marked the profile as needing a write,
	// which would have made every sync push an identical body forever.
	before := map[string]QualitySizeTriple{
		"Bluray-480p": {Min: ptrF(2), Preferred: ptrF(95), Max: ptrF(100)},
		"WEBDL-1080p": {Min: ptrF(4), Preferred: ptrF(95), Max: ptrF(130)},
	}
	after := map[string]QualitySizeTriple{
		// Not in the set: all three nil, meaning "say nothing".
		"Bluray-480p": {},
		"WEBDL-1080p": {Min: ptrF(15), Preferred: ptrF(995), Max: ptrF(1000)},
	}
	got := diffQualitySizes(before, after)
	if len(got) != 1 {
		t.Fatalf("want only the real change, got %d: %v", len(got), got)
	}
	if got[0] != "WEBDL-1080p size: 4/95/130 → 15/995/1000" {
		t.Errorf("got %q", got[0])
	}
	// And a partial triple keeps the fields it does not name.
	partial := map[string]QualitySizeTriple{"Bluray-480p": {Min: ptrF(9)}}
	got = diffQualitySizes(before, partial)
	if len(got) != 1 || got[0] != "Bluray-480p size: 2/95/100 → 9/95/100" {
		t.Errorf("partial triple: got %v", got)
	}
}

// History stores the source as an id, which is not enough to read back: a set
// the user deletes resolves to nothing afterwards. The name is therefore
// resolved when the sync runs, and an id that no longer matches anything says
// so rather than being printed raw.
func TestQualitySizeSourceLabel(t *testing.T) {
	guide := []*TrashQualitySize{
		{TrashID: "series-id", Type: "series"},
		{TrashID: "sqp-id", Type: "sqp-uhd"},
	}
	user := []QualitySizeSet{{ID: "mine", Name: "My sizes", AppType: "sonarr"}}

	cases := []struct {
		source string
		want   string
	}{
		{"", ""},
		{QSSourceOff, ""},
		{QSSourceSetPfx + "series-id", "Series"},
		{QSSourceSetPfx + "sqp-id", "SQP UHD"},
		{QSSourceCustomPfx + "mine", "My sizes"},
		{QSSourceCustomPfx + "deleted", "a set that no longer exists"},
		{QSSourceSetPfx + "dropped-upstream", "a set that no longer exists"},
	}
	for _, c := range cases {
		if got := QualitySizeSourceLabel(c.source, guide, user); got != c.want {
			t.Errorf("%q: got %q, want %q", c.source, got, c.want)
		}
	}
}

// The snapshot must hold the numbers, not a pointer, and must stay empty where
// nothing is written so history cannot claim sizes went to an instance that
// keeps them in the shared list.
func TestRecordSizesWrittenHoldsTheNumbers(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	res := &SyncResult{}
	in := map[string]QualitySizeTriple{
		"WEBDL-1080p": {Min: f(15), Preferred: f(995), Max: f(1000)},
		// A half-filled triple is never produced by a set or an override.
		// Recording half a value would be worse than recording none.
		"Raw-HD": {Min: f(4)},
	}
	if out := recordSizesWritten(res, in); len(out) != 2 {
		t.Errorf("the map handed on must be untouched, got %d entries", len(out))
	}
	if n := len(res.QualitySizesWritten); n != 1 {
		t.Fatalf("written = %d entries, want only the complete one: %+v", n, res.QualitySizesWritten)
	}
	got := res.QualitySizesWritten["WEBDL-1080p"]
	if got.Min != 15 || got.Preferred != 995 || got.Max != 1000 {
		t.Errorf("WEBDL-1080p = %+v, want 15/995/1000", got)
	}

	empty := &SyncResult{}
	recordSizesWritten(empty, nil)
	if empty.QualitySizesWritten != nil {
		t.Error("nothing written means nothing recorded")
	}
}
