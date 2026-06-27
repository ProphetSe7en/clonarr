package calculator

import (
	"math"
	"testing"
)

func rankedSameTier() Input {
	return Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux", "atmos"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"remux"}, PriorityGroup: 2, Partition: Wanted},
		{ID: "C", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"webdl"}, PriorityGroup: 3, Partition: Wanted},
		{ID: "U", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"cam"}, PriorityGroup: 4, Partition: Unwanted},
	}}
}

func TestSnap_Step1Preserves(t *testing.T) {
	in := rankedSameTier()
	r := Calculate(in)
	snapped, ok := Snap(in, r, 1)
	if !ok {
		t.Fatal("snapping to step 1 should preserve the ranking")
	}
	if v := VerifyRanking(in, snapped.CFScores, snapped.MinFormatScore); len(v) != 0 {
		t.Fatalf("snapped result violates ranking: %+v", v)
	}
	for c, s := range snapped.CFScores {
		if s != math.Trunc(s) {
			t.Errorf("score %s = %v is not an integer after snapping", c, s)
		}
	}
}

func TestSnap_FallbackOnCollapse(t *testing.T) {
	in := rankedSameTier() // minimal gaps of 1
	r := Calculate(in)
	_, ok := Snap(in, r, 100) // 100 cannot preserve gaps of 1
	if ok {
		t.Fatal("snapping minimal gaps to step 100 should fail and fall back")
	}
}

func TestIIS_IdenticalCFs(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 2, Partition: Wanted},
	}}
	c := ExplainInfeasibility(in)
	if c.Message == "" {
		t.Fatal("expected a conflict explanation")
	}
	if len(c.Involved) == 0 {
		t.Error("expected involved titles/CFs")
	}
}

func TestIIS_FeasibleNoConflict(t *testing.T) {
	if c := ExplainInfeasibility(rankedSameTier()); c.Message != "" {
		t.Fatalf("feasible input should have no conflict, got %q", c.Message)
	}
}

func TestIIS_OverrideConflict(t *testing.T) {
	in := Input{
		Titles: []Title{
			{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
			{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{}, PriorityGroup: 2, Partition: Wanted},
		},
		Overrides: map[string]Override{"x": {Type: "exclude"}},
	}
	c := ExplainInfeasibility(in)
	if c.Message == "" {
		t.Fatal("expected a conflict explanation for exclude-vs-priority")
	}
}

func TestValidate_CrossQuality(t *testing.T) {
	in := Input{Titles: []Title{
		// A preferred (group 1) but worse quality tier (rank 2) than B.
		{ID: "A", QualityRank: 2, QualityAllowed: true, MatchedCFs: []string{"y"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"z"}, PriorityGroup: 2, Partition: Wanted},
	}}
	issues := Validate(in)
	if !HasErrors(issues) {
		t.Fatal("expected a cross-quality error")
	}
	found := false
	for _, i := range issues {
		if i.Kind == "cross-quality" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cross-quality kind, got %+v", issues)
	}
}

func TestValidate_WantedDisallowed(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: false, MatchedCFs: []string{"y"}, PriorityGroup: 1, Partition: Wanted},
	}}
	issues := Validate(in)
	found := false
	for _, i := range issues {
		if i.Kind == "wanted-disallowed" && i.Severity == "error" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected wanted-disallowed error, got %+v", issues)
	}
}

func TestValidate_CleanNoErrors(t *testing.T) {
	if HasErrors(Validate(rankedSameTier())) {
		t.Fatal("clean ranked input should have no errors")
	}
}

func TestValidate_IdenticalCFsError(t *testing.T) {
	in := Input{Titles: []Title{
		{ID: "A", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 1, Partition: Wanted},
		{ID: "B", QualityRank: 1, QualityAllowed: true, MatchedCFs: []string{"x"}, PriorityGroup: 2, Partition: Wanted},
	}}
	found := false
	for _, i := range Validate(in) {
		if i.Kind == "identical-cfs" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected identical-cfs error")
	}
}
