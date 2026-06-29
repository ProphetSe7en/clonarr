// Package titlegen turns a profile's Custom Formats into candidate release
// titles, so the Scoring Sandbox and the Scoring Generator don't need pasted
// titles. It derives a release-name token + dimension per CF (the dimension
// comes from the CF's TRaSH group; see DimensionForGroup), takes the Cartesian
// product across dimensions, prunes physically-impossible combinations, and
// collapses CFs that score identically to a single representative.
//
// We do NOT evaluate TRaSH regexes here (they use lookahead/lookbehind that Go's
// RE2 can't run). The derived tokens are CANDIDATES; the source of truth is the
// round-trip: build a title, parse it through Arr, and confirm the intended CFs
// matched. Token normalisation below gets the common cases right so that
// round-trip mostly confirms rather than corrects.
package titlegen

import (
	"fmt"
	"regexp"
	"strings"
)

// Dimension is a release-name axis. A release has exactly one value per
// dimension, so the generator picks one token per dimension per title.
type Dimension string

const (
	DimAudio      Dimension = "audio"
	DimChannels   Dimension = "channels"
	DimHDR        Dimension = "hdr"
	DimResolution Dimension = "resolution"
	DimSource     Dimension = "source"
	DimGroup      Dimension = "group"    // release-group tier; emitted as Tier placeholders
	DimService    Dimension = "service"  // streaming service tag (AMZN, NF, ...); WEB-DL only
	DimModifier   Dimension = "modifier" // repack/proper
	DimUnwanted   Dimension = "unwanted" // penalty CFs; only used when IncludeUnwanted
	DimEdition    Dimension = "edition"  // movie versions (IMAX, Criterion, ...); optional sub-axes
)

// DimensionForGroup maps a TRaSH cf-group (by its file/name prefix) to a release
// dimension. Empty means the group isn't a single-token release dimension
// (penalty/misc groups), so its CFs aren't generatable. clonarr already knows
// each CF's group membership, so this is the whole dimension-classification job.
func DimensionForGroup(group string) Dimension {
	g := strings.ToLower(group)
	switch {
	case strings.Contains(g, "audio-channels") || strings.Contains(g, "audio channels"):
		return DimChannels
	case strings.Contains(g, "audio-formats") || strings.Contains(g, "audio formats"):
		return DimAudio
	case strings.Contains(g, "hdr"):
		return DimHDR
	case strings.Contains(g, "resolution"):
		return DimResolution
	case strings.Contains(g, "release-group") || strings.Contains(g, "release group"):
		return DimGroup
	case strings.Contains(g, "streaming service") || strings.Contains(g, "streaming-service"):
		return DimService
	case strings.Contains(g, "repack") || strings.Contains(g, "proper"):
		return DimModifier
	case strings.Contains(g, "movie-versions") || strings.Contains(g, "movie versions"):
		return DimEdition
	case strings.Contains(g, "unwanted"):
		return DimUnwanted
	}
	return ""
}

// editionFamily groups the movie-versions CFs. Within a family a release picks
// one value (IMAX vs Open Matte are mutually exclusive framings); across
// families they combine (IMAX + Special Edition is real). Verified against
// Criterion + IMAX search corpora (2026-06-23).
type editionFamily string

const (
	famFraming     editionFamily = "framing"     // how the image is framed
	famCut         editionFamily = "cut"         // which cut/version
	famRestoration editionFamily = "restoration" // restoration / collection / source
)

// editionInfo maps a movie-versions CF name to its family + release-name token.
// Curated (like the HDR axis) because the title token differs from the CF name
// and the family structure is not derivable from the name. "IMAX Enhanced" is
// intentionally absent: it is the same "IMAX" release token, assigned by source
// at parse time (DSNP / Sony-stream), not a separate generatable token.
func editionInfo(cfName string) (fam editionFamily, token string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(cfName)) {
	case "imax":
		return famFraming, "IMAX", true
	case "open matte":
		return famFraming, "Open.Matte", true
	case "theatrical cut":
		return famCut, "Theatrical.Cut", true
	case "special edition":
		return famCut, "Special.Edition", true
	case "criterion collection":
		return famRestoration, "Criterion", true
	case "remaster":
		return famRestoration, "REMASTERED", true
	case "4k remaster":
		return famRestoration, "4K.Remaster", true
	case "hybrid":
		return famRestoration, "Hybrid", true
	}
	return "", "", false
}

