package calculator

import "testing"

func mkTitle(id, audio, tier string, pri int) Title {
	cfs := []string{audio, "dv", "hdr10"}
	if tier != "" {
		cfs = append(cfs, tier)
	}
	return Title{ID: id, QualityRank: 0, QualityAllowed: true, MatchedCFs: cfs, PriorityGroup: pri, Partition: Wanted}
}

// Identical CF sets at different priorities must merge, not move.
func TestResolve_IdenticalMerges(t *testing.T) {
	in := Input{Titles: []Title{
		mkTitle("a", "dtsx", "t1", 1),
		mkTitle("b", "dtsx", "t1", 2), // identical to a
	}, ScoreBound: 10000}
	r := Resolve(in)
	if len(r.MergeGroups) != 1 || len(r.Moves) != 0 {
		t.Fatalf("expected one merge group and no moves, got merges=%v moves=%v", r.MergeGroups, r.Moves)
	}
}

// A single release placed above its CF-implied rank must be MOVED, not merged.
// "wrong" (dtshdma/t2) is dropped above a dtsx/t2 release, but dtshdma < dtsx
// and t2 < t1, so the placement contradicts the rest. Its CFs appear in other
// correctly-placed releases, so its score is pinned and it genuinely can't sit
// where the user put it. Every CF set here is distinct, so nothing should merge.
func TestResolve_MisplacedReleaseMoves(t *testing.T) {
	in := Input{Titles: []Title{
		mkTitle("top", "dtsx", "t1", 1),
		mkTitle("m1", "dtshdma", "t1", 2),
		mkTitle("wrong", "dtshdma", "t2", 3), // belongs below "d1"
		mkTitle("d1", "dtsx", "t2", 4),
		mkTitle("bot", "dtshdma", "t3", 5),
	}, ScoreBound: 10000}
	r := Resolve(in)
	if r.Conflict != nil {
		t.Fatalf("expected a move resolution, got conflict %q", r.Conflict.Message)
	}
	if len(r.MergeGroups) != 0 {
		t.Errorf("all CF sets are distinct; expected no merges, got %v", r.MergeGroups)
	}
	// One move fixes it. The conflict is the adjacent pair wrong>d1 (the user put
	// dtshdma/t2 above dtsx/t2); moving either one is a valid single fix.
	if len(r.Moves) != 1 {
		t.Fatalf("expected exactly one move, got %+v", r.Moves)
	}
	if id := r.Moves[0].TitleID; id != "wrong" && id != "d1" {
		t.Errorf("expected the move to target the conflicting pair (wrong/d1), got %q", id)
	}
}

// A wanted/unwanted pair sharing one CF set cannot be grouped or moved away.
func TestResolve_UngroupableConflict(t *testing.T) {
	in := Input{Titles: []Title{
		mkTitle("w", "dtsx", "t1", 1),
		{ID: "u", QualityRank: 0, QualityAllowed: true, MatchedCFs: []string{"dtsx", "dv", "hdr10", "t1"}, PriorityGroup: 2, Partition: Unwanted},
	}, ScoreBound: 10000}
	r := Resolve(in)
	if r.Feasible {
		t.Fatalf("a wanted/unwanted identical pair cannot be made feasible")
	}
}
