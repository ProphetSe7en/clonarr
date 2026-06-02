package core

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ProfileDescription is the full auto-derived description of a TRaSH quality
// profile, computed from two data sources without any per-profile hand-curation:
//
//  1. Profile JSON (quality-profiles/X.json) — items[], cutoff, formatItems
//  2. CF-group JSONs (cf-groups/*.json) — quality_profiles.include maps,
//     default flag tells whether HDR/Audio scoring is enabled by default
//     and which HDR variants (DV Boost, HDR10+ Boost) are available as
//     opt-ins. The cf-group lists themselves aren't stored — only the
//     boolean conclusions on Axes.HDR / Axes.Audio.
//
// On top of those raw fields, two composed bullet-lists give the editorial
// framing TRaSH itself doesn't ship: Highlights ("what you get") and
// BestFor ("who it suits"). Both are derived from axes + formatItems
// patterns + the profile-name prefix — no invented prose, every bullet
// asserts a fact the data supports.
//
// New profiles TRaSH adds get described automatically on the next pull.
type ProfileDescription struct {
	TrashID    string      `json:"trashId"`
	Name       string      `json:"name"`
	App        string      `json:"app"` // "radarr" | "sonarr"
	Tagline    string      `json:"tagline,omitempty"`
	Axes       ProfileAxes `json:"axes"`
	Highlights []string    `json:"highlights,omitempty"`
	// Disclaimer is TRaSH's own prose disclaimer from the profile JSON's
	// trash_description field, surfaced when it's not just the structural
	// "Quality Profile that covers: ..." auto-text. Currently used for
	// SQP profiles where TRaSH ships an explicit "join the Discord before
	// using" disclaimer they want shown verbatim on every SQP card.
	//
	// Structured into Before / LinkText / LinkURL / After so the frontend
	// can render the embedded <a href=...>...</a> as an actual clickable
	// link inline in the prose, preserving TRaSH's exact wording for the
	// link label (e.g. "TRaSH-Guide Discord") rather than stripping the
	// HTML to plain text.
	Disclaimer *DisclaimerNotice `json:"disclaimer,omitempty"`
	TrashURL   string            `json:"trashUrl,omitempty"`
}

// DisclaimerNotice carries TRaSH's prose disclaimer split around a single
// embedded link. Designed for trash_description fields like the SQP one:
// "...please join the <a href='...'>TRaSH-Guide Discord</a> for more...".
// Disclaimers without a link have only Before populated.
type DisclaimerNotice struct {
	Before   string `json:"before"`
	LinkText string `json:"linkText,omitempty"`
	LinkURL  string `json:"linkUrl,omitempty"`
	After    string `json:"after,omitempty"`
}

// ProfileAxes is the 7-row quick-fact summary shown as pills on the profile card.
type ProfileAxes struct {
	Resolution string              `json:"resolution"`
	Sources    []string            `json:"sources"`
	Codec      string              `json:"codec"`
	HDR        ProfileHDRSummary   `json:"hdr"`
	Audio      ProfileAudioSummary `json:"audio"`
	Cutoff     string              `json:"cutoff"`
	AvgSize    string              `json:"avgSize,omitempty"`
}

// ProfileHDRSummary reports whether HDR is scored by default and what HDR
// variant add-ons (DV Boost, HDR10+ Boost, etc.) are available as opt-ins.
type ProfileHDRSummary struct {
	Scored bool     `json:"scored"`
	OptIns []string `json:"optIns,omitempty"` // e.g. ["DV Boost", "HDR10+ Boost"]
}

// ProfileAudioSummary reports the relationship between this profile and
// the lossless-audio scoring channel ([Audio] Audio Formats cf-group):
//
//	Scored = true  → group is INCLUDED in this profile AND default=true
//	                 (or lossless-audio CFs sit directly in formatItems via
//	                 the fallback path used by SQP-3 (Audio) etc).
//	                 User-facing: "Lossless audio".
//	OptIn  = true  → group is INCLUDED but default is not true.
//	                 The CFs ship with the profile; user toggles them on
//	                 to prefer lossless. Common on WEB-2160p (Alternative)
//	                 and similar profiles where TRaSH lists Audio Formats
//	                 in quality_profiles.include but leaves it opt-in.
//	                 User-facing: "Lossless available".
//	neither        → group is NOT in include list. User-facing: "Lossy audio".
//
// Scored takes precedence — if both flags could be set (theoretical), the
// stronger statement wins.
type ProfileAudioSummary struct {
	Scored bool `json:"scored"`
	OptIn  bool `json:"optIn,omitempty"`
}

// DescribeProfiles returns ProfileDescription for every profile in the app's
// loaded TRaSH data. Combines profile JSONs and cf-groups — all fields
// auto-derived from data, no external markdown dependency. Safe to call
// before data is loaded — returns empty slice if no profiles available.
func (ts *TrashStore) DescribeProfiles(app string) ([]ProfileDescription, error) {
	snap := ts.Snapshot()
	if snap == nil {
		return nil, nil
	}
	var appData *AppData
	switch app {
	case "radarr":
		appData = &snap.Radarr
	case "sonarr":
		appData = &snap.Sonarr
	default:
		return nil, fmt.Errorf("unknown app: %s", app)
	}
	if len(appData.Profiles) == 0 {
		return nil, nil
	}
	out := make([]ProfileDescription, 0, len(appData.Profiles))
	for _, p := range appData.Profiles {
		out = append(out, describeProfile(app, p, appData.CFGroups))
	}
	return out, nil
}

// DescribeProfile returns the description for a single profile by trash_id.
// Returns nil + nil error if the profile isn't found (caller should 404).
func (ts *TrashStore) DescribeProfile(app, trashID string) (*ProfileDescription, error) {
	all, err := ts.DescribeProfiles(app)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].TrashID == trashID {
			return &all[i], nil
		}
	}
	return nil, nil
}

