package calculator

import "sort"

// resolve.go turns an infeasible ranking into a concrete, minimal set of changes
// the user can apply in one click. Two things can go wrong with a ranking:
//
//   - Some releases match an identical Custom Format set in the same tier, so no
//     score can rank one above the other. These MUST be tied (a merge).
//   - A release is simply in the wrong place: its Custom Formats put it above or
//     below where the user dropped it. These need to MOVE, not merge.
//
// Resolve detects both. It first ties the genuinely identical releases, then, if
// the ranking is still impossible, finds the releases that are out of position
// and works out where each one belongs, so the UI never just merges everything.

// Move is a single repositioning suggestion: place TitleID directly below
// AfterID (empty = move to the top), optionally tying them at one priority.
type Move struct {
	TitleID string `json:"titleId"`
	AfterID string `json:"afterId"`
	Tie     bool   `json:"tie"`
	Note    string `json:"note"`
}

// Resolution is the outcome of analysing an infeasible ranking.
type Resolution struct {
	Feasible    bool       // solvable once MergeGroups are tied (no moves needed)
	MergeGroups [][]string // identical-CF releases that must share a priority
	Moves       []Move     // releases that must be repositioned
	Conflict    *Conflict  // set when neither grouping nor moving can fix it
}

// Resolve analyses an infeasible input and returns the merges and moves needed
// to make the requested ranking achievable.
func Resolve(in Input) Resolution {
	parent := map[string]string{}
	origPriority := map[string]int{}
	byID := map[string]Title{}
	var ranked []string
	for _, t := range in.Titles {
		byID[t.ID] = t
		if !t.QualityAllowed || t.Partition == Unwanted {
			continue
		}
		parent[t.ID] = t.ID
		origPriority[t.ID] = t.PriorityGroup
		ranked = append(ranked, t.ID)
	}
	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	// Mandatory merges: same tier + identical CF set cannot be ranked apart.
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			a, b := byID[ranked[i]], byID[ranked[j]]
			if a.QualityRank == b.QualityRank && sameCFSet(a, b) {
				ra, rb := find(ranked[i]), find(ranked[j])
				if ra != rb {
					parent[ra] = rb
				}
			}
		}
	}

	// Tie every merged class onto its lowest (best) priority, then test.
	tied := tieClasses(in, parent, find, origPriority, ranked)
	merges := groupsOf(ranked, find)
	if Calculate(tied).Feasible {
		return Resolution{Feasible: true, MergeGroups: merges}
	}

	// Still impossible: some releases are in the wrong position. Find them and
	// work out where each belongs from a solution with them lifted out.
	movers, remaining := findMovers(tied, ranked)
	if len(movers) == 0 {
		c := ExplainInfeasibility(tied)
		return Resolution{MergeGroups: merges, Conflict: &c}
	}
	moves := computeMoves(tied, movers, Calculate(remaining), ranked)
	return Resolution{MergeGroups: merges, Moves: moves}
}

// tieClasses rebuilds the input with every union class collapsed onto the lowest
// original priority in the class, so merged releases sit at one priority.
func tieClasses(in Input, parent map[string]string, find func(string) string, origPriority map[string]int, ranked []string) Input {
	rep := map[string]int{}
	for _, id := range ranked {
		r := find(id)
		if p, ok := rep[r]; !ok || origPriority[id] < p {
			rep[r] = origPriority[id]
		}
	}
	w := Input{Overrides: in.Overrides, ScoreBound: in.ScoreBound, MinScore: in.MinScore}
	w.Titles = make([]Title, len(in.Titles))
	copy(w.Titles, in.Titles)
	for i := range w.Titles {
		if _, ok := parent[w.Titles[i].ID]; ok {
			w.Titles[i].PriorityGroup = rep[find(w.Titles[i].ID)]
		}
	}
	return w
}

// liftOut removes the given titles from the ranking by parking each in its own
// quality tier, so they generate no ordering constraints with anyone. Used to
// test whether the rest of the ranking becomes feasible without them.
func liftOut(in Input, ids map[string]bool) Input {
	base := 1
	for _, t := range in.Titles {
		if t.QualityRank >= base {
			base = t.QualityRank + 1
		}
	}
	w := Input{Overrides: in.Overrides, ScoreBound: in.ScoreBound, MinScore: in.MinScore}
	w.Titles = make([]Title, len(in.Titles))
	copy(w.Titles, in.Titles)
	n := 0
	for i := range w.Titles {
		if ids[w.Titles[i].ID] {
			w.Titles[i].QualityRank = base + n
			n++
		}
	}
	return w
}