var (
	reParen   = regexp.MustCompile(`\s*\([^)]*\)`)
	reSpaces  = regexp.MustCompile(`\s+`)
	reChannel = regexp.MustCompile(`\d+\.\d+`)
)

// Token derives a candidate release-name token from a CF name, normalised per
// dimension. Examples:
//
//	audio:      "DTS X" -> "DTS.X", "ATMOS (undefined)" -> "ATMOS"
//	channels:   "5.1 Surround" -> "5.1", "1.0 Mono" -> "1.0"
//	hdr:        "DV Boost" -> "DV", "HDR10+ Boost" -> "HDR10+", "SDR (no WEBDL)" -> "SDR"
//	resolution: "2160p" -> "2160p"
func Token(name string, dim Dimension) string {
	n := strings.TrimSpace(reParen.ReplaceAllString(name, ""))
	switch dim {
	case DimChannels:
		// The channel layout number is the token; drop "Surround/Mono/Stereo/Sound".
		if m := reChannel.FindString(n); m != "" {
			return m
		}
	case DimHDR:
		// "Boost" marks a scoring nuance, not a release token.
		n = strings.TrimSpace(strings.TrimSuffix(n, "Boost"))
	case DimModifier:
		// "Repack/Proper" -> "Repack"; "Repack2"/"Repack3" pass through.
		if i := strings.IndexByte(n, '/'); i >= 0 {
			n = strings.TrimSpace(n[:i])
		}
	}
	return reSpaces.ReplaceAllString(n, ".")
}

// objectAudio tokens require >= 5.1 channels - object audio at stereo is
// impossible. Plain TrueHD is NOT here: real data has TrueHD 2.0 (old films), so
// only the Atmos/DTS-X object formats are channel-gated.
var objectAudio = map[string]bool{
	"TrueHD.ATMOS": true, "DD+.ATMOS": true, "ATMOS": true, "DTS.X": true,
}

var stereoOrLess = map[string]bool{
	"1.0": true, "1.1": true, "2.0": true, "2.1": true, "3.0": true, "3.1": true, "4.0": true, "4.1": true,
}

// maxFiveOneAudio are audio formats that never carry more than 5.1 channels:
// AC3 (DD) is spec-capped at 5.1, AAC and DTS core likewise in practice. They
// still appear at 1.0 / 2.0 / 5.1; only 6.1 / 7.1 are impossible for them.
var maxFiveOneAudio = map[string]bool{"DD": true, "AAC": true, "DTS": true}

// aboveFiveOne are channel layouts beyond 5.1.
var aboveFiveOne = map[string]bool{"6.1": true, "7.1": true, "9.1": true}

// lossyAudio are compressed tracks a Remux would never carry (Remux preserves
// the disc's lossless master). Conservative set of unambiguous tokens.
var lossyAudio = map[string]bool{
	"DD": true, "DD+": true, "AAC": true, "DTS": true, "DTS-ES": true, "DTS-HD.HRA": true,
}

// CF is a profile Custom Format the generator can express, with its score (for
// identical-score collapse) and dimension.
type CF struct {
	Name    string
	TrashID string
	Score   int
	Dim     Dimension
	// Groups holds explicit release-group name literals this CF matches (e.g.
	// "TheFarm", "MainFrame"), for a non-tier release-group CF. Each becomes a
	// group-axis value so a release carrying that group is generated and the CF
	// gets scored. Empty for tier CFs (handled by tier placeholders) and every
	// non-group dimension.
	Groups []string
}