// describeProfile builds a complete ProfileDescription for one profile by
// combining its JSON, the cf-groups that include it, and (optionally) the
// matching markdown section.
//
// Falls back to JSON-only data when markdown is missing or the section is
// absent — UI gets empty Tagline/Note and still renders the axes + composed
// Highlights / BestFor (which only need JSON-derivable facts).
func describeProfile(
	app string,
	profile *TrashQualityProfile,
	allGroups []*TrashCFGroup,
) ProfileDescription {
	out := ProfileDescription{
		TrashID:  profile.TrashID,
		Name:     profile.Name,
		App:      app,
		TrashURL: sanitizeURL(profile.TrashURL),
		Axes: ProfileAxes{
			Resolution: deriveResolution(profile.Items),
			Sources:    deriveSources(profile.Items),
			Codec:      deriveCodec(profile.Items),
			Cutoff:     profile.Cutoff,
		},
	}
	// Tagline + AvgSize are auto-generated uniformly for ALL profiles
	// from the axes + profile metadata. Markdown taglines only existed
	// for ~9 of 30+ profiles, leaving anime / SQP / foreign-language
	// cards visually sparse. Generating both from profile data gives
	// every card the same level of detail.
	out.Tagline = composeTagline(profile, out.Axes)
	out.Axes.AvgSize = typicalSize(app, profile, out.Axes)

	// Walk cf-groups looking for HDR/Audio scoring inclusion. The cf-group
	// LISTS themselves aren't stored — only the boolean conclusions on
	// Axes.HDR.Scored / Axes.Audio.Scored / Axes.HDR.OptIns. That info
	// drives the pills + the composer.
	var hdrOptIns []string
	for _, g := range allGroups {
		if _, ok := g.QualityProfiles.Include[profile.Name]; !ok {
			continue
		}
		isDefault := strings.ToLower(g.Default) == "true"
		if isDefault {
			if g.TrashID == audioFormatsTrashID(app) {
				out.Axes.Audio.Scored = true
			}
			if g.TrashID == hdrFormatsTrashID(app) {
				out.Axes.HDR.Scored = true
			}
		} else if g.TrashID == audioFormatsTrashID(app) {
			// Audio Formats group is listed in the profile's include
			// map but ships without a default flag (or default=false).
			// CFs travel with the profile but aren't scored; user opts
			// in by toggling the group. Surfaces as "Lossless available"
			// on the profile card and as an extra "What you get" bullet
			// so users know the capability is there even though the
			// pill stops short of "Lossless audio".
			out.Axes.Audio.OptIn = true
		} else if label := hdrVariantOptInLabel(app, g.TrashID); label != "" {
			// default=false HDR variant — surface as opt-in on the pill.
			// Matched by trash_id (stable across TRaSH-Guides updates)
			// rather than name prefix — TRaSH adjusts CF-group names
			// occasionally but trash_ids never change once assigned.
			hdrOptIns = append(hdrOptIns, label)
		}
	}
	// Fallback: profiles like [SQP] SQP-3 (Audio) score the same lossless
	// audio CFs directly in formatItems instead of pulling them in via the
	// [Audio] Audio Formats cf-group. Detect that case by looking for any
	// known lossless-audio trash_id in profile.FormatItems. Without this,
	// audio-focused profiles get a false "lossy" pill.
	if !out.Axes.Audio.Scored && profile.FormatItems != nil {
		for _, tid := range profile.FormatItems {
			if losslessAudioCFTrashIDs[tid] {
				out.Axes.Audio.Scored = true
				break
			}
		}
	}
	// Same fallback for HDR scoring. Profiles like SQP-1 (2160p) include
	// DV / HDR10+ CFs directly in formatItems rather than via the
	// [HDR Formats] HDR cf-group. Surface basic HDR scoring whenever ANY
	// HDR-family CF is in formatItems; also collect non-basic variants
	// (DV Boost, DV w/o HDR fallback, HDR10+ Boost) as opt-in labels.
	if profile.FormatItems != nil {
		seen := make(map[string]bool, len(hdrOptIns))
		for _, l := range hdrOptIns {
			seen[l] = true
		}
		for _, tid := range profile.FormatItems {
			label, isHDR := hdrCFTrashIDLabels[tid]
			if !isHDR {
				continue
			}
			out.Axes.HDR.Scored = true
			if label != "" && label != "HDR" && !seen[label] {
				hdrOptIns = append(hdrOptIns, label)
				seen[label] = true
			}
		}
	}
	sort.Strings(hdrOptIns)
	out.Axes.HDR.OptIns = hdrOptIns

	// Compose Highlights from the now-populated axes + profile data.
	// "Best for" was tried earlier but dropped — claiming "best for X TVs"
	// is editorial interpretation we don't have authority to make. The
	// pills + Highlights tell users what the profile DOES; who it suits
	// is the user's own call.
	out.Highlights = composeHighlights(profile, out.Axes)

	// Some profiles ship an explicit prose disclaimer in
	// profile.TrashDescription (vs the structural "Quality Profile that
	// covers: WEBDL 1080p..." form that standard profiles use, which is
	// redundant with our pills + axes).
	//
	// Surface the disclaimer as a prominent notice on the card. Currently
	// applies to:
	//   - SQP profiles — TRaSH ships an "advanced profile, join Discord"
	//     disclaimer they want shown verbatim on every SQP card
	//   - Base Profile — TRaSH's internal test profile ("This is a base
	//     profile that we use for testing"); users shouldn't pick it
	//     thinking it's a real profile
	if profile.TrashDescription != "" && (isSQPProfile(profile.Name) || isBaseProfile(profile.Name)) {
		notice := parseDisclaimerHTML(profile.TrashDescription)
		notice.LinkURL = sanitizeURL(notice.LinkURL)
		// Drop the link entirely when the URL fails the scheme gate — otherwise
		// the frontend would render a click-able label pointing to "" which is
		// either confusing (re-navigates the current page) or a vector if a
		// future framework upgrade interprets the empty href differently.
		if notice.LinkURL == "" {
			notice.LinkText = ""
		}
		out.Disclaimer = &notice
		if notice.LinkURL != "" {
			out.TrashURL = notice.LinkURL
		}
	}
	return out
}

