package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"testing"
)

func TestBuildAlignment_MatchesAndGaps(t *testing.T) {
	cKeys := []string{"Q1", "Q3", "Q4"}
	gKeys := []string{"Q1", "Q2", "Q3", "Q5"}
	
	// Q1 matches
	// Q2 is missing in current
	// Q3 matches
	// Q4 is missing in guide
	// Q5 is missing in current
	
	cItemMap := map[string]arr.ArrQualityItem{
		"Q1": {Name: "Q1"},
		"Q3": {Name: "Q3"},
		"Q4": {Name: "Q4"},
	}
	gItemMap := map[string]core.QualityItem{
		"Q1": {Name: "Q1"},
		"Q2": {Name: "Q2"},
		"Q3": {Name: "Q3"},
		"Q5": {Name: "Q5"},
	}
	
	guideExpectedEnabled := map[string]bool{
		"Q1": true, "Q2": true, "Q3": true, "Q4": true, "Q5": true,
	}
	
	rows := BuildAlignment("radarr", cKeys, gKeys, true, guideExpectedEnabled, cItemMap, gItemMap)
	
	// We expect:
	// Q1 <-> Q1 (Match: true)
	// "" <-> Q2 (Match: false)
	// Q3 <-> Q3 (Match: true)
	// Q4 <-> "" (Match: false)
	// "" <-> Q5 (Match: false)
	
	for i, r := range rows {
		t.Logf("row %d: Current=%q, Guide=%q, Match=%v", i, r.Current, r.Guide, r.Match)
	}
	
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	
	if rows[0].Current != "Q1" || rows[0].Guide != "Q1" || !rows[0].Match { t.Errorf("row 0 mismatch") }
	if rows[1].Current != "" || rows[1].Guide != "Q2" || rows[1].Match { t.Errorf("row 1 mismatch") }
	if rows[2].Current != "Q3" || rows[2].Guide != "Q3" || !rows[2].Match { t.Errorf("row 2 mismatch") }
	if rows[3].Current != "" || rows[3].Guide != "Q5" || rows[3].Match { t.Errorf("row 3 mismatch") }
	if rows[4].Current != "Q4" || rows[4].Guide != "" || rows[4].Match { t.Errorf("row 4 mismatch") }
}

func TestBuildAlignment_DisabledSection(t *testing.T) {
	cKeys := []string{"Q1", "Q2"}
	gKeys := []string{"Q1"}
	
	cItemMap := map[string]arr.ArrQualityItem{"Q1": {Name: "Q1"}, "Q2": {Name: "Q2"}}
	gItemMap := map[string]core.QualityItem{"Q1": {Name: "Q1"}}
	
	// Guide expects Q2 to be enabled, but it's in the disabled section
	guideExpectedEnabled := map[string]bool{
		"Q2": true,
	}
	
	rows := BuildAlignment("radarr", cKeys, gKeys, false, guideExpectedEnabled, cItemMap, gItemMap)
	
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	
	// Q1 matches and is NOT expected enabled -> Match: true
	if rows[0].Current != "Q1" || rows[0].Guide != "Q1" || !rows[0].Match {
		t.Errorf("expected Q1 to be true Match")
	}
	
	// Q2 is in disabled section but guide expects it enabled -> Match: false
	if rows[1].Current != "Q2" || rows[1].Guide != "" || rows[1].Match {
		t.Errorf("expected Q2 to be false Match, got %v", rows[1].Match)
	}
}
