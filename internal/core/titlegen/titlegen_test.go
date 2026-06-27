package titlegen

import (
	"strings"
	"testing"
)

func TestToken(t *testing.T) {
	cases := []struct {
		name string
		dim  Dimension
		want string
	}{
		// audio: drop parentheticals, spaces -> '.'
		{"DTS X", DimAudio, "DTS.X"},
		{"ATMOS (undefined)", DimAudio, "ATMOS"},
		{"DD+ ATMOS", DimAudio, "DD+.ATMOS"},
		{"TrueHD ATMOS", DimAudio, "TrueHD.ATMOS"},
		{"DTS-HD MA", DimAudio, "DTS-HD.MA"},
		// channels: just the layout number
		{"5.1 Surround", DimChannels, "5.1"},
		{"1.0 Mono", DimChannels, "1.0"},
		{"7.1 Surround", DimChannels, "7.1"},
		// hdr: drop "Boost" and parentheticals
		{"DV Boost", DimHDR, "DV"},
		{"HDR10+ Boost", DimHDR, "HDR10+"},
		{"SDR (no WEBDL)", DimHDR, "SDR"},
		{"DV (w/o HDR fallback)", DimHDR, "DV"},
		{"HDR", DimHDR, "HDR"},
		// resolution: as-is
		{"2160p", DimResolution, "2160p"},
	}
	for _, c := range cases {
		if got := Token(c.name, c.dim); got != c.want {
			t.Errorf("Token(%q, %s) = %q, want %q", c.name, c.dim, got, c.want)
		}
	}
}

func TestDimensionForGroup(t *testing.T) {
	cases := map[string]Dimension{
		"audio-formats":         DimAudio,
		"audio-channels":        DimChannels,
		"hdr-formats-hdr":       DimHDR,
		"hdr-formats-dv-boost":  DimHDR,
		"optional-resolutions":  DimResolution,
		"release-groups-hq":          DimGroup,
		"required-repack-proper":      DimModifier,
		"optional-streaming-services": DimService,
		"optional-unwanted":           DimUnwanted,
		"optional-miscellaneous":      "", // not a release dimension
	}
	for group, want := range cases {
		if got := DimensionForGroup(group); got != want {
			t.Errorf("DimensionForGroup(%q) = %q, want %q", group, got, want)
		}
	}
}

// A small audio-priority profile: distinct audio scores, two channel layouts,
// one resolution. Verify skip-rules + collapse + title shape.
func TestGenerate_AudioProfile(t *testing.T) {
	cfs := []CF{
		{Name: "TrueHD ATMOS", Score: 1000, Dim: DimAudio}, // object audio
		{Name: "DTS X", Score: 900, Dim: DimAudio},         // object audio
		{Name: "TrueHD", Score: 500, Dim: DimAudio},        // lossless
		{Name: "DD+", Score: 100, Dim: DimAudio},           // lossy
		{Name: "5.1 Surround", Score: 50, Dim: DimChannels},
		{Name: "2.0 Stereo", Score: 20, Dim: DimChannels},
		{Name: "2160p", Score: 0, Dim: DimResolution},
	}
	titles := Generate(cfs, Options{Sources: []string{"WEB-DL"}})
	if len(titles) == 0 {
		t.Fatal("expected titles, got none")
	}
	has := func(sub string) bool {
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				return true
			}
		}
		return false
	}
	// Object audio at 5.1 must exist...
	if !has("TrueHD.ATMOS.5.1") {
		t.Error("expected an object-audio + 5.1 title")
	}
	// ...but object audio at 2.0 must be skipped (impossible).
	if has("TrueHD.ATMOS.2.0") || has("DTS.X.2.0") {
		t.Error("object audio at stereo should be skipped")
	}
	// Lossless stereo (TrueHD at 2.0) skipped by default (rare-but-real off).
	if has("Movie.2026.2160p.WEB-DL.TrueHD.2.0") {
		t.Error("lossless stereo should be skipped unless allowed")
	}
	// Title shape: Movie.2026.<res>.<source>.<audio>...
	for _, ti := range titles {
		if !strings.HasPrefix(ti, "Movie.2026.2160p.WEB-DL.") {
			t.Errorf("unexpected title shape: %q", ti)
		}
	}
	t.Logf("generated %d titles, e.g. %q", len(titles), titles[0])
}