// --- Derivation helpers (JSON → axes) ---

// deriveResolution returns a human-readable "1080p (720p Bluray fallback)" style
// summary from the allowed quality items in the profile.
func deriveResolution(items []QualityItem) string {
	// Walk allowed items in order; the first one is the cutoff target. We
	// classify by resolution (extracted from names like "Bluray-1080p",
	// "WEB 2160p"). Items appear top-down by preference, so the first
	// resolution mentioned is the target; lower resolutions are fallbacks.
	//
	// "Merged QPs" profiles (typically German UHD variants) wrap resolution
	// items inside a generic parent like {name: "Merged QPs", items:
	// ["Bluray-2160p", "WEBDL-2160p"]}. Falling back to scan nested items
	// when the parent name has no resolution token covers that case.
	resOrder := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		if !it.Allowed {
			continue
		}
		res := itemResolution(it)
		if res == "" {
			continue
		}
		if !seen[res] {
			seen[res] = true
			resOrder = append(resOrder, res)
		}
	}
	if len(resOrder) == 0 {
		return "unknown"
	}
	if len(resOrder) == 1 {
		return resOrder[0]
	}
	// Multiple resolutions — main + fallback list
	return resOrder[0] + " (" + strings.Join(resOrder[1:], ", ") + " fallback)"
}

// deriveSources returns the deduplicated list of release types accepted by the
// profile (Bluray, Bluray Remux, WEB-DL, WEBRip, etc.).
func deriveSources(items []QualityItem) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		if !it.Allowed {
			continue
		}
		// Flatten nested items (WEB 1080p contains WEBRip-1080p + WEBDL-1080p)
		names := []string{it.Name}
		names = append(names, it.Items...)
		for _, n := range names {
			src := extractSource(n)
			if src == "" || seen[src] {
				continue
			}
			seen[src] = true
			out = append(out, src)
		}
	}
	return out
}

// itemResolution returns the resolution token for a quality item, falling
// back to nested items[] when the parent name has no resolution (Merged
// QPs profiles). Used by both deriveResolution and fallbackHighlight so
// the single-level vs nested-children behavior stays consistent.
func itemResolution(it QualityItem) string {
	if res := extractResolution(it.Name); res != "" {
		return res
	}
	for _, sub := range it.Items {
		if res := extractResolution(sub); res != "" {
			return res
		}
	}
	return ""
}

// deriveCodec returns the dominant codec for the resolution range of the profile.
// HD = x264 (industry standard for Bluray/WEB-DL HD). UHD = x265/HEVC (industry
// standard for 4K Bluray/WEB-DL). Mixed profiles default to the highest-resolution
// codec since that's what the cutoff drives toward.
func deriveCodec(items []QualityItem) string {
	hasUHD := false
	for _, it := range items {
		if !it.Allowed {
			continue
		}
		// Check parent name + nested items[] (Merged QPs profiles wrap
		// the resolution tokens one level deeper)
		names := append([]string{it.Name}, it.Items...)
		for _, n := range names {
			if strings.Contains(n, "2160p") || strings.Contains(strings.ToLower(n), "uhd") {
				hasUHD = true
				break
			}
		}
		if hasUHD {
			break
		}
	}
	if hasUHD {
		return "x265"
	}
	return "x264"
}

// extractResolution pulls a normalized resolution token from a quality name.
// "Bluray-1080p" → "1080p"; "WEB 2160p" → "2160p"; "Remux-1080p" → "1080p".
// Returns "" for non-resolution items (Raw-HD, Unknown, etc.).
func extractResolution(name string) string {
	for _, res := range []string{"2160p", "1080p", "720p", "576p", "480p"} {
		if strings.Contains(name, res) {
			return res
		}
	}
	return ""
}

// extractSource normalizes a quality-item or sub-item name to a canonical
// source label. "Bluray-1080p" → "Bluray"; "WEBDL-1080p" → "WEB-DL";
// "WEBRip-1080p" → "WEBRip"; "Remux-2160p" → "UHD Bluray Remux"; etc.
// Returns "" for quality-grouping items (e.g. "WEB 1080p" itself, since its
// children carry the actual source).
func extractSource(name string) string {
	switch {
	case strings.Contains(name, "Bluray-2160p Remux"), strings.HasPrefix(name, "Remux-2160p"):
		return "UHD Bluray Remux"
	case strings.Contains(name, "Bluray-1080p Remux"), strings.HasPrefix(name, "Remux-1080p"):
		return "Bluray Remux"
	case strings.HasPrefix(name, "Bluray-2160p"):
		return "UHD Bluray"
	case strings.HasPrefix(name, "Bluray-"):
		return "Bluray"
	case strings.HasPrefix(name, "WEBDL-"):
		return "WEB-DL"
	case strings.HasPrefix(name, "WEBRip-"):
		return "WEBRip"
	case strings.HasPrefix(name, "HDTV-"):
		return "HDTV"
	case strings.HasPrefix(name, "DVD"):
		return "DVD"
	}
	return ""
}

// cleanTagline strips redundant prefixes from TRaSH's markdown tagline.
// Most start with "If you prefer " which adds no info on a profile card —
// the user is on the profile's card, of course they're considering it.
func cleanTagline(raw string) string {
	if raw == "" {
		return ""
	}
	for _, prefix := range []string{
		"If you prefer ",
		"If you want ",
	} {
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimPrefix(raw, prefix)
			// Capitalize first letter, keep rest verbatim
			if rest == "" {
				return raw
			}
			return strings.ToUpper(rest[:1]) + rest[1:]
		}
	}
	return raw
}

// --- HDR / Audio cf-group ID lookup ---

// audioFormatsTrashID returns the trash_id of the primary Audio Formats
// cf-group for the given app. Used by describeProfile to flag the Audio axis
// as "scored" when this group is included with default=true. IDs are stable
// (set at CF-group creation, never changed by TRaSH).
func audioFormatsTrashID(app string) string {
	switch app {
	case "radarr":
		return "9d5acd8f1da78dfbae788182f7605200"
	case "sonarr":
		return "e9a1944a254e6f8a9da63083f7ae15cb"
	}
	return ""
}

