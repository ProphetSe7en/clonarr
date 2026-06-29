package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"clonarr/internal/core"
	"clonarr/internal/core/titlegen"
)

// handleScoringGenerateTitles builds candidate release titles from a profile's
// scored Custom Formats, so the Scoring Sandbox can populate itself without
// pasted titles. The client sends the profile scores it already fetched (from
// /api/scoring/profile-scores) plus the profileKey; this adds each CF's
// dimension (from its TRaSH cf-group), derives unwanted-format tokens from the
// CF specifications, parses the profile's allowed quality items, and runs the
// generator. The titles are fed back through the normal parse + score pipeline,
// which stays the source of truth for what actually matches.
func (s *Server) handleScoringGenerateTitles(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		AppType string `json:"appType"`
		Scores  []struct {
			TrashID string `json:"trashId"`
			Name    string `json:"name"`
			Score   int    `json:"score"`
		} `json:"scores"`
		// Qualities is the set of quality names the user selected in the picker
		// (leaf names, e.g. "Bluray-2160p", "WEBDL-1080p"). Each becomes a
		// source x resolution context. Empty falls back to the tier-CF grid.
		Qualities []string `json:"qualities"`
		Options   struct {
			IncludeUnwanted     bool `json:"includeUnwanted"`
			Allow1080pHDR       bool `json:"allow1080pHdr"`
			Allow2160pX264      bool `json:"allow2160pX264"`
			AllowLosslessStereo bool `json:"allowLosslessStereo"`
		} `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.AppType == "" {
		writeError(w, 400, "appType is required")
		return
	}

	snap := s.Core.Trash.Snapshot()
	ad := core.SnapshotAppData(snap, req.AppType)

	// trashID -> dimension, from the TRaSH cf-groups (audio-formats -> audio,
	// etc.). clonarr already loads these; we just read them.
	dimOf := map[string]titlegen.Dimension{}
	if ad != nil {
		for _, g := range ad.CFGroups {
			dim := titlegen.DimensionForGroup(g.Name)
			if dim == "" {
				continue
			}
			for _, cf := range g.CustomFormats {
				dimOf[cf.TrashID] = dim
			}
		}
	}

	// Keep scored CFs that map to a release dimension (generatable), and collect
	// the unwanted ones so we can derive penalty tokens from their specs.
	var cfs []titlegen.CF
	var unwanted []titlegen.Unwanted
	for _, sc := range req.Scores {
		// A score-0 CF cannot change the result, so it never generates. The
		// engine applies this to the byDim axes; apply it here too so score-0
		// unwanted (e.g. an unscored 10bit) does not slip into the title set.
		if sc.Score == 0 {
			continue
		}
		var cfDef *core.TrashCF
		if ad != nil {
			cfDef = ad.CustomFormats[sc.TrashID]
		}
		dim := dimOf[sc.TrashID]
		if dim == "" {
			// A release-group CF that is not a member of any TRaSH cf-group
			// (e.g. a profile that scores a single group like TheFarm directly)
			// still generates a group token, so classify it by its spec.
			if cfDef != nil && isReleaseGroupCF(cfDef) {
				dim = titlegen.DimGroup
			}
		}
		if dim == "" {
			continue
		}
		if dim == titlegen.DimUnwanted {
			if cfDef != nil {
				if u, ok := unwantedDecoration(cfDef); ok {
					unwanted = append(unwanted, u)
				}
			}
			continue
		}
		cf := titlegen.CF{Name: sc.Name, TrashID: sc.TrashID, Score: sc.Score, Dim: dim}
		// Non-tier release-group CFs (TheFarm, MainFrame, ...) carry an explicit
		// group name; mine a representative literal so the generator emits a
		// release with that group. Tier CFs keep the tier-placeholder path.
		if dim == titlegen.DimGroup && cfDef != nil && !titlegen.IsTierCF(sc.Name) {
			if g := releaseGroupLiteral(cfDef); g != "" {
				cf.Groups = []string{g}
			}
		}
		cfs = append(cfs, cf)
	}

	// Parse the picked quality names into source x resolution contexts.
	var qctx []titlegen.QualityCtx
	seenQ := map[titlegen.QualityCtx]bool{}
	for _, name := range req.Qualities {
		if q, ok := parseQualityName(name); ok && !seenQ[q] {
			seenQ[q] = true
			qctx = append(qctx, q)
		}
	}

	// Codec axis. Only vary h264 vs h265 when the profile scores a codec CF
	// (x265, x265 (HD), x264, HEVC, ...); otherwise the two would score the same
	// and just double the list. The CFs define which codec scores how, so this
	// is derived, not a manual pick.
	vcodecs := []string{"h265"}
	for _, sc := range req.Scores {
		// Only a SCORED codec CF justifies an h264 variant; a score-0 codec CF
		// would just double the list with titles that score identically.
		if reCodecCF.MatchString(sc.Name) && sc.Score != 0 {
			vcodecs = []string{"h265", "h264"}
			break
		}
	}

	opts := titlegen.Options{
		Allow1080pHDR:       req.Options.Allow1080pHDR,
		Allow2160pX264:      req.Options.Allow2160pX264,
		AllowLosslessStereo: req.Options.AllowLosslessStereo,
		// Unwanted generation is driven by the picker selection: if the caller
		// included any unwanted-axis CF in scores[], generate the penalties for
		// them. (The old global "Include unwanted" toggle is now just the
		// per-axis Unwanted picker.)
		IncludeUnwanted: len(unwanted) > 0,
		Unwanted:        unwanted,
		Qualities:       qctx,
		VCodecs:         vcodecs,
		Sources:         []string{"WEB-DL"}, // fallback only when no qualities/tiers resolve
	}

	titles, stats := titlegen.GenerateWithStats(cfs, opts)
	writeJSON(w, map[string]any{
		"titles":         titles,
		"generatableCFs": len(cfs),
		"stats":          stats,
	})
}

// isReleaseGroupCF reports whether a CF matches by release group (has a
// non-negated ReleaseGroupSpecification). Used to classify a standalone group
// CF (e.g. TheFarm) that a profile scores directly without it belonging to a
// TRaSH release-group cf-group.
func isReleaseGroupCF(cf *core.TrashCF) bool {
	for _, sp := range cf.Specifications {
		if sp.Implementation == "ReleaseGroupSpecification" && !sp.Negate {
			return true
		}
	}
	return false
}

// releaseGroupLiteral lifts a representative release-group name from a CF's
// ReleaseGroupSpecification(s) (e.g. "^(TheFarm)$" -> "TheFarm"), so the
// generator can emit a title carrying that group and the CF gets scored.
// Returns "" when no clean literal can be extracted.
func releaseGroupLiteral(cf *core.TrashCF) string {
	for _, sp := range cf.Specifications {
		if sp.Implementation != "ReleaseGroupSpecification" || sp.Negate {
			continue
		}
		if lit, ok := extractLiteral(core.SpecValue(sp.Fields)); ok {
			return strings.TrimPrefix(lit, "-")
		}
	}
	return ""
}

// curatedUnwanted holds the few unwanted CFs whose token needs hand-placement
// (tech tokens or a full-body override) rather than a literal lifted from the
// release-title/group regex.
func unwantedDecoration(cf *core.TrashCF) (titlegen.Unwanted, bool) {
	u := titlegen.Unwanted{Name: cf.Name}
	switch cf.Name {
	case "BR-DISK":
		// A disc release with no resolution/HEVC (the CF excludes those).
		u.Body = "COMPLETE.BLURAY.AVC"
		return u, true
	case "AV1":
		u.Codec = "AV1" // replaces h265 rather than sitting next to it
		return u, true
	case "3D":
		u.Suffix = ".3D"
		return u, true
	case "10bit":
		u.Suffix = ".10bit"
		return u, true
	}

	// Otherwise lift a literal from the specs. Collect the positive (non-negated)
	// title and group patterns separately, and watch for the No-RlsGroup catch-all.
	var titleVals, groupVals []string
	noGroup := false
	for _, sp := range cf.Specifications {
		val := core.SpecValue(sp.Fields)
		switch sp.Implementation {
		case "ReleaseGroupSpecification":
			if sp.Negate {
				if strings.TrimSpace(val) == "." {
					noGroup = true // No-RlsGroup: matches when there is no group
				}
				continue
			}
			groupVals = append(groupVals, val)
		case "ReleaseTitleSpecification":
			if !sp.Negate {
				titleVals = append(titleVals, val)
			}
		}
	}

	// A CF that has title patterns is title/context based (e.g. it also needs
	// HDR present), so a bare release-group token would be a false trigger.
	// Prefer a clean title literal; only fall back to a group name when the CF
	// is a pure group blocklist (LQ, Bad Dual Groups, ...).
	if len(titleVals) > 0 {
		for _, v := range titleVals {
			if lit, ok := extractLiteral(v); ok {
				u.Suffix = titleTagSuffix(lit)
				return u, true
			}
		}
		return u, false // title based but no clean literal -> can't express it
	}
	for _, v := range groupVals {
		if lit, ok := extractLiteral(v); ok {
			u.Suffix = "-" + strings.TrimPrefix(lit, "-")
			return u, true
		}
	}
	if noGroup {
		u.NoGroup = true
		return u, true
	}
	return u, false
}

// titleTagSuffix decides how a release-title literal attaches to a base title:
// a "-GROUP" suffix stays as-is, a "[tag]" gets a leading space, anything else
// becomes a dotted token.
func titleTagSuffix(lit string) string {
	switch {
	case strings.HasPrefix(lit, "-"):
		return lit
	case strings.HasPrefix(lit, "["):
		return " " + lit
	default:
		return "." + lit
	}
}

var (
	reCleanLiteral = regexp.MustCompile(`^[\[\-]?[A-Za-z0-9][A-Za-z0-9._\[\]\-]*$`)
	// reCodecCF matches a CF name that scores codecs differently, so the
	// generator should vary h264 vs h265 (x265, x265 (HD), x264, HEVC, AVC).
	reCodecCF = regexp.MustCompile(`(?i)(x[ ._-]?26[45]|h[ ._-]?26[45]|hevc|\bavc\b|\b26[45]\b)`)
)

// extractLiteral lifts a usable literal out of a CF's regex. It accepts only a
// FLAT alternation group whose every member is a clean literal (e.g.
// `\b(-CAKES|-GGEZ)` -> "-CAKES", `^(YIFY)$` -> "YIFY"), or a bare literal with
// no regex structure (`-4P\b` -> "-4P", `\[rarbg\]` -> "[rarbg]"). Nested or
// complex patterns (lookarounds, char classes, quantifiers) are rejected, so we
// never mine a fragment like "and" out of a structured regex.
func extractLiteral(rx string) (string, bool) {
	for _, grp := range flatGroups(rx) {
		// First clean member of a flat group. flatGroups already rejected nested
		// / structured patterns, so a stray member like "Extended[ ._-]Clip" only
		// gets skipped, while a sibling like "Extras" is still usable.
		for _, m := range strings.Split(grp, "|") {
			if lit, ok := cleanLiteral(m); ok {
				return lit, true
			}
		}
	}
	// No usable group: accept only a value with no regex structure at all.
	if !strings.ContainsAny(rx, "()|") {
		if lit, ok := cleanLiteral(rx); ok {
			return lit, true
		}
	}
	return "", false
}

// flatGroups returns the contents of top-level `(...)` groups that contain no
// nested parentheses and are not lookarounds. A nested or lookaround group is
// skipped entirely, so a structured pattern yields no flat group.
func flatGroups(rx string) []string {
	var out []string
	depth, start := 0, -1
	nested, lookaround := false, false
	for i := 0; i < len(rx); i++ {
		switch rx[i] {
		case '(':
			if depth == 0 {
				start, nested = i, false
				lookaround = i+1 < len(rx) && rx[i+1] == '?'
			} else {
				nested = true
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
				if depth == 0 {
					if start >= 0 && !nested && !lookaround {
						out = append(out, rx[start+1:i])
					}
					start = -1
				}
			}
		}
	}
	return out
}

func cleanLiteral(s string) (string, bool) {
	s = strings.NewReplacer(`\b`, "", `\B`, "", `^`, "", `$`, "").Replace(s)
	s = strings.Trim(s, "()")
	s = strings.NewReplacer(`\[`, "[", `\]`, "]", `\.`, ".", `\-`, "-", `\_`, "_").Replace(s)
	s = strings.TrimSpace(s)
	if !reCleanLiteral.MatchString(s) {
		return "", false
	}
	// Need at least two alphanumerics so we don't grab regex debris ("-", "E").
	n := 0
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			n++
		}
	}
	if n < 2 {
		return "", false
	}
	return s, true
}

var reResolution = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|576p|480p)\b`)

// parseQualityName turns an Arr quality (or quality-group) name into a release
// source + resolution token, e.g. "Bluray-1080p" -> {Bluray, 1080p},
// "WEB 2160p" -> {WEB-DL, 2160p}. ok=false for qualities we don't generate
// (DVD, SDTV, CAM, Raw-HD, ...).
func parseQualityName(name string) (titlegen.QualityCtx, bool) {
	low := strings.ToLower(name)
	res := strings.ToLower(reResolution.FindString(name))
	if res == "" {
		return titlegen.QualityCtx{}, false
	}
	var src string
	switch {
	case strings.Contains(low, "remux"):
		src = "Remux"
	case strings.Contains(low, "bluray") || strings.Contains(low, "blu-ray") || strings.Contains(low, "br-disk"):
		src = "Bluray"
	case strings.Contains(low, "webrip"):
		src = "WEBRip"
	case strings.Contains(low, "webdl") || strings.Contains(low, "web-dl") || strings.Contains(low, "web"):
		src = "WEB-DL"
	case strings.Contains(low, "hdtv"):
		src = "HDTV"
	default:
		return titlegen.QualityCtx{}, false
	}
	return titlegen.QualityCtx{Source: src, Resolution: res}, true
}