// Service and modifier are pick-one-or-none; IncludeUnwanted appends one title
// per Unwanted decoration on a single group-less base release.
func TestGenerate_ServiceModifierUnwanted(t *testing.T) {
	cfs := []CF{
		{Name: "5.1 Surround", Score: 50, Dim: DimChannels},
		{Name: "2160p", Score: 0, Dim: DimResolution},
		{Name: "AMZN", Score: 25, Dim: DimService},
		{Name: "Repack/Proper", Score: 5, Dim: DimModifier},
	}
	unwanted := []Unwanted{
		{Name: "AV1", Codec: "AV1"},
		{Name: "3D", Suffix: ".3D"},
		{Name: "LQ", Suffix: "-YIFY"},
		{Name: "No-RlsGroup", NoGroup: true},
		{Name: "BR-DISK", Body: "COMPLETE.BLURAY.AVC"},
	}
	has := func(titles []string, sub string) bool {
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				return true
			}
		}
		return false
	}

	// Off: no unwanted tokens; service + modifier still appear (WEB-DL source).
	off := Generate(cfs, Options{Sources: []string{"WEB-DL"}, Unwanted: unwanted})
	if !has(off, "AMZN") {
		t.Error("expected a streaming-service title")
	}
	if !has(off, "Repack") {
		t.Error("expected a repack/proper title")
	}
	if has(off, "-YIFY") || has(off, "AV1") || has(off, "COMPLETE.BLURAY") {
		t.Error("unwanted tokens must not appear when IncludeUnwanted is off")
	}

	// Service only on WEB-DL, never Bluray.
	bd := Generate(cfs, Options{Sources: []string{"Bluray"}})
	if has(bd, "AMZN") {
		t.Error("streaming service must not appear on Bluray")
	}

	// On: bad-group names, AV1 and suffix unwanted tags (3D) are product axes
	// (they multiply across the grid); only body-override unwanted (BR-DISK,
	// a whole-disc release) stays one-each.
	on := Generate(cfs, Options{Sources: []string{"WEB-DL"}, IncludeUnwanted: true, Unwanted: unwanted})
	count := func(titles []string, sub string) int {
		n := 0
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				n++
			}
		}
		return n
	}
	if !has(on, "-YIFY") {
		t.Error("expected release-group unwanted titles (-YIFY)")
	}
	// AV1 replaces the codec rather than sitting next to h265.
	if !has(on, "AV1") {
		t.Error("expected AV1 unwanted titles")
	}
	for _, ti := range on {
		if strings.Contains(ti, "AV1") && strings.Contains(ti, "h265") {
			t.Errorf("AV1 title should not also carry h265: %q", ti)
		}
	}
	// Bad group and codec are product axes: they combine with service/modifier,
	// so each shows up in more than one title.
	if c := count(on, "-YIFY"); c < 2 {
		t.Errorf("bad-group unwanted should be a product axis (multiple titles), got %d", c)
	}
	if c := count(on, "AV1"); c < 2 {
		t.Errorf("AV1 codec should be a product axis (multiple titles), got %d", c)
	}
	// Suffix unwanted tags are a product axis now: 3D combines with service /
	// modifier / etc., so it appears in more than one title (Calculator needs
	// every unwanted in combination, not just on one sample release).
	if c := count(on, ".3D"); c < 2 {
		t.Errorf("3D suffix tag should be a product axis (multiple titles), got %d", c)
	}
	// Body-override unwanted is the exception: a whole-disc release can't carry
	// the other axes, so BR-DISK stays one-each.
	if c := count(on, "COMPLETE.BLURAY.AVC"); c != 1 {
		t.Errorf("BR-DISK should be one-each, got %d", c)
	}
}

// AAC / DD (AC3) / DTS core exist at 1.0 / 2.0 / 5.1 but never 6.1 / 7.1.
func TestSkip_AudioChannelCaps(t *testing.T) {
	cases := []struct {
		audio, ch string
		skipped   bool
	}{
		{"DD", "5.1", false}, {"DD", "7.1", true},
		{"AAC", "2.0", false}, {"AAC", "7.1", true},
		{"DTS", "1.0", false}, {"DTS", "6.1", true},
		{"DTS-ES", "6.1", false}, // 6.1 is DTS-ES's native layout
		{"TrueHD", "7.1", false}, // object/lossless: high channels fine
	}
	for _, c := range cases {
		got := skip(combo{resolution: "2160p", source: "WEB-DL", audio: c.audio, channels: c.ch, vcodec: "h265"}, Options{})
		if got != c.skipped {
			t.Errorf("skip(audio=%s, ch=%s) = %v, want %v", c.audio, c.ch, got, c.skipped)
		}
	}
}