// losslessAudioCFTrashIDs is the set of TRaSH-Guides CF trash_ids that
// represent lossless / object-based-audio formats. Used as a fallback
// when a profile lists these CFs directly in formatItems instead of
// pulling them in via the [Audio] Audio Formats cf-group (e.g. all
// Radarr SQP profiles do this).
//
// What counts as "lossless audio" for this signal:
//   - True lossless: FLAC, PCM, TrueHD, DTS-HD MA, DTS-HD HRA
//   - Object-based / Atmos-bearing (premium audio scoring): TrueHD
//     ATMOS, DTS X, ATMOS (undefined), DD+ ATMOS
// Excluded (lossy, not premium-scored): AAC, DD, DD+, DTS, DTS-ES,
// MP3, Opus.
//
// Includes both Radarr and Sonarr trash_ids — app prefixes happen to
// differ but membership-by-trash_id is stable across TRaSH updates.
var losslessAudioCFTrashIDs = map[string]bool{
	// Radarr
	"496f355514737f7d83bf7aa4d24f8169": true, // TrueHD ATMOS
	"2f22d89048b01681dde8afe203bf2e95": true, // DTS X
	"417804f7f2c4308c1f4c5d380d4c4475": true, // ATMOS (undefined)
	"1af239278386be2919e1bcee0bde047e": true, // DD+ ATMOS
	"3cafb66171b47f226146a0770576870f": true, // TrueHD
	"dcf3ec6938fa32445f590a4da84256cd": true, // DTS-HD MA
	"a570d4a0e56a2874b64e5bfa55202a1b": true, // FLAC
	"e7c2fcae07cbada050a0af3357491d7b": true, // PCM
	"8e109e50e0a0b83a5098b056e13bf6db": true, // DTS-HD HRA
	// Sonarr (parallel set — no profiles currently use these via
	// formatItems but pre-populated so future SQP-style profiles get
	// the same detection automatically).
	"0d7824bb924701997f874e7ff7d4844a": true, // TrueHD ATMOS
	"9d00418ba386a083fbf4d58235fc37ef": true, // DTS X
	"b6fbafa7942952a13e17e2b1152b539a": true, // ATMOS (undefined)
	"4232a509ce60c4e208d13825b7c06264": true, // DD+ ATMOS
	"1808e4b9cee74e064dfae3f1db99dbfe": true, // TrueHD
	"c429417a57ea8c41d57e6990a8b0033f": true, // DTS-HD MA
	"851bd64e04c9374c51102be3dd9ae4cc": true, // FLAC
	"30f70576671ca933adbdcfc736a69718": true, // PCM
	"cfa5fbd8f02a86fc55d8d223d06a5e1f": true, // DTS-HD HRA
}

// hdrCFTrashIDsByID lookup: trash_id → human label for HDR-related CFs
// that profiles can include directly in formatItems. Mirrors the
// losslessAudio fallback but for HDR scoring. Profiles like SQP-1 (2160p)
// list these CFs directly, bypassing the cf-group route — without this
// fallback they'd get a false "no HDR" pill.
//
// Includes the basic HDR CF + the variant CFs (DV Boost, DV w/o HDR
// fallback, HDR10+ Boost). Excludes SDR — that's the absence of HDR.
var hdrCFTrashIDLabels = map[string]string{
	// Radarr
	"493b6d1dbec3c3364c59d7607f7e3405": "HDR",                    // basic HDR
	"b337d6812e06c200ec9a2d3cfa9d20a7": "DV Boost",               // variant
	"923b6abef9b17f937fab56cfcf89e1f1": "DV (w/o HDR fallback)",  // variant
	"caa37d0df9c348912df1fb1d88f9273a": "HDR10+ Boost",           // variant
	// Sonarr
	"505d871304820ba7106b693be6fe4a9e": "HDR",
	"7c3a61a9c6cb04f52f1544be6d44a026": "DV Boost",
	"9b27ab6498ec0f31a3353992e19434ca": "DV (w/o HDR fallback)",
	"0c4b99df9206d2cfac3c05ab897dd62a": "HDR10+ Boost",
}

// hdrFormatsTrashID returns the trash_id of the primary HDR Formats cf-group
// for the given app. Same purpose as audioFormatsTrashID for HDR scoring.
func hdrFormatsTrashID(app string) string {
	switch app {
	case "radarr":
		return "ef20e67b95a381fb3bc6d1f06ea24f46"
	case "sonarr":
		return "7e1724c5da59e7474803ad25be98f6a3"
	}
	return ""
}

// hdrVariantOptInLabel returns a short pill-friendly label ("DV Boost",
// "HDR10+ Boost", "DV (w/o HDR fallback)") for known HDR-variant cf-groups
// matched by trash_id. Returns "" when the trash_id isn't a recognized
// variant OR is the SDR group (too niche for the axis pill).
//
// Trash_id matching (not name prefix) is deliberate: TRaSH renames or
// restructures cf-group names occasionally, but trash_ids never change
// once assigned. The cost is a small per-app lookup table — manageable
// for the ~5 HDR variants TRaSH ships, and adding new ones is a single
// table entry when they appear.
func hdrVariantOptInLabel(app, trashID string) string {
	var table map[string]string
	switch app {
	case "radarr":
		table = map[string]string{
			"1616617ab3a14397a2b2321bcbda44d1": "DV Boost",
			"7fc2751eef7e6bdc70b74136e5e35c76": "DV (w/o HDR fallback)",
			"b29413a7487478fe98228ce79e5689e4": "HDR10+ Boost",
			// "47f0d69750de9e16855915fa73bb7b08" (SDR) intentionally omitted —
			// it's a negative-prefer "exclude SDR" toggle, not a user-facing
			// HDR-format choice; cluttering the pill with it hurts more than
			// helps.
		}
	case "sonarr":
		table = map[string]string{
			"e0b2774083df4265f25c9e5bc6c80940": "DV Boost",
			"d776a1ea912a117d66d83b880ff2055d": "DV (w/o HDR fallback)",
			"7d366c213e5c23a052b157356fac1921": "HDR10+ Boost",
		}
	}
	return table[trashID]
}