// QualityCtx is a source + resolution the caller derived from the profile's
// allowed quality items (or its cutoff). When Options.Qualities is set it drives
// the source x resolution grid instead of the tier/resolution-CF fallback.
type QualityCtx struct {
	Source     string // release token: "Bluray","WEB-DL","WEBRip","Remux","HDTV"
	Resolution string // "2160p","1080p",...
}

// Unwanted is one penalty to apply to a good base release, so the profile is
// shown rejecting it. The caller derives these from each default-on unwanted
// CF's specifications (a release-group name, a title tag, or the absence of a
// group). Exactly one field drives the shape.
type Unwanted struct {
	Name    string // CF name (label / dedup)
	Suffix  string // appended to the base good title, e.g. "-YIFY", " [rartv]", ".3D"
	NoGroup bool   // emit the base with no release-group suffix (No-RlsGroup)
	Codec   string // replace the codec (e.g. "AV1") instead of appending
	Body    string // full body override after Title.Year (e.g. BR-DISK "COMPLETE.BLURAY.AVC")
}

// Options control the base name and the rare-but-real skip toggles.
type Options struct {
	Title   string // default "Movie"
	Year    string // default "2026"
	VCodecs []string // codec axis; default ["h265"]. Add "h264" only when a codec
	// CF (x265, x265 (HD), x264, ...) scores them differently, else they'd
	// collapse to one. The skip rules prune the impossible codec combos.
	Sources   []string
	Qualities []QualityCtx // profile's allowed quality items / cutoff (preferred grid)

	Allow1080pHDR       bool // 1080p with HDR/DV (rare-but-real)
	Allow2160pX264      bool // 2160p with h264 (rare-but-real)
	AllowLosslessStereo bool // lossless audio at 1.0/2.0 (rare-but-real)
	AllowLossyRemux     bool // Remux with a lossy track (rare-but-real)

	// IncludeUnwanted appends the Unwanted penalty titles (one per entry), so
	// you can see the profile reject them. Off for the Sandbox landscape by
	// default; on for the Calculator (negatives anchor the scores).
	IncludeUnwanted bool
	Unwanted        []Unwanted
}

// combo is one picked value per dimension. infix is an extra mid-title token
// (used for inline unwanted tags) placed just before the codec. edition is the
// optional movie-version tag(s) (e.g. "IMAX", "Criterion", "IMAX.Special.Edition"),
// placed right after the year.
type combo struct{ resolution, source, audio, channels, hdr, service, modifier, infix, vcodec, edition string }

// editionCombos builds the movie-version tag combinations from the scored
// edition CFs. Within a family pick-one (so each combo has at most one framing,
// one cut, one restoration); across families combine. Always includes the
// empty "" (no edition), so a profile with no edition CFs adds no titles.
// Criterion + IMAX is excluded (verified: never co-occurs).
func editionCombos(editionCFs []CF) []string {
	byFam := map[editionFamily][]string{}
	for _, cf := range editionCFs {
		if fam, tok, ok := editionInfo(cf.Name); ok {
			byFam[fam] = append(byFam[fam], tok)
		}
	}
	opts := func(fam editionFamily) []string { return append([]string{""}, byFam[fam]...) }
	var out []string
	for _, fr := range opts(famFraming) {
		for _, ct := range opts(famCut) {
			for _, rs := range opts(famRestoration) {
				if fr == "IMAX" && rs == "Criterion" {
					continue // Criterion + IMAX never co-occur
				}
				var parts []string
				for _, p := range []string{fr, ct, rs} {
					if p != "" {
						parts = append(parts, p)
					}
				}
				out = append(out, strings.Join(parts, "."))
			}
		}
	}
	return out
}