// findMovers finds the releases that are out of position. When a single release
// is to blame it picks the one most out of place (its Custom Formats put it
// farthest from where the user dropped it); otherwise it accumulates movers
// greedily, using the minimal conflict to make progress.
func findMovers(in Input, ranked []string) ([]string, Input) {
	curIdx := map[string]int{}
	for i, id := range ranked {
		curIdx[id] = i
	}

	// Phase 1: if removing one release makes the rest feasible, prefer the most
	// displaced candidate rather than the first one that happens to work.
	bestID, bestDisp := "", -1
	for _, id := range ranked {
		sol := Calculate(liftOut(in, map[string]bool{id: true}))
		if !sol.Feasible {
			continue
		}
		if d := displacement(in, id, sol, ranked, curIdx); d > bestDisp {
			bestID, bestDisp = id, d
		}
	}
	if bestID != "" {
		return []string{bestID}, liftOut(in, map[string]bool{bestID: true})
	}

	// Phase 2: several releases are out of place; accumulate them greedily.
	lifted := map[string]bool{}
	remaining := in
	for len(lifted) < len(ranked) {
		if Calculate(remaining).Feasible {
			return liftedList(ranked, lifted), remaining
		}
		best := ""
		for _, id := range ranked {
			if lifted[id] {
				continue
			}
			trial := map[string]bool{id: true}
			for k := range lifted {
				trial[k] = true
			}
			if Calculate(liftOut(in, trial)).Feasible {
				best = id
				break
			}
		}
		if best == "" {
			// No single extra lift resolves it; take one release from the
			// minimal conflict to make progress, then re-test.
			for _, sc := range FindIIS(remaining) {
				if sc.kind != "priority" {
					continue
				}
				if !lifted[sc.titleA] {
					best = sc.titleA
					break
				}
				if sc.titleB != "" && !lifted[sc.titleB] {
					best = sc.titleB
					break
				}
			}
		}
		if best == "" {
			return nil, in
		}
		lifted[best] = true
		remaining = liftOut(in, lifted)
	}
	if Calculate(remaining).Feasible {
		return liftedList(ranked, lifted), remaining
	}
	return nil, in
}

// displacement measures how far a release's Custom-Format-implied rank is from
// where the user placed it: the number of other releases that outscore it (its
// natural position) versus its current position. A correctly placed release
// scores 0; the bigger the gap, the more out of place it is.
func displacement(in Input, id string, sol Result, ranked []string, curIdx map[string]int) int {
	var self Title
	byID := map[string]Title{}
	for _, t := range in.Titles {
		byID[t.ID] = t
		if t.ID == id {
			self = t
		}
	}
	ms := TitleScore(self, sol.CFScores)
	implied := 0
	for _, other := range ranked {
		if other == id {
			continue
		}
		if TitleScore(byID[other], sol.CFScores) > ms+0.5 {
			implied++
		}
	}
	d := curIdx[id] - implied
	if d < 0 {
		d = -d
	}
	return d
}

func liftedList(ranked []string, lifted map[string]bool) []string {
	out := make([]string, 0, len(lifted))
	for _, id := range ranked {
		if lifted[id] {
			out = append(out, id)
		}
	}
	return out
}

// computeMoves places each moved release where its score actually lands among
// the releases that stayed put, tying it to an equal-scoring neighbour.
func computeMoves(in Input, movers []string, sol Result, ranked []string) []Move {
	byID := map[string]Title{}
	for _, t := range in.Titles {
		byID[t.ID] = t
	}
	moverSet := map[string]bool{}
	for _, m := range movers {
		moverSet[m] = true
	}
	anchors := make([]string, 0, len(ranked))
	for _, id := range ranked {
		if !moverSet[id] {
			anchors = append(anchors, id)
		}
	}
	score := func(id string) float64 { return TitleScore(byID[id], sol.CFScores) }
	sort.SliceStable(anchors, func(i, j int) bool { return score(anchors[i]) > score(anchors[j]) })

	const tol = 0.5
	moves := make([]Move, 0, len(movers))
	for _, m := range movers {
		ms := score(m)
		after := ""
		for _, a := range anchors {
			if score(a) >= ms-tol {
				after = a
			} else {
				break
			}
		}
		tie := after != "" && score(after) <= ms+tol && score(after) >= ms-tol
		note := "This release ranks lower than where you placed it."
		if after == "" {
			note = "This release ranks higher than where you placed it; move it to the top."
		} else if tie {
			note = "This release scores the same as the one it should sit beside; tie them."
		}
		moves = append(moves, Move{TitleID: m, AfterID: after, Tie: tie, Note: note})
	}
	return moves
}

// groupsOf returns the union classes with two or more members, members sorted by
// their position in the ranked list and groups ordered by their first member.
func groupsOf(ranked []string, find func(string) string) [][]string {
	idx := map[string]int{}
	for i, id := range ranked {
		idx[id] = i
	}
	members := map[string][]string{}
	for _, id := range ranked {
		r := find(id)
		members[r] = append(members[r], id)
	}
	var roots []string
	for r, m := range members {
		if len(m) >= 2 {
			sort.Slice(m, func(a, b int) bool { return idx[m[a]] < idx[m[b]] })
			roots = append(roots, r)
		}
	}
	sort.Slice(roots, func(a, b int) bool { return idx[members[roots[a]][0]] < idx[members[roots[b]][0]] })
	out := make([][]string, 0, len(roots))
	for _, r := range roots {
		out = append(out, members[r])
	}
	return out
}