// --- Editorial composers (auto-derived "what you get" + "best for") ---
//
// composeHighlights and composeBestFor turn the raw axes + formatItems +
// profile name into bullet-list editorial framing TRaSH itself doesn't
// ship. The rule is strict: each bullet must assert a FACT the data
// supports. No invented use-cases, no aspirational marketing copy. If a
// profile's data doesn't justify a bullet, we leave it out — sparse cards
// are fine.

// composeHighlights returns "What you get" bullets describing what the
// profile picks for and what it prefers. Phrasing rule: speak to end
// users, not to TRaSH-Guides power-users. No internal jargon — terms
// like "BD Tier 1-8", "Repack/Proper", "DV Boost CF" mean nothing to
// someone choosing a profile for the first time. We translate to
// plain-English equivalents that describe outcomes.
func composeHighlights(profile *TrashQualityProfile, axes ProfileAxes) []string {
	var out []string

	// 1) Source statement — most important fact, always first.
	//    Note: SQP profiles get their warning via the Disclaimer field
	//    (TRaSH's own verbatim text from profile.trash_description),
	//    rendered as a separate notice block above Highlights — not
	//    duplicated here.
	if src := sourceHighlight(profile, axes.Sources); src != "" {
		out = append(out, src)
	}

	// 1b) Fallback behavior — critical for differentiating
	//     "strict" vs "Alternative" vs "Combined" profile variants. Three
	//     profiles can have the same cutoff + same sources but completely
	//     different fallback chains (Remux + WEB 2160p = strict 2160p,
	//     Remux 2160p Alternative = falls all the way down to SDTV,
	//     Remux 2160p Combined = 2160p + 1080p). Without this bullet
	//     they'd render identically.
	if fb := fallbackHighlight(profile.Items); fb != "" {
		out = append(out, fb)
	}

	// 2) HDR — describe what the user gets, not the toggle mechanism.
	//    Inline format list with "etc." since the exact mix varies per
	//    profile and the user just needs "this profile picks HDR".
	if axes.HDR.Scored {
		formats := normalizeHDROptInFormats(axes.HDR.OptIns)
		if len(formats) > 0 {
			out = append(out, "Picks HDR releases (HDR10, "+joinAnd(formats)+", etc.)")
		} else {
			out = append(out, "Picks HDR releases (HDR10, etc.)")
		}
	}

	// 3) Audio — name the formats users recognise, drop "DTS-HD MA" tail
	//    which non-cinephiles don't parse anyway. Three states:
	//    - Scored: lossless is the default behaviour
	//    - OptIn: lossless CFs are bundled but the user has to enable
	//      the [Audio] Audio Formats group to prefer them — common on
	//      WEB profiles where TRaSH lists Audio Formats as available
	//      but leaves the default off because lossless is rarer on WEB
	//    - Neither: profile ships lossy-only
	if axes.Audio.Scored {
		out = append(out, "Prefers releases with lossless audio (Atmos, DTS-X, TrueHD)")
	} else if axes.Audio.OptIn {
		out = append(out, "Lossless audio available — enable the [Audio] Audio Formats group to prefer Atmos / DTS-X / TrueHD")
	}

	// 4) Variant-specific tuning. No "Tier 1-8" detail — users don't know
	//    what TRaSH tiers are.
	//
	//    Anime Dual Audio CF: previously triggered a "prefers multi-audio
	//    releases" bullet. Dropped after verifying the CF has trash_scores
	//    = null in TRaSH data — its presence in formatItems doesn't mean
	//    scoring; could just be tracking. The German Anime variant has
	//    German DL +11000 doing the multi-audio work, not Anime Dual Audio.
	//    Generic claim about anime multi-audio isn't data-supported.
	if hasAnimeTuning(profile) {
		out = append(out, "Tuned for anime-specific release groups")
	}
	if vlabel := languageVariantHighlight(profile.Name); vlabel != "" {
		out = append(out, vlabel)
	}
	// SQP profiles vary widely (SQP-1 = streaming 1080p, SQP-3 Audio = UHD
	// audio-focused, etc.) — the generic "stricter streaming scoring"
	// blurb doesn't fit them all. The Disclaimer already conveys "advanced
	// profile, read the Discord guide" via TRaSH's own wording; no need
	// for clonarr to add a separate guess about what SQP does.

	// 5) Repack/Proper — say what it DOES, not what the CFs are called.
	if hasRepackScoring(profile) {
		out = append(out, "Automatically upgrades when an improved release of the same file is published")
	}

	// 6) Typical file size — last bullet, useful sanity-check before
	//    committing storage. Skipped when markdown didn't ship a size for
	//    this profile (anime / SQP / foreign variants).
	if axes.AvgSize != "" {
		out = append(out, "Typical size: "+axes.AvgSize)
	}

	return out
}

// normalizeHDROptInFormats maps TRaSH's HDR add-on cf-group names (which
// describe SCORING BOOSTS, not formats) to the actual HDR formats the
// user gains the ability to prefer. End-user vocabulary: "Dolby Vision"
// and "HDR10+", not "DV Boost / DV (w/o HDR fallback) / HDR10+ Boost".
func normalizeHDROptInFormats(optIns []string) []string {
	var hasDV, hasHDR10Plus bool
	for _, o := range optIns {
		if strings.HasPrefix(o, "DV") {
			hasDV = true
		}
		if strings.Contains(o, "HDR10+") {
			hasHDR10Plus = true
		}
	}
	var out []string
	if hasDV {
		out = append(out, "Dolby Vision")
	}
	if hasHDR10Plus {
		out = append(out, "HDR10+")
	}
	return out
}

