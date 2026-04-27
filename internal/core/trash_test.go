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
