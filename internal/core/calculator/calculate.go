package calculator

import "sort"

// calculate.go is the calculator's domain layer: it translates a ranked set of
// release titles (with their quality rank, matched Custom Formats, priority and
// wanted/unwanted partition) into the linear program built in lp.go, solves it,
// and returns Custom Format scores plus a recommended minFormatScore that
// reproduce the requested ranking.
//
// Quality rank convention (matches Sonarr/Radarr): LOWER rank index = higher
// quality priority. Quality dominance means a higher-quality release always
// wins regardless of CF score, so only SAME-tier pairs generate LP constraints;
// cross-tier conflicts are the validator's job (see spec Hard Limits §2).
//
// Constraints are built as labelled "semantic" descriptors so the IIS deletion
// filter (iis.go) can remove them one at a time to find a minimal conflict.

// Partition marks whether a title should be accepted (Wanted) or rejected
// (Unwanted) by the resulting profile.
type Partition int

const (
	Wanted Partition = iota
	Unwanted
)

// Title is one ranked release in a calculator session.
type Title struct {
	ID             string
	QualityRank    int      // lower = higher quality priority
	QualityAllowed bool     // is this quality enabled in the bound profile
	MatchedCFs     []string // CF ids matched (language/flag CFs already stripped)
	PriorityGroup  int      // 1 = most preferred; equal value = tie
	Partition      Partition
}

// Override pins a single CF's score. Type is "exclude" | "boost" | "unwanted".
type Override struct {
	Type string
	Min  float64 // boost:  s_c >= Min
	Max  float64 // unwanted: s_c <= Max (typically negative)
}

// Input is a calculator session reduced to what the solver needs.
type Input struct {
	Titles     []Title
	Overrides  map[string]Override
	ScoreBound float64  // |s_c| <= ScoreBound; defaults to 10000 when zero
	MinScore   *float64 // when set, pins minFormatScore to this value (the
	// profile's own minimum): wanted releases must reach it, unwanted stay
	// below it, CF scores solve against it. nil = solve freely.
}

// Result holds calculated scores or an infeasibility flag.
type Result struct {
	Feasible       bool
	CFScores       map[string]float64
	MinFormatScore float64
}

// semConstraint is one ranking/partition/override constraint, carrying both the
// LP coefficients and the metadata needed to translate an infeasibility back to
// user-facing terms.
type semConstraint struct {
	kind   string // "priority" | "equal" | "wanted" | "unwanted" | "override"
	titleA string
	titleB string
	cf     string
	coef   map[int]float64
	sense  Sense
	rhs    float64
}

func cfSet(cfs []string) map[string]bool {
	s := make(map[string]bool, len(cfs))
	for _, c := range cfs {
		if c != "" {
			s[c] = true
		}
	}
	return s
}

// cfUniverse returns the sorted set of CFs that participate: every CF matched
// by an allowed title, plus any CF pinned by an override.
func cfUniverse(in Input) []string {
	u := map[string]bool{}
	for _, t := range in.Titles {
		if !t.QualityAllowed {
			continue
		}
		for c := range cfSet(t.MatchedCFs) {
			u[c] = true
		}
	}
	for c := range in.Overrides {
		u[c] = true
	}
	list := make([]string, 0, len(u))
	for c := range u {
		list = append(list, c)
	}
	sortStrings(list)
	return list
}

// buildBase creates the LP with score/aux variables, the |score| objective and
// the sanity bounds - everything that is always present, deterministically
// indexed (so semantic-constraint coefficients stay valid across rebuilds).
func buildBase(in Input) (p *Problem, sVar map[string]int, mVar int, cfList []string) {
	bound := in.ScoreBound
	if bound <= 0 {
		bound = 10000
	}
	cfList = cfUniverse(in)
	p = NewProblem()
	sVar = make(map[string]int, len(cfList))
	for _, c := range cfList {
		s := p.AddVar(true) // score, free
		tt := p.AddVar(false)
		sVar[c] = s
		p.SetObjective(tt, 1)
		p.AddConstraint(map[int]float64{tt: 1, s: -1}, GE, 0) // t >= s
		p.AddConstraint(map[int]float64{tt: 1, s: 1}, GE, 0)  // t >= -s
		p.AddConstraint(map[int]float64{s: 1}, LE, bound)
		p.AddConstraint(map[int]float64{s: 1}, GE, -bound)
	}
	mVar = p.AddVar(true) // minFormatScore, free
	return p, sVar, mVar, cfList
}