// skip returns true if a combination is physically impossible, or rare-but-real
// and not explicitly allowed. Predicates over the dimension VALUES.
func skip(c combo, o Options) bool {
	vcodec := c.vcodec
	if vcodec == "" {
		vcodec = "h265"
	}
	hdr := c.hdr
	isHDR := hdr != "" && !strings.EqualFold(hdr, "SDR")

	// --- Impossible (0 in real data) -> always skip ---
	if vcodec == "h264" && isHDR {
		return true // HDR is HEVC-only
	}
	if objectAudio[c.audio] && stereoOrLess[c.channels] {
		return true // object audio needs >= 5.1
	}
	if maxFiveOneAudio[c.audio] && aboveFiveOne[c.channels] {
		return true // AC3 (DD) is spec-capped at 5.1; AAC and DTS core likewise
	}
	// AV1 is a WEB-DL/encode codec; a Remux is the untouched disc stream, so AV1
	// never appears on Remux (0 of hundreds of real AV1 releases). Everything
	// else (Bluray, 2160p, HDR, tiers, lossless audio) does combine with AV1.
	if vcodec == "AV1" && c.source == "Remux" {
		return true
	}
	// Remux is a disc stream copy: codec is fixed by resolution (1080p = AVC/h264,
	// 2160p = HEVC/h265), HDR only at 2160p, and it never carries a lossy track.
	if c.source == "Remux" {
		if c.resolution == "2160p" && vcodec != "h265" {
			return true // UHD remux is HEVC
		}
		if c.resolution != "2160p" && vcodec == "h265" {
			return true // sub-2160p remux is AVC, not HEVC
		}
		if c.resolution != "2160p" && isHDR {
			return true // only 2160p remux carries HDR
		}
		if !o.AllowLossyRemux && lossyAudio[c.audio] {
			return true // remux preserves the disc's lossless master
		}
	}

	// --- Rare-but-real -> skip unless allowed ---
	if !o.Allow2160pX264 && c.resolution == "2160p" && vcodec == "h264" {
		return true
	}
	if !o.Allow1080pHDR && c.resolution == "1080p" && isHDR {
		return true
	}
	if !o.AllowLosslessStereo && losslessAudio[c.audio] && stereoOrLess[c.channels] {
		return true
	}

	// Streaming-service tags (AMZN, NF, ...) only appear on WEB releases.
	if c.service != "" && c.source != "WEB-DL" {
		return true
	}

	// --- Edition rules (verified against the Criterion/IMAX corpora) ---
	if c.edition != "" {
		// Criterion is disc-only (0 of ~300 on WEB-DL/WEBRip): Bluray or Remux.
		if strings.Contains(c.edition, "Criterion") && c.source != "Bluray" && c.source != "Remux" {
			return true
		}
		// 4K Remaster implies UHD.
		if strings.Contains(c.edition, "4K.Remaster") && c.resolution != "2160p" {
			return true
		}
	}
	return false
}

var losslessAudio = map[string]bool{
	"TrueHD": true, "DTS-HD.MA": true, "DTS-HD.HRA": true, "FLAC": true, "PCM": true,
}

