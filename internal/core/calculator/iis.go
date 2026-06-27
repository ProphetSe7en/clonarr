package calculator

import "fmt"

// iis.go turns an infeasible calculator problem into something a user can act
// on. It extracts an Irreducible Infeasible Subset (IIS) of the ranking /
// partition / override constraints via a deletion filter, then translates the
// surviving constraints into a plain-language explanation with suggested fixes.

// Conflict is a human-readable description of why a calculation is impossible.
type Conflict struct {
	Message     string   `json:"message"`
	Suggestions []string `json:"suggestions"`
	Involved    []string `json:"involved"` // title ids / CF ids referenced, for UI highlighting
}

func semanticsFor(in Input) []semConstraint {
	_, sVar, mVar, _ := buildBase(in)
	return buildSemantics(in, sVar, mVar)
}

// FindIIS returns a minimal subset of semantic constraints that together remain
// infeasible. Returns nil when the full problem is feasible. Deletion filter:
// drop each constraint in turn; if the rest is still infeasible the constraint
// was redundant, otherwise it is essential and kept.
func FindIIS(in Input) []semConstraint {
	sems := semanticsFor(in)
	if solveWith(in, sems).Feasible {
		return nil
	}
	remaining := append([]semConstraint{}, sems...)
	i := 0
	for i < len(remaining) {
		trial := make([]semConstraint, 0, len(remaining)-1)
		trial = append(trial, remaining[:i]...)
		trial = append(trial, remaining[i+1:]...)
		if !solveWith(in, trial).Feasible {
			remaining = trial // constraint i not needed for infeasibility
		} else {
			i++ // constraint i is essential
		}
	}
	return remaining
}

// ExplainInfeasibility produces a user-facing Conflict for an infeasible input,
// or a zero Conflict (empty Message) when the input is actually feasible.
func ExplainInfeasibility(in Input) Conflict {
	iis := FindIIS(in)
	if len(iis) == 0 {
		return Conflict{}
	}

	byID := map[string]Title{}
	for _, t := range in.Titles {
		byID[t.ID] = t
	}
	involved := map[string]bool{}
	var hasPriority, hasEqual, hasWanted, hasUnwanted, hasOverride bool
	var overrideCFs []string
	for _, sc := range iis {
		switch sc.kind {
		case "priority":
			hasPriority = true
		case "equal":
			hasEqual = true
		case "wanted":
			hasWanted = true
		case "unwanted":
			hasUnwanted = true
		case "override":
			hasOverride = true
			overrideCFs = append(overrideCFs, sc.cf)
		}
		if sc.titleA != "" {
			involved[sc.titleA] = true
		}
		if sc.titleB != "" {
			involved[sc.titleB] = true
		}
		if sc.cf != "" {
			involved[sc.cf] = true
		}
	}

	// Pattern 1: two titles, same tier, share an identical CF set.
	if (hasPriority || hasEqual) && !hasOverride {
		for _, sc := range iis {
			if sc.kind != "priority" || sc.titleB == "" {
				continue
			}
			a, b := byID[sc.titleA], byID[sc.titleB]
			if sameCFSet(a, b) {
				shared := sharedCFList(a)
				return Conflict{
					Message: fmt.Sprintf("Releases %q and %q match the same Custom Formats (%s) and sit in the same quality tier, so no score combination can rank one above the other.",
						sc.titleA, sc.titleB, joinCFs(shared)),
					Suggestions: []string{
						"Give both releases the same priority.",
						fmt.Sprintf("Remove %q or %q from the list.", sc.titleA, sc.titleB),
						"Add a release that exposes a Custom Format unique to one of them.",
					},
					Involved: keys(involved),
				}
			}
		}
	}

	// Pattern 2: a wanted and an unwanted release cannot be separated.
	if hasWanted && hasUnwanted {
		return Conflict{
			Message: "A wanted release and an unwanted release share the same Custom Formats in the same quality tier, so no minimum score can accept one and reject the other.",
			Suggestions: []string{
				"Move one of them to the other side of the Wanted/Unwanted split.",
				"Add a Custom Format override that breaks the overlap (exclude or boost a shared format).",
				"Remove one of the two releases.",
			},
			Involved: keys(involved),
		}
	}

	// Pattern 3: a per-CF override fights a ranking requirement.
	if hasOverride && (hasPriority || hasWanted || hasUnwanted) {
		return Conflict{
			Message: fmt.Sprintf("A Custom Format override (%s) conflicts with the ranking you asked for. The only thing separating two releases is a format the override pins.",
				joinCFs(overrideCFs)),
			Suggestions: []string{
				"Remove or relax the override (for example use a small boost instead of exclude).",
				"Equalise the priority of the two affected releases.",
			},
			Involved: keys(involved),
		}
	}

	// Fallback: report the surviving constraints generically.
	return Conflict{
		Message:     "The requested ranking cannot be satisfied. A small set of priority, partition or override rules conflict.",
		Suggestions: []string{"Relax one of the involved priorities, overrides, or the Wanted/Unwanted split."},
		Involved:    keys(involved),
	}
}

func sameCFSet(a, b Title) bool {
	sa, sb := cfSet(a.MatchedCFs), cfSet(b.MatchedCFs)
	if len(sa) != len(sb) {
		return false
	}
	for c := range sa {
		if !sb[c] {
			return false
		}
	}
	return true
}

func sharedCFList(t Title) []string {
	out := make([]string, 0)
	for c := range cfSet(t.MatchedCFs) {
		out = append(out, c)
	}
	sortStrings(out)
	return out
}

func joinCFs(cfs []string) string {
	if len(cfs) == 0 {
		return "none"
	}
	s := cfs[0]
	for _, c := range cfs[1:] {
		s += ", " + c
	}
	return s
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