// joinAnd joins a list with "and" before the last element, comma
// elsewhere. Reads naturally in prose: ["A","B","C"] → "A, B and C".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// cutoffSource resolves the profile's cutoff item to its canonical source
// label (the same vocabulary extractSource produces). Returns "" when the
// profile has no cutoff field or when the cutoff item has no resolvable
// source (e.g. quality-grouping shells whose own name doesn't carry a
// source token AND whose children are empty — degenerate case).
//
// The cutoff drives "what the profile is ASKING for" — anything ranked
// lower in the items list is fallback. Without this signal, sourceHighlight
// and pickSourceClass have to guess from the flat set of allowed sources,
// which yields wrong answers on profiles like WEB-2160p (Alternative)
// where Bluray is allowed as fallback but the cutoff is WEB 2160p.
func cutoffSource(profile *TrashQualityProfile) string {
	if profile == nil || profile.Cutoff == "" {
		return ""
	}
	for _, it := range profile.Items {
		if it.Name != profile.Cutoff {
			continue
		}
		// Try the item itself first (covers single-source items like
		// "Bluray-2160p"). Fall through to sub-items if the parent is
		// a quality-grouping shell ("WEB 2160p" wrapping WEBRip-2160p
		// + WEBDL-2160p).
		if s := extractSource(it.Name); s != "" {
			return s
		}
		for _, sub := range it.Items {
			if s := extractSource(sub); s != "" {
				return s
			}
		}
		return ""
	}
	return ""
}

// sourceHighlight returns the canonical primary-source-description bullet
// derived from the profile's CUTOFF when available, falling back to the
// normalised source set for profiles without a recognisable cutoff. The
// cutoff path matters for "Alternative" variants — WEB-2160p (Alternative)
// has Bluray allowed as fallback but the cutoff is WEB 2160p, so the
// primary description must talk about WEB-DL, not Bluray.
//
// Describes ONLY the cutoff-tier source — the "+ WEB" / fallback wording
// lives elsewhere:
//   - Source pill (e.g. "Bluray Remux + WEB") covers what other sources
//     are accepted at the same resolution
//   - fallbackHighlight covers the lower-resolution fallback chain
//
// Earlier versions appended "with WEB-DL fallback" here too, which
// contradicted fallbackHighlight on permissive profiles ("(Alternative)"
// variants that fall through to SDTV/DVD). Keeping primary-source-only
// makes the three signals — pill, source bullet, fallback bullet —
// non-overlapping and consistent.
func sourceHighlight(profile *TrashQualityProfile, sources []string) string {
	// Two-step resolve: try cutoff first (authoritative answer for
	// "what is the profile targeting"), then fall back to the set
	// heuristic when cutoff is empty OR returns a source token the
	// switch doesn't recognise (e.g. a future TRaSH addition like a
	// hypothetical HDTV-cutoff profile). Without the fallback on
	// unknown cutoff values, profile cards would silently drop the
	// highlight bullet on any TRaSH schema change.
	primary := cutoffSource(profile)
	mapPrimaryToBullet := func(p string) string {
		switch p {
		case "UHD Bluray Remux":
			return "Uncompressed 4K Bluray Remux (disc-perfect picture)"
		case "Bluray Remux":
			return "Uncompressed 1080p Bluray Remux (disc-perfect picture)"
		case "UHD Bluray":
			return "4K Bluray encodes from approved release groups"
		case "Bluray":
			return "1080p Bluray encodes from approved release groups"
		case "WEB-DL", "WEBRip":
			return "Streaming WEB-DL releases from approved release groups"
		}
		return ""
	}
	if bullet := mapPrimaryToBullet(primary); bullet != "" {
		return bullet
	}
	// Cutoff empty or unrecognised → derive from full sources set.
	set := map[string]bool{}
	for _, s := range sources {
		set[s] = true
	}
	switch {
	case set["UHD Bluray Remux"]:
		return mapPrimaryToBullet("UHD Bluray Remux")
	case set["Bluray Remux"]:
		return mapPrimaryToBullet("Bluray Remux")
	case set["UHD Bluray"]:
		return mapPrimaryToBullet("UHD Bluray")
	case set["Bluray"]:
		return mapPrimaryToBullet("Bluray")
	case set["WEB-DL"], set["WEBRip"]:
		return mapPrimaryToBullet("WEB-DL")
	}
	return ""
}

// hasAnimeTuning is true when the profile uses TRaSH's anime-specific
// scoring system. Detected by either the [Anime] name prefix or the
// presence of any Anime Tier CF in formatItems (covers both Sonarr and
// Radarr anime profiles).
func hasAnimeTuning(p *TrashQualityProfile) bool {
	if strings.HasPrefix(p.Name, "[Anime]") {
		return true
	}
	for name := range p.FormatItems {
		if strings.HasPrefix(name, "Anime BD Tier") || strings.HasPrefix(name, "Anime Web Tier") {
			return true
		}
	}
	return false
}

// hasRepackScoring is true when the profile scores TRaSH's Repack/Proper
// CFs (the standard re-release auto-upgrade mechanism). Universal in
// TRaSH profiles today; guarded so a future profile without it doesn't
// claim the feature.
func hasRepackScoring(p *TrashQualityProfile) bool {
	for name := range p.FormatItems {
		if name == "Repack/Proper" || name == "Repack2" || name == "Repack3" {
			return true
		}
	}
	return false
}

// languageVariantHighlight returns a one-line variant description for
// recognised French/German naming prefixes. These prefixes encode well-
// documented torrenting-community conventions, not editorial speculation:
//
//   [French MULTi.VF]  — multi-audio with French dub (Version Française)
//   [French MULTi.VO]  — multi-audio with original audio (e.g. English)
//   [French VOSTFR]    — original audio with French subtitles
//   [German]           — German-audio variant of the same base profile
//
// Returns empty for unrecognised prefixes — caller skips the bullet.
func languageVariantHighlight(name string) string {
	switch {
	case strings.HasPrefix(name, "[French MULTi.VF]"):
		return "Multi-audio with French dub (VF) preferred"
	case strings.HasPrefix(name, "[French MULTi.VO]"):
		return "Multi-audio with original audio (VO) preferred over French dubs"
	case strings.HasPrefix(name, "[French VOSTFR]"):
		return "Original audio with French subtitles (VOSTFR)"
	case strings.HasPrefix(name, "[German]"):
		return "German audio preferred"
	}
	return ""
}

