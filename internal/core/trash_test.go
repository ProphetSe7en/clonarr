package core

import "testing"

// CompareCFCategories drives the unified group-sort across both backend and
// frontend (the JS _compareCFCategories mirrors this). The contract:
//   - Tier 0: regular TRaSH categories (alphabetical within tier)
//   - Tier 1: SQP-prefix categories
//   - Tier 2: "Other" / unrecognised
//   - Tier 3: Custom
// Within tier, alphabetical on category name.
func TestCompareCFCategories_Tiering(t *testing.T) {
	cases := []struct {
		a, b string
		want int // -1, 0, +1
		desc string
	}{
		// Within tier 0 — pure alphabetical
		{"Audio", "HDR Formats", -1, "tier-0 alphabetical: A before H"},
		{"HDR Formats", "Audio", 1, "tier-0 alphabetical: reverse"},
		{"Audio", "Audio", 0, "same category equal"},
		// Tier 0 wins over tier 1
		{"Anime", "SQP", -1, "tier-0 before tier-1 SQP"},
		{"SQP-1", "Audio", 1, "tier-1 SQP-1 after tier-0 Audio"},
		// Tier 0 wins over tier 3 Custom
		{"Audio", "Custom", -1, "tier-0 before tier-3 Custom"},
		{"Custom", "Audio", 1, "Custom after regular category"},
		// Tier 1: all SQP-prefix grouped together, alphabetical within
		{"SQP", "SQP-1", -1, "SQP before SQP-1 alphabetical"},
		{"SQP-1", "SQP-4 (MA Hybrid)", -1, "alphabetical within SQP tier"},
		{"SQP-4 (MA Hybrid) Optional", "SQP-1", 1, "alphabetical reverse"},
		// Other goes between SQP and Custom
		{"SQP-anything", "Other", -1, "SQP-anything before Other"},
		{"Other", "Custom", -1, "Other before Custom"},
		// Empty-string treated as Other
		{"", "Custom", -1, "empty string treated as Other, before Custom"},
		{"Audio", "", -1, "tier-0 before empty-string Other"},
	}
	for _, c := range cases {
		got := CompareCFCategories(c.a, c.b)
		// Normalise to -1/0/+1
		switch {
		case got < 0:
			got = -1
		case got > 0:
			got = 1
		}
		if got != c.want {
			t.Errorf("%s: CompareCFCategories(%q, %q) = %d, want %d",
				c.desc, c.a, c.b, got, c.want)
		}
	}
}

// CompareCFGroups drives sort across cf-groups using the TRaSH `group` integer.
// The contract:
//   - Tier 3 (custom): user-authored groups always last
//   - Tier 1 (has explicit Group): sorted by integer, alphabetical tiebreak
//   - Tier 2 (no Group): alphabetical fallback in middle band
func TestCompareCFGroups_Tiering(t *testing.T) {
	intp := func(n int) *int { return &n }

	cases := []struct {
		desc                                   string
		aName                                  string
		aGroup                                 *int
		aCustom                                bool
		bName                                  string
		bGroup                                 *int
		bCustom                                bool
		want                                   int // -1, 0, +1
	}{
		// Both have Group — compare by integer
		{"both have group, lower wins", "[Audio]", intp(1), false, "[German]", intp(11), false, -1},
		{"both have group, higher loses", "[German]", intp(11), false, "[Audio]", intp(1), false, 1},
		{"both have group, same value → alphabetical", "[Banana]", intp(5), false, "[Apple]", intp(5), false, 1},
		{"both have group, same value, alpha A wins", "[Apple]", intp(5), false, "[Banana]", intp(5), false, -1},
		// Group=0 is a real value, not "absent" — lowest possible
		{"group=0 is highest priority, beats group=1", "[Z]", intp(0), false, "[A]", intp(1), false, -1},
		{"group=0 distinguishable from nil", "[Z]", intp(0), false, "[A]", nil, false, -1},
		// Tier 1 vs Tier 2 — explicit group always beats no-group
		{"has-group beats no-group", "[Anime]", intp(81), false, "[Other]", nil, false, -1},
		{"no-group loses to has-group", "[Other]", nil, false, "[Anime]", intp(81), false, 1},
		// Both Tier 2 — alphabetical
		{"both no-group, alphabetical", "[Apple]", nil, false, "[Banana]", nil, false, -1},
		{"both no-group, alphabetical reverse", "[Banana]", nil, false, "[Apple]", nil, false, 1},
		// Custom always last regardless of Group
		{"custom always last vs has-group", "[My Custom]", intp(1), true, "[Other]", nil, false, 1},
		{"custom always last vs no-group", "[My Custom]", nil, true, "[Other]", nil, false, 1},
		{"non-custom beats custom even when custom has Group=0", "[Other]", nil, false, "[My Custom]", intp(0), true, -1},
		// Both custom — alphabetical fallback
		{"both custom alphabetical", "[A]", nil, true, "[B]", nil, true, -1},
		// Equal
		{"identical → 0", "[Same]", intp(5), false, "[Same]", intp(5), false, 0},
	}
	for _, c := range cases {
		got := CompareCFGroups(c.aName, c.aGroup, c.aCustom, c.bName, c.bGroup, c.bCustom)
		switch {
		case got < 0:
			got = -1
		case got > 0:
			got = 1
		}
		if got != c.want {
			t.Errorf("%s: CompareCFGroups(%q,%v,%v, %q,%v,%v) = %d, want %d",
				c.desc, c.aName, c.aGroup, c.aCustom, c.bName, c.bGroup, c.bCustom, got, c.want)
		}
	}
}

// Ensure the SQP detection is case-insensitive — TRaSH normally uses the
// upper-case "[SQP]" prefix but defensive coding for any drift.
func TestCategoryTier_SQPCaseInsensitive(t *testing.T) {
	cases := map[string]int{
		"SQP":                          1,
		"sqp-1":                        1,
		"SqP-4 (MA Hybrid)":            1,
		"sqp-something":                1,
		"Audio":                        0,
		"Custom":                       3,
		"Other":                        2,
		"":                             2,
		"Streaming Services":           0,
		"Squad":                        0, // does not start with SQP — the literal Q matters
	}
	for cat, want := range cases {
		got := CategoryTier(cat)
		if got != want {
			t.Errorf("CategoryTier(%q) = %d, want %d", cat, got, want)
		}
	}
}