// tokensOf returns the distinct tokens for a dimension (no score-collapse).
func tokensOf(cfs []CF) []string {
	seen := map[string]bool{}
	var out []string
	for _, cf := range cfs {
		t := Token(cf.Name, cf.Dim)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// nonZero keeps only CFs that change the profile's score. A score-0 CF scores
// identically whether or not its token is in the title, so generating a
// variation for it adds rows that never change the outcome. Applied to the
// decoration axes (audio/HDR/channels/service/modifier/edition); the structural
// quality ladder (resolution/source/group tiers) is handled separately.
func nonZero(cfs []CF) []CF {
	var out []CF
	for _, cf := range cfs {
		if cf.Score != 0 {
			out = append(out, cf)
		}
	}
	return out
}

// context is one (source, resolution, group) the title is built around. The
// group value is the release-group axis: either a tier placeholder
// ("Tier1".."TierN"/"NoTier", which the scoring side maps back to the right tier
// CF by source + resolution), a bad-group unwanted name ("YIFY", emitted as a
// real release group so the LQ/Bad-Dual/... CF matches), or "" for a group-less
// release (No-RlsGroup, or a profile with no tiers). One group per release, so
// tiers, bad groups and no-group are mutually exclusive values on this one axis.
type context struct {
	source, resolution, group string
}

var reTier = regexp.MustCompile(`(?i)^(.+?)\s+tier\s+0*(\d+)$`)

// IsTierCF reports whether a CF name is a release-group TIER (e.g. "WEB Tier
// 01"), which the generator expresses with tier placeholders rather than a
// literal group token. The caller uses this to decide whether to mine a literal
// release-group name from a CF's spec instead.
func IsTierCF(name string) bool { return reTier.MatchString(name) }

// sourceForLabel maps a tier CF's source label to a release source token and
// the resolution(s) it implies. Returns ok=false for labels we don't generate
// yet (language-specific FR/German/Anime tiers).
func sourceForLabel(label string) (source string, resolutions []string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "web":
		return "WEB-DL", []string{"2160p", "1080p"}, true
	case "hd bluray":
		return "Bluray", []string{"1080p"}, true
	case "uhd bluray":
		return "Bluray", []string{"2160p"}, true
	case "remux":
		return "Remux", []string{"2160p"}, true
	case "bluray":
		return "Bluray", []string{"2160p", "1080p"}, true
	}
	return "", nil, false
}

// pair is a source + resolution the title is built around, before tiers.
type pair struct{ source, resolution string }

// basePairs decides the (source, resolution) grid. Priority:
//  1. Options.Qualities - the profile's allowed quality items (or its cutoff),
//     parsed by the caller. This is the authoritative coverage when present.
//  2. release-group tier CFs - their source label implies source + resolution.
//  3. Options.Sources x the resolution CFs - the last-resort fallback.
func basePairs(labels map[string]int, resolutionCFs []CF, o Options) []pair {
	seen := map[pair]bool{}
	var out []pair
	add := func(src, res string) {
		p := pair{src, res}
		if src == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	if len(o.Qualities) > 0 {
		for _, q := range o.Qualities {
			add(q.Source, q.Resolution)
		}
		return out
	}
	if len(labels) > 0 {
		for label := range labels {
			src, resos, _ := sourceForLabel(label)
			for _, res := range resos {
				add(src, res)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	for _, src := range o.Sources {
		for _, res := range orEmpty(tokensOf(resolutionCFs)) {
			add(src, res)
		}
	}
	return out
}

// tierLabels returns the max tier number per source label in the group CFs.
func tierLabels(group []CF) map[string]int {
	maxTier := map[string]int{}
	for _, cf := range group {
		m := reTier.FindStringSubmatch(cf.Name)
		if m == nil {
			continue
		}
		label := strings.ToLower(m[1])
		n := atoi(m[2])
		if _, _, ok := sourceForLabel(label); ok && n > maxTier[label] {
			maxTier[label] = n
		}
	}
	return maxTier
}

// maxTierFor returns the highest tier number the profile defines for a given
// (source, resolution), so a base pair can expand into NoTier + Tier1..N.
func maxTierFor(p pair, labels map[string]int) int {
	best := 0
	for label, mx := range labels {
		src, resos, ok := sourceForLabel(label)
		if !ok || src != p.source {
			continue
		}
		for _, res := range resos {
			if res == p.resolution && mx > best {
				best = mx
			}
		}
	}
	return best
}

// buildContexts derives the (source, resolution, tier) contexts to generate:
// the base (source, resolution) grid (quality items / tiers / Options), each
// expanded into NoTier + Tier1..N placeholders where the profile has tier CFs
// for that source + resolution.
func buildContexts(groupCFs, resolutionCFs []CF, badGroups []string, o Options) []context {
	labels := tierLabels(groupCFs) // computed once, reused for every base pair
	// Named "good" release groups: non-tier group CFs that carry an explicit
	// group literal (TheFarm, MainFrame, ...). Each is its own group-axis value
	// so a release with that group is generated and the CF can score it. (Tier
	// CFs are excluded here; they use the tier placeholders above.)
	var goodGroups []string
	seenGood := map[string]bool{}
	for _, cf := range groupCFs {
		if reTier.MatchString(cf.Name) {
			continue
		}
		for _, g := range cf.Groups {
			if g != "" && !seenGood[g] {
				seenGood[g] = true
				goodGroups = append(goodGroups, g)
			}
		}
	}
	var ctxs []context
	for _, p := range basePairs(labels, resolutionCFs, o) {
		mx := maxTierFor(p, labels)
		if mx == 0 {
			ctxs = append(ctxs, context{source: p.source, resolution: p.resolution, group: ""})
		} else {
			ctxs = append(ctxs, context{source: p.source, resolution: p.resolution, group: "NoTier"})
			for t := 1; t <= mx; t++ {
				ctxs = append(ctxs, context{source: p.source, resolution: p.resolution, group: fmt.Sprintf("Tier%d", t)})
			}
		}
		// Bad-group unwanted are release-group-axis values too: a release carries
		// one group, so an LQ / Bad-Dual / Obfuscated / ... release is an
		// alternative to the tier placeholders at this same quality. They combine
		// with every other axis (audio, codec, modifier, ...): that is the danger
		// surface the Calculator needs covered, so they are generated against
		// every base pair.
		for _, g := range badGroups {
			ctxs = append(ctxs, context{source: p.source, resolution: p.resolution, group: g})
		}
		// Named good release groups are group-axis values too: one release per
		// group at this quality, alongside the tier placeholders.
		for _, g := range goodGroups {
			ctxs = append(ctxs, context{source: p.source, resolution: p.resolution, group: g})
		}
	}
	return ctxs
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// GenStats breaks down where the generated count comes from, so the UI can show
// the math: contexts (source x resolution x group) times each decoration axis,
// minus impossible combinations and duplicates, plus the additive unwanted tags.
type GenStats struct {
	Contexts   int `json:"contexts"`
	Audio      int `json:"audio"`
	Channels   int `json:"channels"`
	HDR        int `json:"hdr"`
	Services   int `json:"services"`
	Modifiers  int `json:"modifiers"`
	Codecs     int `json:"codecs"`
	Editions   int `json:"editions"`
	Unwanted   int `json:"unwanted"`   // suffix unwanted-tag axis (incl. the "none" option)
	Product    int `json:"product"`    // raw cartesian (contexts x all axes), before pruning
	Skipped    int `json:"skipped"`    // pruned as impossible by skip()
	Deduped    int `json:"deduped"`    // dropped as duplicate titles
	BaseTitles int `json:"baseTitles"` // titles from the product (after skip + dedup)
	TagTitles  int `json:"tagTitles"`  // one-each additive unwanted-tag titles
	Total      int `json:"total"`      // BaseTitles + TagTitles
}

// Generate builds candidate release titles from the profile's CFs. Audio,
// channels and HDR come from the CFs (one title per distinct release token, so
// every picked CF appears); source, resolution and release-group tier come from
// the tier CFs (or Options when the profile has no tiers).
func Generate(cfs []CF, o Options) []string {
	titles, _ := GenerateWithStats(cfs, o)
	return titles
}

// GenerateWithStats is Generate plus a breakdown of how the count was reached.
func GenerateWithStats(cfs []CF, o Options) ([]string, GenStats) {
	if o.Title == "" {
		o.Title = "Movie"
	}
	if o.Year == "" {
		o.Year = "2026"
	}
	if len(o.VCodecs) == 0 {
		o.VCodecs = []string{"h265"}
	}
	if len(o.Sources) == 0 {
		o.Sources = []string{"WEB-DL"}
	}

	byDim := map[Dimension][]CF{}
	for _, cf := range cfs {
		byDim[cf.Dim] = append(byDim[cf.Dim], cf)
	}
	// One title per DISTINCT release token on each decoration axis, but ONLY for
	// CFs that actually change the score (score != 0). A score-0 CF produces
	// titles that score identically with or without its token, so generating a
	// variation for it is pure noise in the scoring sandbox (e.g. streaming
	// services are usually score-0 in TRaSH profiles). Distinct non-zero tokens
	// are all kept though, even when several share the same score.
	audio := orEmpty(tokensOf(nonZero(byDim[DimAudio])))
	hdr := orEmpty(tokensOf(nonZero(byDim[DimHDR])))
	channels := orEmpty(tokensOf(nonZero(byDim[DimChannels])))
	// Service and modifier (repack/proper) are pick-one-OR-none: each distinct
	// scoring token, plus the empty option (most releases carry neither).
	services := withEmpty(tokensOf(nonZero(byDim[DimService])))
	modifiers := withEmpty(tokensOf(nonZero(byDim[DimModifier])))

	// Partition the unwanted decorations into the axes they belong on. Bad-group
	// names and No-RlsGroup are release-group-axis values; AV1 is a codec-axis
	// value; the rest (3D, 10bit, Monochrome, bracket tags, BR-DISK body) are
	// additive tags that can co-occur with anything, so they stay one-each rather
	// than multiplying the product.
	var badGroups, extraCodecs []string
	var tags []Unwanted
	if o.IncludeUnwanted {
		for _, u := range o.Unwanted {
			switch {
			case u.Codec != "":
				extraCodecs = append(extraCodecs, u.Codec)
			case u.NoGroup:
				badGroups = append(badGroups, "") // group-less release
			case strings.HasPrefix(u.Suffix, "-"):
				badGroups = append(badGroups, strings.TrimPrefix(u.Suffix, "-"))
			default:
				tags = append(tags, u)
			}
		}
	}
	codecs := append(append([]string{}, o.VCodecs...), extraCodecs...)

	// Suffix-style unwanted tags (3D, 10bit, Upscaled, ...) can attach to ANY
	// release, so they form a pick-one-or-none axis that multiplies into the
	// product: every context x audio x ... combination is generated with each
	// unwanted tag and with none. This is what lets the Calculator see an
	// unwanted flag in combination with every other axis (the danger surface),
	// not just on one sample release. Body-override unwanted (BR-DISK = a whole
	// disc release) cannot carry the other axes, so it stays one-off.
	var suffixTags, bodyTags []Unwanted
	for _, u := range tags {
		if u.Body != "" {
			bodyTags = append(bodyTags, u)
		} else {
			suffixTags = append(suffixTags, u)
		}
	}
	unwantedAxis := append([]Unwanted{{}}, suffixTags...) // {} = no unwanted tag

	// Source, resolution and release-group come from the tier CFs (or Options +
	// resolution CFs when the profile has no tiers), with bad-group unwanted added
	// as group-axis values.
	contexts := buildContexts(byDim[DimGroup], byDim[DimResolution], badGroups, o)

	// Movie-version tags (optional). editionCombos always yields "" (no edition),
	// so a profile without edition CFs generates exactly as before. Score-0
	// editions are dropped for the same reason as the other decoration axes.
	editions := editionCombos(nonZero(byDim[DimEdition]))

	st := GenStats{
		Contexts: len(contexts), Audio: len(audio), Channels: len(channels), HDR: len(hdr),
		Services: len(services), Modifiers: len(modifiers), Codecs: len(codecs), Editions: len(editions),
		Unwanted: len(unwantedAxis),
	}
	st.Product = st.Contexts * st.Audio * st.Channels * st.HDR * st.Services * st.Modifiers * st.Codecs * st.Editions * st.Unwanted

	var titles []string
	seen := map[string]bool{} // a group-less No-RlsGroup value can coincide with a no-tier base pair
	var base *combo           // first clean release, reused for the additive-tag titles
	for _, ctx := range contexts {
		for _, a := range audio {
			for _, ch := range channels {
				for _, h := range hdr {
					for _, sv := range services {
						for _, md := range modifiers {
							for _, vc := range codecs {
								for _, ed := range editions {
									c := combo{resolution: ctx.resolution, source: ctx.source, audio: a, channels: ch, hdr: h, service: sv, modifier: md, vcodec: vc, edition: ed}
									if skip(c, o) {
										st.Skipped += len(unwantedAxis) // a skipped base removes all its unwanted variants
										continue
									}
									if base == nil {
										bc := c
										base = &bc
									}
									// Pick-one-or-none unwanted axis: each clean combo is also
									// emitted with each suffix unwanted tag attached.
									for _, uw := range unwantedAxis {
										bc := c
										var t string
										switch {
										case uw.Suffix == "":
											t = buildTitle(bc, ctx.group, o) // no unwanted tag
										case strings.HasPrefix(uw.Suffix, "."):
											bc.infix = uw.Suffix[1:] // inline tag (.3D) before the codec
											t = buildTitle(bc, ctx.group, o)
										default:
											t = buildTitle(bc, ctx.group, o) + uw.Suffix // bracket/end tag
										}
										if seen[t] {
											st.Deduped++
											continue
										}
										seen[t] = true
										titles = append(titles, t)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	st.BaseTitles = len(titles)

	// Body-override unwanted (BR-DISK) is a whole-disc release that can't carry
	// the product axes, so it stays one-off on a single good base release.
	if o.IncludeUnwanted && base != nil {
		for _, t := range unwantedTitles(*base, bodyTags, o) {
			if !seen[t] {
				seen[t] = true
				titles = append(titles, t)
			}
		}
	}
	st.TagTitles = len(titles) - st.BaseTitles
	st.Total = len(titles)
	return titles, st
}

// unwantedTitles renders one penalty title per Unwanted decoration on a single
// group-less good base release (so each shows the profile rejecting exactly one
// thing). The base carries no tier suffix, so a "-GROUP" decoration reads as the
// release group and No-RlsGroup reads as a group-less release.
func unwantedTitles(base combo, tags []Unwanted, o Options) []string {
	var out []string
	seen := map[string]bool{}
	for _, u := range tags {
		var title string
		switch {
		case u.Body != "":
			title = o.Title + "." + o.Year + "." + u.Body
		case u.Codec != "":
			bc := base
			bc.vcodec = u.Codec // e.g. AV1 replaces the codec rather than appending
			title = buildTitle(bc, "", o)
		case u.NoGroup:
			title = buildTitle(base, "", o) // group-less release (No-RlsGroup)
		case strings.HasPrefix(u.Suffix, "."):
			// Inline tag (.Monochrome, .3D, ...): place it before the codec, not at
			// the very end, so the CF's not-at-end guards (`(?!$)`) still match.
			bc := base
			bc.infix = u.Suffix[1:]
			title = buildTitle(bc, "", o)
		default:
			// Release-group (-NAME) or bracket ([tag]) suffix belongs at the end.
			title = buildTitle(base, "", o) + u.Suffix
		}
		if title == "" || seen[title] {
			continue
		}
		seen[title] = true
		out = append(out, title)
	}
	return out
}

// withEmpty prepends the empty option to a value list (the "no tag" case).
func withEmpty(v []string) []string {
	return append([]string{""}, v...)
}

func orEmpty(v []string) []string {
	if len(v) == 0 {
		return []string{""}
	}
	return v
}

func buildTitle(c combo, groupSuffix string, o Options) string {
	parts := []string{o.Title, o.Year}
	vcodec := c.vcodec
	if c.source == "Remux" { // a remux names its codec AVC/HEVC, not x264/x265
		switch vcodec {
		case "h265":
			vcodec = "HEVC"
		case "h264":
			vcodec = "AVC"
		}
	}
	for _, p := range []string{c.edition, c.resolution, c.service, c.source, c.audio, c.channels, c.modifier, c.hdr, c.infix, vcodec} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	title := strings.Join(parts, ".")
	if groupSuffix != "" {
		title += "-" + groupSuffix // tier placeholder (scoring maps it) or bad-group name
	}
	return title
}
