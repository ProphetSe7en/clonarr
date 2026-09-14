package arr

import (
	"encoding/json"
	"math"
	"testing"
)

// =============================================================================
// QualitySizeLimitsFor
// =============================================================================

func TestQualitySizeLimitsFor_Radarr(t *testing.T) {
	lim := QualitySizeLimitsFor("radarr")
	if lim.Max != 2000 || lim.Preferred != 1999 {
		t.Errorf("radarr limits = %+v, want {Max:2000 Preferred:1999}", lim)
	}
}

func TestQualitySizeLimitsFor_Sonarr(t *testing.T) {
	lim := QualitySizeLimitsFor("sonarr")
	if lim.Max != 1000 || lim.Preferred != 995 {
		t.Errorf("sonarr limits = %+v, want {Max:1000 Preferred:995}", lim)
	}
}

func TestQualitySizeLimitsFor_CaseInsensitive(t *testing.T) {
	// Instance types are lower-cased in config, but callers should not have to
	// care — a mixed-case type must not silently fall through to Radarr.
	lim := QualitySizeLimitsFor("Sonarr")
	if lim.Max != 1000 {
		t.Errorf("\"Sonarr\" resolved to Max %v, want 1000", lim.Max)
	}
}

func TestQualitySizeLimitsFor_UnknownDefaultsToRadarr(t *testing.T) {
	lim := QualitySizeLimitsFor("")
	if lim.Max != 2000 || lim.Preferred != 1999 {
		t.Errorf("empty type = %+v, want the Radarr limits", lim)
	}
}

// =============================================================================
// SizeOrLimit
// =============================================================================

func TestSizeOrLimit_NilIsTheLimit(t *testing.T) {
	// nil is how the API reports "Unlimited". Resolving it to 0 (what FloatVal
	// does) is the bug this function exists to prevent.
	if got := SizeOrLimit(nil, 2000); got != 2000 {
		t.Errorf("SizeOrLimit(nil, 2000) = %v, want 2000", got)
	}
	if got := FloatVal(nil); got != 0 {
		t.Errorf("FloatVal(nil) = %v, want 0 — SizeOrLimit must not share this behaviour", got)
	}
}

func TestSizeOrLimit_ValuePassesThrough(t *testing.T) {
	for _, v := range []float64{0, 17.1, 100, 1999} {
		if got := SizeOrLimit(FloatPtr(v), 2000); got != v {
			t.Errorf("SizeOrLimit(%v, 2000) = %v, want %v", v, got, v)
		}
	}
}

// =============================================================================
// SizePtr / NormalizeSize
// =============================================================================

func TestSizePtr_AtOrAboveLimitIsNil(t *testing.T) {
	// The instance stores a value at its limit as null. Writing the number
	// works, but it reads back as null and the quality then looks drifted on
	// every following comparison.
	for _, v := range []float64{2000, 2500} {
		if got := SizePtr(v, 2000); got != nil {
			t.Errorf("SizePtr(%v, 2000) = %v, want nil", v, *got)
		}
	}
	if got := SizePtr(1999, 1999); got != nil {
		t.Errorf("SizePtr(1999, 1999) = %v, want nil", *got)
	}
}

func TestSizePtr_BelowLimitKeepsValue(t *testing.T) {
	got := SizePtr(95, 1999)
	if got == nil {
		t.Fatal("SizePtr(95, 1999) = nil, want a pointer to 95")
	}
	if *got != 95 {
		t.Errorf("SizePtr(95, 1999) = %v, want 95", *got)
	}
}

func TestNormalizeSize_NilStaysNil(t *testing.T) {
	// A definition arriving from the UI may already carry null. Collapsing it
	// through FloatVal first would turn Unlimited into an explicit 0.
	if got := NormalizeSize(nil, 1999); got != nil {
		t.Errorf("NormalizeSize(nil, 1999) = %v, want nil", *got)
	}
}