func isSQPProfile(name string) bool { return strings.HasPrefix(name, "[SQP]") }


// fallbackHighlight describes how the profile behaves when its primary
// (cutoff) resolution isn't available. Returns empty for strict
// single-resolution profiles. Drives the distinction between standard
// profiles, "(Alternative)" variants (accept anything down to SDTV),
// and "(Combined)" variants (2160p + 1080p together).
func fallbackHighlight(items []QualityItem) string {
	var resOrder []string
	seen := map[string]bool{}
	for _, it := range items {
		if !it.Allowed {
			continue
		}
		res := itemResolution(it)
		if res == "" || seen[res] {
			continue
		}
		seen[res] = true
		resOrder = append(resOrder, res)
	}
	if len(resOrder) <= 1 {
		// Strict profile — no fallback to describe (saves a bullet
		// that would just repeat the cutoff resolution).
		return ""
	}
	primary := resOrder[0]
	rest := resOrder[1:]
	// Very permissive profiles (4+ fallback rungs) — describe as full
	// fallback rather than enumerating "1080p, 720p, 576p, 480p" which
	// reads like data noise.
	if len(rest) >= 4 {
		return "Falls back through all lower qualities (down to SDTV/DVD) when no " + primary + " release is available"
	}
	return "Falls back to " + joinAnd(rest) + " when no " + primary + " release is available"
}

// isBaseProfile is true for TRaSH's internal test profile "Base Profile",
// which ships with a "this is a base profile we use for testing" disclaimer.
// Surfacing it on the card warns users not to pick it as a real profile.
func isBaseProfile(name string) bool { return name == "Base Profile" }

// sanitizeURL returns the URL only if it has a safe http(s) scheme, else "".
// Defends against malicious or compromised TRaSH upstream content embedding
// javascript: / data: / vbscript: URLs in trash_url or anchor hrefs inside
// trash_description. The frontend renders these via Alpine ':href' bindings
// that bypass the classic sanitizeHTML() path (which already strips unsafe
// schemes for x-html), so we gate at the JSON boundary instead.
func sanitizeURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

// parseDisclaimerHTML splits TRaSH's trash_description HTML into a
// DisclaimerNotice with the first <a href="...">label</a> preserved as
// structured Before / LinkText / LinkURL / After fields. <br> tags become
// spaces. Disclaimers without a link end up with only Before populated.
// NOT a general-purpose sanitiser — assumes input is TRaSH-shipped JSON
// content, not user input. Keeps to stdlib regex rather than pulling in
// bluemonday for a single field.
func parseDisclaimerHTML(s string) DisclaimerNotice {
	// Convert <br> tags to spaces first so the disclaimer reads as prose
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, " ")

	// Extract the first anchor: href + label + the surrounding before/after
	// segments in one regex pass. Submatches: 1=href, 2=label.
	anchorRe := regexp.MustCompile(`(?is)(.*?)<a[^>]+href=["']([^"']+)["'][^>]*>(.*?)</a>(.*)`)
	m := anchorRe.FindStringSubmatch(s)

	clean := func(x string) string {
		// Strip any leftover HTML tags + collapse whitespace
		x = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(x, "")
		return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(x, " "))
	}
	if m == nil {
		// No anchor found — whole string becomes Before
		return DisclaimerNotice{Before: clean(s)}
	}
	return DisclaimerNotice{
		Before:   strings.TrimRight(clean(m[1]), " ") + " ",
		LinkURL:  m[2],
		LinkText: clean(m[3]),
		After:    " " + strings.TrimLeft(clean(m[4]), " "),
	}
}

// --- Auto-generated tagline + size (uniform across all profiles) ---
//
// Replaces the markdown-derived Tagline + AvgSize that only existed
// for ~9 of TRaSH's ~30 profiles (standard Radarr/Sonarr). Generating
// both from JSON-derivable facts gives every profile card the same
// level of detail: Anime, SQP, French, German all get a tagline + size
// estimate without depending on TRaSH adding per-profile markdown.
//
// The composition is rule-based (not editorial): each leading
// adjective and size range maps from observable profile properties.

// composeTagline returns a short "what the profile prefers" descriptor
// like "Prefers high-quality 1080p encodes" or "Prefers uncompressed 4K
// UHD Remux". The "Prefers" verb is deliberate — without it the tagline
// reads as a guarantee ("Uncompressed 4K UHD Remux") which is misleading
// when the profile actually has fallback policies. "Prefers" signals
// this is the target, not the outcome. Pills + What you get cover the
// actual fallback behavior.
//
// Appends a variant suffix when the profile name signals it deviates
// further from strict cutoff-only behavior:
//   "Remux 2160p (Alternative)" → "...· with full fallback chain"
//   "Remux 2160p (Combined)"    → "...· multi-resolution"
func composeTagline(profile *TrashQualityProfile, axes ProfileAxes) string {
	if isBaseProfile(profile.Name) {
		return "Test profile (do not use)"
	}
	tier := lowercaseTierAdjective(pickTierAdjective(profile, axes))
	res := pickResolutionLabel(axes)
	sourceClass := pickSourceClass(profile, axes)
	parts := []string{tier, res, sourceClass}
	body := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if body != "" {
			body += " "
		}
		body += p
	}
	// "Prefers ... when available" — the verb signals goal, the "when
	// available" suffix signals fallback exists (even strict profiles
	// fall back from Remux to WEB-DL at the same resolution).
	out := "Prefers " + body + " when available"
	// Extra-honesty suffix for the permissive variants. Distinction:
	//   (Combined)   — falls back on RESOLUTION only (2160p → 1080p),
	//                  same quality tiers (Remux + Bluray + WEB) at both
	//   (Alternative) — falls back on QUALITY too (accepts HDTV / DVD /
	//                  SDTV that the strict + Combined profiles reject)
	switch {
	case strings.Contains(profile.Name, "(Alternative)"):
		out += " · with fallback quality"
	case strings.Contains(profile.Name, "(Combined)"):
		out += " · with fallback resolution"
	}
	return out
}

