package core

import (
	"clonarr/internal/arr"
	"testing"
)

func TestFlattenGroups(t *testing.T) {
	items := []arr.ArrQualityItem{
		arrQ(1, "Flat1", true),
		arrG(100, "Group1", true, 
			arrQ(2, "Member1", true),
			arrQ(3, "Member2", true),
		),
		arrQ(4, "Flat2", false),
	}
	
	groups := flattenGroups(items)
	
	if groups["Flat1"] != "" { t.Errorf("expected Flat1 to have no group, got %q", groups["Flat1"]) }
	if groups["Flat2"] != "" { t.Errorf("expected Flat2 to have no group, got %q", groups["Flat2"]) }
	if groups["Member1"] != "Group1" { t.Errorf("expected Member1 to be in Group1, got %q", groups["Member1"]) }
	if groups["Member2"] != "Group1" { t.Errorf("expected Member2 to be in Group1, got %q", groups["Member2"]) }
}

func TestDiffArrProfile_StructureChanges(t *testing.T) {
	current := &arr.ArrQualityProfile{
		Items: []arr.ArrQualityItem{
			{Name: "A", Allowed: true},
			{Name: "B", Allowed: true},
			{Name: "Group", Allowed: true, Items: []arr.ArrQualityItem{{Name: "C", Allowed: true}}},
		},
	}
	// Target has C ungrouped, and B and A swapped
	target := &arr.ArrQualityProfile{
		Items: []arr.ArrQualityItem{
			{Name: "B", Allowed: true},
			{Name: "A", Allowed: true},
			{Name: "C", Allowed: true},
		},
	}
	// diffArrProfile(current, target, cfs, rule, priorAvailableGroups, scoreOverrides, extras, ad)
	details := diffArrProfile(current, target, nil, &AutoSyncRule{}, map[string]bool{}, map[string]int{}, nil, nil)
	
	groupDriftFound := false
	orderDriftFound := false
	
	for _, d := range details {
		if d.Field == "group" && d.CFName == "C" && d.Current == "Group" && d.Target == "Ungrouped" {
			groupDriftFound = true
		}
	}
	
	if !groupDriftFound { t.Error("expected group drift for C") }
	
	// Target2 has only order changes
	target2 := &arr.ArrQualityProfile{
		Items: []arr.ArrQualityItem{
			{Name: "B", Allowed: true},
			{Name: "A", Allowed: true},
			{Name: "Group", Allowed: true, Items: []arr.ArrQualityItem{{Name: "C", Allowed: true}}},
		},
	}
	
	details2 := diffArrProfile(current, target2, nil, &AutoSyncRule{}, map[string]bool{}, map[string]int{}, nil, nil)
	
	for _, d := range details2 {
		if d.Field == "quality_order" && d.CFName == "structure" {
			orderDriftFound = true
		}
	}
	
	if !orderDriftFound { t.Error("expected order drift") }
}
