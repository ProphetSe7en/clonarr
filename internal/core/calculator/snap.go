package calculator

import "math"

// snap.go rounds raw LP scores to readable steps (5, 10, ...) and re-verifies
// that the requested ranking still holds afterwards. Snapping can in principle
// collapse a tight gap, so the caller is told whether the snapped result is
// still valid and falls back to the unsnapped scores when it is not.

// Violation describes a ranking/partition rule that a score set fails to honour.
type Violation struct {
	Kind   string // "priority" | "equal" | "wanted" | "unwanted"
	TitleA string
	TitleB string
}

// VerifyRanking checks that a score map + minFormatScore reproduce the input's
// same-tier ranking, equal-priority ties and wanted/unwanted partition. An
// empty result means the scores are valid.
func VerifyRanking(in Input, scores map[string]float64, M float64) []Violation {
	const tol = 0.5
	var v []Violation
	parts := make([]Title, 0, len(in.Titles))
	for _, t := range in.Titles {
		if t.QualityAllowed {
			parts = append(parts, t)
		}
	}
	for i := 0; i < len(parts); i++ {
		for j := 0; j < len(parts); j++ {
			if i == j || parts[i].QualityRank != parts[j].QualityRank {
				continue
			}
			a, b := parts[i], parts[j]
			// Unwanted titles take no part in priority ranking (mirrors the
			// constraint builder); only the reject check below applies to them.
			if a.Partition == Unwanted || b.Partition == Unwanted {
				continue
			}
			sa, sb := TitleScore(a, scores), TitleScore(b, scores)
			if a.PriorityGroup < b.PriorityGroup && !(sa > sb+tol) {
				v = append(v, Violation{Kind: "priority", TitleA: a.ID, TitleB: b.ID})
			}
			if a.PriorityGroup == b.PriorityGroup && i < j && math.Abs(sa-sb) > tol {
				v = append(v, Violation{Kind: "equal", TitleA: a.ID, TitleB: b.ID})
			}
		}
	}
	for _, t := range parts {
		s := TitleScore(t, scores)
		if t.Partition == Wanted && !(s >= M-tol) {
			v = append(v, Violation{Kind: "wanted", TitleA: t.ID})
		}
		if t.Partition == Unwanted && !(s <= M-tol) {
			v = append(v, Violation{Kind: "unwanted", TitleA: t.ID})
		}
	}
	return v
}

func snapValue(v float64, step int) float64 {
	if step <= 1 {
		return math.Round(v)
	}
	return math.Round(v/float64(step)) * float64(step)
}

// Snap rounds every CF score to the nearest step and recomputes a
// minFormatScore that cleanly separates wanted from unwanted. It returns the
// snapped result and whether the snapped scores still satisfy the ranking; when
// false the caller should keep the unsnapped result.
func Snap(in Input, r Result, step int) (Result, bool) {
	if !r.Feasible {
		return r, false
	}
	snapped := Result{Feasible: true, CFScores: make(map[string]float64, len(r.CFScores))}
	for c, s := range r.CFScores {
		snapped.CFScores[c] = snapValue(s, step)
	}

	// A pinned minFormatScore stays put; only re-verify the snapped scores.
	if in.MinScore != nil {
		snapped.MinFormatScore = *in.MinScore
		if len(VerifyRanking(in, snapped.CFScores, snapped.MinFormatScore)) != 0 {
			return r, false
		}
		return snapped, true
	}

	// Recompute M to sit between the highest unwanted and lowest wanted score.
	minWanted := math.Inf(1)
	maxUnwanted := math.Inf(-1)
	for _, t := range in.Titles {
		if !t.QualityAllowed {
			continue
		}
		s := TitleScore(t, snapped.CFScores)
		if t.Partition == Wanted && s < minWanted {
			minWanted = s
		}
		if t.Partition == Unwanted && s > maxUnwanted {
			maxUnwanted = s
		}
	}
	switch {
	case math.IsInf(minWanted, 1) && math.IsInf(maxUnwanted, -1):
		snapped.MinFormatScore = 0
	case math.IsInf(minWanted, 1): // only unwanted titles
		snapped.MinFormatScore = snapValue(maxUnwanted, step) + float64(maxInt(step, 1))
	case math.IsInf(maxUnwanted, -1): // only wanted titles
		snapped.MinFormatScore = snapValue(minWanted, step)
	default:
		// Land M on a step at or just below the lowest wanted score, but
		// strictly above the highest unwanted score.
		m := snapValue(minWanted, step)
		if m > minWanted {
			m -= float64(maxInt(step, 1))
		}
		snapped.MinFormatScore = m
	}

	ok := len(VerifyRanking(in, snapped.CFScores, snapped.MinFormatScore)) == 0
	if !ok {
		return r, false
	}
	return snapped, true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