func TestNormalizeSize_CollapsesLimit(t *testing.T) {
	if got := NormalizeSize(FloatPtr(2000), 2000); got != nil {
		t.Errorf("NormalizeSize(2000, 2000) = %v, want nil", *got)
	}
	got := NormalizeSize(FloatPtr(100), 2000)
	if got == nil {
		t.Fatal("NormalizeSize(100, 2000) = nil, want 100")
	}
	if *got != 100 {
		t.Errorf("NormalizeSize(100, 2000) = %v, want 100", *got)
	}
}

// =============================================================================
// Regression: an Unlimited quality must not report drift against TRaSH
// =============================================================================

// qsDrifts mirrors the comparison in buildQualitySizeDefs / autoSyncQualitySizes.
func qsDrifts(def ArrQualityDefinition, min, preferred, max float64, lim QualitySizeLimits) bool {
	return math.Abs(FloatVal(def.MinSize)-min) >= 0.05 ||
		math.Abs(SizeOrLimit(def.PreferredSize, lim.Preferred)-preferred) >= 0.05 ||
		math.Abs(SizeOrLimit(def.MaxSize, lim.Max)-max) >= 0.05
}

func TestQualityDefinition_UnlimitedMatchesTrash(t *testing.T) {
	// Verbatim /api/v3/qualitydefinition entry from a Radarr instance whose
	// max and preferred sliders sit at Unlimited: both fields are absent, not
	// null. TRaSH ships min 17.1 / preferred 1999 / max 2000 for this quality,
	// so the instance already matches and must not be queued for sync.
	const payload = `{
		"quality": {"id": 4, "name": "HDTV-720p", "source": "tv", "resolution": 720, "modifier": "none"},
		"title": "HDTV-720p",
		"weight": 14,
		"minSize": 17.1,
		"id": 15
	}`

	var def ArrQualityDefinition
	if err := json.Unmarshal([]byte(payload), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.MaxSize != nil || def.PreferredSize != nil {
		t.Fatalf("expected nil max/preferred, got %v/%v", def.MaxSize, def.PreferredSize)
	}

	lim := QualitySizeLimitsFor("radarr")
	if qsDrifts(def, 17.1, 1999, 2000, lim) {
		t.Error("Unlimited quality reported as drifted from TRaSH; it already matches")
	}
}

func TestQualityDefinition_ExplicitSizesStillCompare(t *testing.T) {
	// A quality with all three sizes present must keep comparing normally —
	// the nil handling must not mask a real difference.
	const payload = `{
		"quality": {"id": 8, "name": "WEBDL-480p", "source": "webdl", "resolution": 480, "modifier": "none"},
		"title": "WEBDL-480p",
		"weight": 11,
		"minSize": 0,
		"maxSize": 100,
		"preferredSize": 95,
		"id": 11
	}`

	var def ArrQualityDefinition
	if err := json.Unmarshal([]byte(payload), &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	lim := QualitySizeLimitsFor("radarr")
	if qsDrifts(def, 0, 95, 100, lim) {
		t.Error("matching explicit sizes reported as drifted")
	}
	if !qsDrifts(def, 0, 1999, 2000, lim) {
		t.Error("explicit 95/100 must not be treated as Unlimited")
	}
}

// =============================================================================
// Round trip: what we write must read back as a match
// =============================================================================

func TestQualitySize_WriteThenCompareIsStable(t *testing.T) {
	// Sync writes the TRaSH targets, the instance reports them back, and the
	// next comparison must be clean. Before the limit handling this loop never
	// settled: 1999/2000 went out, null came back, drift was reported again.
	lim := QualitySizeLimitsFor("radarr")
	written := ArrQualityDefinition{
		MinSize:       FloatPtr(17.1),
		PreferredSize: SizePtr(1999, lim.Preferred),
		MaxSize:       SizePtr(2000, lim.Max),
	}
	if written.PreferredSize != nil || written.MaxSize != nil {
		t.Fatal("expected the limits to be written as null")
	}

	// Simulate the instance's response: absent fields unmarshal to nil.
	body, err := json.Marshal(written)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var readBack ArrQualityDefinition
	if err := json.Unmarshal(body, &readBack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if qsDrifts(readBack, 17.1, 1999, 2000, lim) {
		t.Error("a freshly synced quality still reports drift")
	}
}