// Remux is a disc stream copy: codec fixed by resolution, HDR only at 2160p,
// never AV1 or a lossy track. Confirmed against real release data.
func TestSkip_RemuxAndAV1(t *testing.T) {
	cases := []struct {
		name    string
		c       combo
		skipped bool
	}{
		{"2160p remux HEVC ok", combo{resolution: "2160p", source: "Remux", vcodec: "h265", audio: "TrueHD", channels: "7.1"}, false},
		{"2160p remux h264 no", combo{resolution: "2160p", source: "Remux", vcodec: "h264", audio: "TrueHD", channels: "7.1"}, true},
		{"1080p remux AVC ok", combo{resolution: "1080p", source: "Remux", vcodec: "h264", audio: "DTS-HD.MA", channels: "5.1"}, false},
		{"1080p remux h265 no", combo{resolution: "1080p", source: "Remux", vcodec: "h265", audio: "DTS-HD.MA", channels: "5.1"}, true},
		{"1080p remux HDR no", combo{resolution: "1080p", source: "Remux", vcodec: "h264", hdr: "HDR10", audio: "DTS-HD.MA", channels: "5.1"}, true},
		{"remux lossy audio no", combo{resolution: "1080p", source: "Remux", vcodec: "h264", audio: "DD", channels: "5.1"}, true},
		{"AV1 remux no", combo{resolution: "2160p", source: "Remux", vcodec: "AV1", audio: "TrueHD", channels: "7.1"}, true},
		{"AV1 bluray ok", combo{resolution: "2160p", source: "Bluray", vcodec: "AV1", hdr: "HDR10", audio: "TrueHD", channels: "7.1"}, false},
		{"AV1 webdl 1080p ok", combo{resolution: "1080p", source: "WEB-DL", vcodec: "AV1", audio: "DD+", channels: "5.1"}, false},
	}
	for _, c := range cases {
		if got := skip(c.c, Options{}); got != c.skipped {
			t.Errorf("%s: skip = %v, want %v", c.name, got, c.skipped)
		}
	}
}

// Movie-versions group classifies to DimEdition, and each scored CF maps to a
// family + release token (within-family pick-one, families combine).
func TestEditionClassification(t *testing.T) {
	if d := DimensionForGroup("optional-movie-versions"); d != DimEdition {
		t.Errorf("movie-versions group = %q, want DimEdition", d)
	}
	cases := []struct {
		name  string
		fam   editionFamily
		token string
		ok    bool
	}{
		{"IMAX", famFraming, "IMAX", true},
		{"Open Matte", famFraming, "Open.Matte", true},
		{"Theatrical Cut", famCut, "Theatrical.Cut", true},
		{"Special Edition", famCut, "Special.Edition", true},
		{"Criterion Collection", famRestoration, "Criterion", true},
		{"4K Remaster", famRestoration, "4K.Remaster", true},
		{"Some Unknown Edition", "", "", false},
	}
	for _, c := range cases {
		fam, tok, ok := editionInfo(c.name)
		if ok != c.ok || (ok && (fam != c.fam || tok != c.token)) {
			t.Errorf("editionInfo(%q) = (%q,%q,%v), want (%q,%q,%v)", c.name, fam, tok, ok, c.fam, c.token, c.ok)
		}
	}
}

// Editions are optional sub-axes: no edition CFs adds nothing; with them the
// product grows, Criterion stays disc-only, and Criterion never meets IMAX.
func TestGenerate_Editions(t *testing.T) {
	base := []CF{{Name: "5.1 Surround", Score: 0, Dim: DimChannels}}
	o := Options{Qualities: []QualityCtx{{Source: "Bluray", Resolution: "2160p"}, {Source: "WEB-DL", Resolution: "2160p"}}}
	contains := func(titles []string, sub string) bool {
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				return true
			}
		}
		return false
	}

	none := Generate(base, o)
	if contains(none, "IMAX") || contains(none, "Criterion") {
		t.Error("no edition CFs, but a title carries an edition tag")
	}

	withEd := append(append([]CF{}, base...),
		CF{Name: "IMAX", Score: 25, Dim: DimEdition},
		CF{Name: "Criterion Collection", Score: 25, Dim: DimEdition},
	)
	on := Generate(withEd, o)
	if !contains(on, ".IMAX.") {
		t.Error("expected IMAX titles")
	}
	if !contains(on, "Criterion") {
		t.Error("expected Criterion titles")
	}
	if len(on) <= len(none) {
		t.Errorf("editions should add titles: none=%d on=%d", len(none), len(on))
	}
	for _, ti := range on {
		if strings.Contains(ti, "Criterion") && strings.Contains(ti, "WEB-DL") {
			t.Errorf("Criterion must be disc-only, got WEB-DL: %q", ti)
		}
		if strings.Contains(ti, "IMAX") && strings.Contains(ti, "Criterion") {
			t.Errorf("Criterion + IMAX must never co-occur: %q", ti)
		}
	}
}