// lowercaseTierAdjective lowercases the first letter of the tier
// adjective so it reads naturally after the "Prefers" prefix
// ("uncompressed", "high-quality") — UNLESS it starts with a proper
// noun (French / German), which stays capitalized regardless of
// sentence position ("Prefers French-dubbed", "Prefers German-language").
func lowercaseTierAdjective(adj string) string {
	if adj == "" {
		return adj
	}
	if strings.HasPrefix(adj, "French") || strings.HasPrefix(adj, "German") {
		return adj
	}
	return strings.ToLower(adj[:1]) + adj[1:]
}

// pickTierAdjective picks the leading word for the tagline. Each branch
// maps from an observable profile property to a defensible label — no
// invented descriptors. Standard fallthrough = "High-quality".
func pickTierAdjective(profile *TrashQualityProfile, axes ProfileAxes) string {
	switch {
	case isSQPProfile(profile.Name):
		return "Streaming-optimized"
	case hasAnimeTuning(profile):
		return "Anime-tuned"
	case strings.HasPrefix(profile.Name, "[French MULTi.VF]"):
		return "French-dubbed"
	case strings.HasPrefix(profile.Name, "[French MULTi.VO]"):
		return "French (original audio)"
	case strings.HasPrefix(profile.Name, "[French VOSTFR]"):
		return "French-subtitled"
	case strings.HasPrefix(profile.Name, "[German]"):
		return "German-language"
	case hasRemuxSource(axes):
		return "Uncompressed"
	}
	return "High-quality"
}

// pickResolutionLabel returns the user-facing resolution token for the
// tagline. "4K UHD" is more recognisable than "2160p" for consumer
// audience; "1080p" stays as-is.
func pickResolutionLabel(axes ProfileAxes) string {
	switch {
	case strings.Contains(axes.Resolution, "2160p"):
		return "4K UHD"
	case strings.Contains(axes.Resolution, "1080p"):
		return "1080p"
	case strings.Contains(axes.Resolution, "720p"):
		return "720p"
	}
	return axes.Resolution
}

// pickSourceClass returns the trailing word/phrase for the tagline. Uses
// the profile's CUTOFF source as the authoritative signal — that's what
// the profile is actually preferring. Falls back to a flat-set heuristic
// only when the cutoff doesn't resolve (e.g. hand-built profile without
// a recognisable cutoff item).
//
// Cutoff-driven examples (these were all "encodes" before the fix):
//
//	Remux 2160p              cutoff = Bluray-2160p Remux  → "Remux"
//	WEB-2160p (Alternative)  cutoff = WEB 2160p           → "WEB-DL"
//	Bluray-2160p             cutoff = Bluray-2160p        → "Bluray"
func pickSourceClass(profile *TrashQualityProfile, axes ProfileAxes) string {
	// Cutoff first. If cutoff is empty OR returns a token outside the
	// switch (e.g. a future TRaSH source category we don't yet map),
	// fall through to the set-based heuristic so the function still
	// produces a meaningful label instead of the bare "encodes" default.
	switch cutoffSource(profile) {
	case "UHD Bluray Remux", "Bluray Remux":
		return "Remux"
	case "WEB-DL", "WEBRip":
		return "WEB-DL"
	case "UHD Bluray", "Bluray":
		return "Bluray"
	}
	// Fallback heuristic — runs for empty cutoff OR unknown cutoff
	// source. The order matters: Remux > WEB-only > generic encodes.
	if hasRemuxSource(axes) {
		return "Remux"
	}
	if isWebOnlyProfile(axes) {
		return "WEB-DL"
	}
	return "encodes"
}

// hasRemuxSource is true when any "Remux" variant appears in the
// derived sources list (UHD Bluray Remux / Bluray Remux).
func hasRemuxSource(axes ProfileAxes) bool {
	for _, s := range axes.Sources {
		if strings.Contains(s, "Remux") {
			return true
		}
	}
	return false
}

// isWebOnlyProfile is true when the profile only accepts WEB-DL /
// WEBRip sources (no Bluray, Remux, HDTV, DVD). Sonarr WEB-1080p /
// WEB-2160p are the canonical examples.
func isWebOnlyProfile(axes ProfileAxes) bool {
	web, nonWeb := false, false
	for _, s := range axes.Sources {
		switch s {
		case "WEB-DL", "WEBRip":
			web = true
		default:
			nonWeb = true
		}
	}
	return web && !nonWeb
}

// typicalSize returns a size-range estimate ("6–15 GB / movie",
// "1–3 GB / episode") chosen from a small lookup based on Remux/WEB
// classification and resolution. NOT computed from TRaSH's
// quality-size JSONs (which give bitrate caps, not real-world typical
// sizes) — these ranges reflect typical observed sizes in practice
// and align with what TRaSH's own setup-quality-profiles.md reports
// for the standard profiles that have a documented _Size:_ line.
func typicalSize(app string, profile *TrashQualityProfile, axes ProfileAxes) string {
	is2160 := strings.Contains(axes.Resolution, "2160p")
	isRemux := hasRemuxSource(axes)
	isWeb := isWebOnlyProfile(axes)

	var movieSize, episodeSize string
	switch {
	case isRemux && is2160:
		movieSize, episodeSize = "40–100 GB", "10–25 GB"
	case isRemux:
		movieSize, episodeSize = "20–40 GB", "3–8 GB"
	case is2160 && isWeb:
		movieSize, episodeSize = "15–50 GB", "5–15 GB"
	case is2160:
		movieSize, episodeSize = "20–60 GB", "5–15 GB"
	case isWeb:
		movieSize, episodeSize = "4–12 GB", "1–3 GB"
	default:
		movieSize, episodeSize = "6–15 GB", "1–3 GB"
	}

	if app == "sonarr" {
		return episodeSize + " / episode"
	}
	return movieSize + " / movie"
}
