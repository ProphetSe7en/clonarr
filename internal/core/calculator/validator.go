package calculator

import "fmt"

// validator.go implements the Test-button checks (spec "Validation rules"). It
// catches the structural problems that have clear, specific fixes BEFORE the LP
// runs, so the user gets actionable errors rather than a generic infeasibility.
// LP-only conflicts (override-vs-priority, subtle overlaps) are left to iis.go.

// Issue is one validation finding. Severity is "error" or "warning"; Calculate
// should be blocked while any error remains.
type Issue struct {
	Level    int      `json:"level"`
	Severity string   `json:"severity"`
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Titles   []string `json:"titles"`
}

// Validate runs the pre-calculation checks and returns all findings. Title
// parsing (Level 1) happens at the API layer; this works on already-parsed
// titles. A title with QualityRank < 0 is treated as an unknown quality.
func Validate(in Input) []Issue {
	var issues []Issue
	// Cross-quality ranking only concerns titles whose quality is actually
	// enabled in the profile; disallowed-quality titles are handled by the
	// wanted-disallowed / unwanted-disallowed checks below.
	allowed := make([]Title, 0, len(in.Titles))
	for _, t := range in.Titles {
		if t.QualityAllowed {
			allowed = append(allowed, t)
		}
	}

	// Level 2: quality known to the profile.
	for _, t := range in.Titles {
		if t.QualityRank < 0 {
			issues = append(issues, Issue{Level: 2, Severity: "error", Kind: "unknown-quality",
				Message: fmt.Sprintf("Release %q has a quality the bound profile does not recognise.", t.ID),
				Titles:  []string{t.ID}})
		}
	}

	// Level 3: cross-quality priority conflict - a preferred release sitting in
	// a lower (worse) quality tier than the one it should beat.
	for i := 0; i < len(allowed); i++ {
		for j := 0; j < len(allowed); j++ {
			if i == j {
				continue
			}
			a, b := allowed[i], allowed[j]
			if a.PriorityGroup < b.PriorityGroup && a.QualityRank > b.QualityRank {
				issues = append(issues, Issue{Level: 3, Severity: "error", Kind: "cross-quality",
					Message: fmt.Sprintf("You ranked %q above %q, but %q is in a lower quality tier. Quality always wins, so this ordering is impossible.", a.ID, b.ID, a.ID),
					Titles:  []string{a.ID, b.ID}})
			}
		}
	}

	// Level 4: clusters of titles with identical (quality tier, CF set) but
	// different priorities.
	type cluster struct {
		ids    []string
		groups map[int]bool
	}
	clusters := map[string]*cluster{}
	for _, t := range in.Titles {
		// Only ranked titles can be "impossible to rank apart": disallowed
		// qualities are out, and unwanted titles aren't ranked at all.
		if !t.QualityAllowed || t.Partition == Unwanted {
			continue
		}
		key := fmt.Sprintf("%d|%s", t.QualityRank, joinCFs(sharedCFList(t)))
		c := clusters[key]
		if c == nil {
			c = &cluster{groups: map[int]bool{}}
			clusters[key] = c
		}
		c.ids = append(c.ids, t.ID)
		c.groups[t.PriorityGroup] = true
	}
	for _, c := range clusters {
		if len(c.ids) >= 2 && len(c.groups) >= 2 {
			issues = append(issues, Issue{Level: 4, Severity: "error", Kind: "identical-cfs",
				Message: fmt.Sprintf("These releases match the same Custom Formats in the same quality tier but have different priorities, which cannot be ranked apart: %s.", joinCFs(c.ids)),
				Titles:  c.ids})
		}
	}

	// Level 5: wanted release in a disallowed quality.
	for _, t := range in.Titles {
		if t.Partition == Wanted && !t.QualityAllowed {
			issues = append(issues, Issue{Level: 5, Severity: "error", Kind: "wanted-disallowed",
				Message: fmt.Sprintf("Release %q is marked Wanted but its quality is not enabled in the profile, so it would be rejected regardless of score.", t.ID),
				Titles:  []string{t.ID}})
		}
	}

	// Level 6: an obvious wanted-vs-unwanted CF-set overlap (same tier).
	for i := 0; i < len(in.Titles); i++ {
		for j := i + 1; j < len(in.Titles); j++ {
			a, b := in.Titles[i], in.Titles[j]
			if a.QualityRank != b.QualityRank || !a.QualityAllowed || !b.QualityAllowed {
				continue
			}
			if a.Partition != b.Partition && sameCFSet(a, b) {
				issues = append(issues, Issue{Level: 6, Severity: "error", Kind: "partition-overlap",
					Message: fmt.Sprintf("Wanted and Unwanted releases %q and %q share the same Custom Formats in the same quality tier, so no minimum score can separate them.", a.ID, b.ID),
					Titles:  []string{a.ID, b.ID}})
			}
		}
	}

	// Level 7: informational warnings.
	for _, t := range in.Titles {
		if t.Partition == Unwanted && !t.QualityAllowed {
			issues = append(issues, Issue{Level: 7, Severity: "warning", Kind: "unwanted-disallowed",
				Message: fmt.Sprintf("Release %q is already rejected by the quality gate, so it does not affect the calculation.", t.ID),
				Titles:  []string{t.ID}})
		}
		if len(cfSet(t.MatchedCFs)) == 0 && t.QualityAllowed {
			issues = append(issues, Issue{Level: 7, Severity: "warning", Kind: "no-cfs",
				Message: fmt.Sprintf("Release %q matched no Custom Formats, so it provides no ranking signal beyond its quality.", t.ID),
				Titles:  []string{t.ID}})
		}
	}

	return issues
}

// HasErrors reports whether any issue blocks calculation.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == "error" {
			return true
		}
	}
	return false
}
