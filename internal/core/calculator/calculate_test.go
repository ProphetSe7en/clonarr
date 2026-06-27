package calculator

import "testing"

// verify checks that a calculated result actually reproduces the requested
// ranking and partition. Robust to alternate LP optima (asserts the ordering
// properties, not exact score values).
func verify(t *testing.T, in Input, r Result) {
	t.Helper()
	if !r.Feasible {
		t.Fatal("expected feasible result")
	}
	M := r.MinFormatScore
	for i := range in.Titles {
		for j := range in.Titles {
			a, b := in.Titles[i], in.Titles[j]
			if !a.QualityAllowed || !b.QualityAllowed || a.QualityRank != b.QualityRank {
				continue
			}
			if a.Partition == Unwanted || b.Partition == Unwanted {
				continue // unwanted titles are not ranked, only rejected
			}
			sa := TitleScore(a, r.CFScores)
			sb := TitleScore(b, r.CFScores)
			if a.PriorityGroup < b.PriorityGroup && !(sa > sb+0.5) {
				t.Errorf("%s (%.1f) should outrank %s (%.1f)", a.ID, sa, b.ID, sb)
			}
			if a.PriorityGroup == b.PriorityGroup && i < j && (sa-sb > 0.5 || sb-sa > 0.5) {
				t.Errorf("%s (%.1f) and %s (%.1f) are equal priority but score differently", a.ID, sa, b.ID, sb)
			}
		}
	}
	for _, tl := range in.Titles {
		if !tl.QualityAllowed {
			continue
		}
		s := TitleScore(tl, r.CFScores)
		if tl.Partition == Wanted && !(s >= M-0.5) {
			t.Errorf("wanted %s scores %.1f, below minFormatScore %.1f", tl.ID, s, M)
		}
		if tl.Partition == Unwanted && !(s <= M-0.5) {
			t.Errorf("unwanted %s scores %.1f, not below minFormatScore %.1f", tl.ID, s, M)
		}
	}
}

// A realistic same-tier ranking with a wanted/unwanted split.
func TestCalculate_RankedSameTier(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux", "atmos"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux"}, PriorityGroup: 2, Partition: Wanted},
		{ID: "C", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"webdl"}, PriorityGroup: 3, Partition: Wanted},
		{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"cam"}, PriorityGroup: 4, Partition: Unwanted},
	}}
	verify(t, in, Calculate(in))
}

// Equal-priority titles in the same tier must score equally.
func TestCalculate_EqualPriority(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"y"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"z"}, PriorityGroup: 2, Partition: Unwanted},
	}}
	verify(t, in, Calculate(in))
}

// Two titles with an identical CF set in the same tier cannot be ranked apart.
func TestCalculate_IdenticalCFsInfeasible(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 2, Partition: Wanted},
	}}
	if r := Calculate(in); r.Feasible {
		t.Fatalf("expected infeasible (identical CFs, same tier, different priority); got scores %v", r.CFScores)
	}
}

// Cross-tier preference where the preferred title also has the better quality
// is auto-satisfied by quality dominance: no constraint, feasible.
func TestCalculate_CrossTierFeasible(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"y"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 2, QualityAllowed: true, MatchedCFs: []string{"z"}, PriorityGroup: 2, Partition: Wanted},
	}}
	r := Calculate(in)
	if !r.Feasible {
		t.Fatal("cross-tier consistent preference should be feasible")
	}
}

// A boost override forces a minimum score on a CF.
func TestCalculate_BoostOverride(t *testing.T) {
	in := Input{
		Titles: []Title{
			{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
			{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{}, PriorityGroup: 2, Partition: Wanted},
		},
		Overrides: map[string]Override{"x": {Type: "boost", Min: 50}},
	}
	r := Calculate(in)
	verify(t, in, r)
	if r.CFScores["x"] < 49.5 {
		t.Errorf("boost override: x = %.1f, want >= 50", r.CFScores["x"])
	}
}

// An unwanted title interleaved among wanted ones stays feasible: it only
// gets the reject constraint, not priority ranking.
func TestCalculate_UnwantedInterleavedFeasible(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"cam"}, PriorityGroup: 2, Partition: Unwanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"webdl"}, PriorityGroup: 3, Partition: Wanted},
	}}
	r := Calculate(in)
	verify(t, in, r)
}

// Flagging a CF as the reason a release is unwanted (an "unwanted" override)
// forces that CF strongly negative, so it generalises to every release with it.
func TestCalculate_CFReasonGeneralizes(t *testing.T) {
	in := Input{
		Titles: []Title{
			{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux"}, PriorityGroup: 1, Partition: Wanted},
			{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"cam"}, PriorityGroup: 2, Partition: Unwanted},
		},
		Overrides: map[string]Override{"cam": {Type: "unwanted", Max: -1000}},
	}
	r := Calculate(in)
	verify(t, in, r)
	if r.CFScores["cam"] > -1000+0.5 {
		t.Errorf("cam should be forced <= -1000, got %.1f", r.CFScores["cam"])
	}
}

// A pinned minimum score forces wanted releases to reach it and unwanted to
// stay below, so CF scores land at realistic magnitudes (not 0/1).
func TestCalculate_PinnedMinScore(t *testing.T) {
	min := 200.0
	in := Input{
		MinScore: &min,
		Titles: []Title{
			{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux", "atmos"}, PriorityGroup: 1, Partition: Wanted},
			{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux"}, PriorityGroup: 2, Partition: Wanted},
			{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"cam"}, PriorityGroup: 3, Partition: Unwanted},
		},
	}
	r := Calculate(in)
	if !r.Feasible || r.MinFormatScore != 200 {
		t.Fatalf("expected feasible with M=200, got feasible=%v M=%v", r.Feasible, r.MinFormatScore)
	}
	if s := TitleScore(in.Titles[1], r.CFScores); s < 200 {
		t.Errorf("wanted B should reach 200, scored %.1f", s)
	}
	if s := TitleScore(in.Titles[2], r.CFScores); s >= 200 {
		t.Errorf("unwanted U should be below 200, scored %.1f", s)
	}
}

// An exclude override that removes the only distinguishing CF is infeasible.
func TestCalculate_ExcludeOverrideConflict(t *testing.T) {
	in := Input{
		Titles: []Title{
			{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
			{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{}, PriorityGroup: 2, Partition: Wanted},
		},
		Overrides: map[string]Override{"x": {Type: "exclude"}},
	}
	if r := Calculate(in); r.Feasible {
		t.Fatalf("exclude on the only distinguishing CF should be infeasible; got %v", r.CFScores)
	}
}