// buildSemantics produces the labelled ranking/partition/override constraints.
//
// Within a quality tier, the wanted titles are sorted by priority and only
// CONSECUTIVE pairs are constrained (each rank > the next, equal priorities tied
// together). Transitivity makes this equivalent to the full pairwise set but
// reduces the constraint count from O(n^2) to O(n), which keeps Calculate, the
// IIS deletion filter and the auto-grouping loop fast on large rankings.
func buildSemantics(in Input, sVar map[string]int, mVar int) []semConstraint {
	scoreCoef := func(t Title) map[int]float64 {
		m := map[int]float64{}
		for c := range cfSet(t.MatchedCFs) {
			if v, ok := sVar[c]; ok {
				m[v] += 1
			}
		}
		return m
	}
	diffCoef := func(a, b Title) map[int]float64 {
		coef := scoreCoef(a)
		for k, v := range scoreCoef(b) {
			coef[k] -= v
		}
		return coef
	}

	// Bucket wanted, allowed titles by tier; unwanted titles take no part in the
	// ranking (only the reject constraint below applies to them).
	byTier := map[int][]Title{}
	for _, t := range in.Titles {
		if !t.QualityAllowed || t.Partition == Unwanted {
			continue
		}
		byTier[t.QualityRank] = append(byTier[t.QualityRank], t)
	}
	tiers := make([]int, 0, len(byTier))
	for r := range byTier {
		tiers = append(tiers, r)
	}
	sort.Ints(tiers)

	var out []semConstraint
	for _, r := range tiers {
		ts := byTier[r]
		sort.SliceStable(ts, func(i, j int) bool { return ts[i].PriorityGroup < ts[j].PriorityGroup })
		for k := 1; k < len(ts); k++ {
			a, b := ts[k-1], ts[k] // a has the lower (better) priority number
			if a.PriorityGroup == b.PriorityGroup {
				out = append(out, semConstraint{kind: "equal", titleA: a.ID, titleB: b.ID, coef: diffCoef(a, b), sense: EQ, rhs: 0})
			} else {
				out = append(out, semConstraint{kind: "priority", titleA: a.ID, titleB: b.ID, coef: diffCoef(a, b), sense: GE, rhs: 1})
			}
		}
	}
	for _, t := range in.Titles {
		if !t.QualityAllowed {
			continue
		}
		coef := scoreCoef(t)
		coef[mVar] -= 1
		if t.Partition == Wanted {
			out = append(out, semConstraint{kind: "wanted", titleA: t.ID, coef: coef, sense: GE, rhs: 0})
		} else {
			out = append(out, semConstraint{kind: "unwanted", titleA: t.ID, coef: coef, sense: LE, rhs: -1})
		}
	}
	for c, ov := range in.Overrides {
		v, ok := sVar[c]
		if !ok {
			continue
		}
		switch ov.Type {
		case "exclude":
			out = append(out, semConstraint{kind: "override", cf: c, coef: map[int]float64{v: 1}, sense: EQ, rhs: 0})
		case "boost":
			out = append(out, semConstraint{kind: "override", cf: c, coef: map[int]float64{v: 1}, sense: GE, rhs: ov.Min})
		case "unwanted":
			out = append(out, semConstraint{kind: "override", cf: c, coef: map[int]float64{v: 1}, sense: LE, rhs: ov.Max})
		}
	}
	return out
}

// solveWith builds the base, applies the given semantic constraints, solves and
// maps the solution back to CF scores. Used by Calculate (all semantics) and by
// the IIS deletion filter (subsets).
func solveWith(in Input, sems []semConstraint) Result {
	p, sVar, mVar, cfList := buildBase(in)
	if in.MinScore != nil {
		// Pin minFormatScore to the profile's own minimum so CF scores solve
		// against a realistic threshold instead of collapsing to 0/1.
		p.AddConstraint(map[int]float64{mVar: 1}, EQ, *in.MinScore)
	}
	for _, sc := range sems {
		p.AddConstraint(sc.coef, sc.sense, sc.rhs)
	}
	sol := p.Solve()
	if !sol.Feasible {
		return Result{Feasible: false}
	}
	scores := make(map[string]float64, len(cfList))
	for _, c := range cfList {
		scores[c] = sol.X[sVar[c]]
	}
	return Result{Feasible: true, CFScores: scores, MinFormatScore: sol.X[mVar]}
}

// Calculate builds and solves the LP for the given input.
func Calculate(in Input) Result {
	_, sVar, mVar, _ := buildBase(in)
	return solveWith(in, buildSemantics(in, sVar, mVar))
}

// TitleScore returns the total CF score a title would receive under the given
// score map.
func TitleScore(t Title, scores map[string]float64) float64 {
	total := 0.0
	for c := range cfSet(t.MatchedCFs) {
		total += scores[c]
	}
	return total
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