// Quality items drive the source x resolution grid and expand into tier
// placeholders where the profile has tier CFs for that source.
func TestGenerate_QualityContextsWithTiers(t *testing.T) {
	cfs := []CF{
		{Name: "HD Bluray Tier 01", Score: 100, Dim: DimGroup},
		{Name: "HD Bluray Tier 02", Score: 90, Dim: DimGroup},
		{Name: "5.1 Surround", Score: 0, Dim: DimChannels},
	}
	o := Options{Qualities: []QualityCtx{
		{Source: "Bluray", Resolution: "1080p"},
		{Source: "HDTV", Resolution: "720p"}, // no tiers -> single context
	}}
	titles := Generate(cfs, o)
	has := func(sub string) bool {
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				return true
			}
		}
		return false
	}
	if !has("1080p.Bluray") {
		t.Error("expected a Bluray-1080p title from the quality grid")
	}
	if !has("-Tier1") || !has("-Tier2") || !has("-NoTier") {
		t.Error("expected NoTier + Tier1 + Tier2 placeholders for HD Bluray")
	}
	if !has("720p.HDTV") {
		t.Error("expected an HDTV-720p title")
	}
	// HDTV has no tier CFs, so its titles must not carry tier placeholders.
	for _, ti := range titles {
		if strings.Contains(ti, "HDTV") && strings.Contains(ti, "Tier") {
			t.Errorf("HDTV title should have no tier placeholder: %q", ti)
		}
	}
}

// The codec axis generates both h265 and h264, with the skip rules pruning the
// impossible/rare codec combos (h264+HDR, 2160p+h264).
// A score-0 CF must not create a generation axis: it would only produce titles
// that score identically with or without its token (pure noise). A non-zero CF
// on the same axis still generates.
func TestGenerate_SkipsZeroScoreDecorations(t *testing.T) {
	o := Options{Qualities: []QualityCtx{{Source: "WEB-DL", Resolution: "1080p"}}}
	cfs := []CF{
		{Name: "AMZN", Score: 0, Dim: DimService},  // score 0 -> must not generate
		{Name: "ATVP", Score: 50, Dim: DimService}, // scored -> must generate
	}
	titles := Generate(cfs, o)
	hasAMZN, hasATVP := false, false
	for _, ti := range titles {
		if strings.Contains(ti, "AMZN") {
			hasAMZN = true
		}
		if strings.Contains(ti, "ATVP") {
			hasATVP = true
		}
	}
	if hasAMZN {
		t.Error("score-0 service AMZN should not be generated")
	}
	if !hasATVP {
		t.Error("scored service ATVP should be generated")
	}
}

func TestGenerate_CodecAxis(t *testing.T) {
	cfs := []CF{{Name: "5.1 Surround", Score: 5, Dim: DimChannels}}
	o := Options{
		Qualities: []QualityCtx{{Source: "WEB-DL", Resolution: "1080p"}, {Source: "WEB-DL", Resolution: "2160p"}},
		VCodecs:   []string{"h265", "h264"},
	}
	titles := Generate(cfs, o)
	has := func(sub string) bool {
		for _, ti := range titles {
			if strings.Contains(ti, sub) {
				return true
			}
		}
		return false
	}
	if !has("1080p.WEB-DL.5.1.h264") {
		t.Error("expected an h264 variant at 1080p")
	}
	if !has("1080p.WEB-DL.5.1.h265") {
		t.Error("expected an h265 variant at 1080p")
	}
	if !has("2160p.WEB-DL.5.1.h265") {
		t.Error("expected an h265 variant at 2160p")
	}
	for _, ti := range titles {
		if strings.Contains(ti, "2160p") && strings.Contains(ti, "h264") {
			t.Errorf("2160p + h264 should be skipped by default: %q", ti)
		}
	}
}

// Identical-score audio formats collapse to one representative.
// Every distinct audio token generates, even when CFs share a score - the user
// picks which CFs to generate for, so none are collapsed away by score.
func TestGenerate_DistinctTokensNotCollapsed(t *testing.T) {
	cfs := []CF{
		{Name: "TrueHD", Score: 500, Dim: DimAudio},
		{Name: "DTS-HD MA", Score: 500, Dim: DimAudio},
		{Name: "FLAC", Score: 500, Dim: DimAudio},
		{Name: "5.1 Surround", Score: 0, Dim: DimChannels},
		{Name: "2160p", Score: 0, Dim: DimResolution},
	}
	titles := Generate(cfs, Options{})
	for _, name := range []string{"TrueHD", "DTS-HD.MA", "FLAC"} {
		found := false
		for _, ti := range titles {
			if strings.Contains(ti, name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a title with audio %q (distinct tokens must not be collapsed by score)", name)
		}
	}
}
