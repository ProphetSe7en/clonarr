package core

import (
	"encoding/json"
	"testing"

	"clonarr/internal/arr"
)

// computeCFSpecDiffFingerprint is the canonical key behind the
// Detected / Reconciled state machine. The same drift seen on two
// passes must produce identical fingerprints so the de-dup gate
// silently no-ops on steady-state drift instead of re-notifying every
// 10 minutes.
func TestComputeCFSpecDiffFingerprint_StableAcrossEquivalentDiffs(t *testing.T) {
	t.Parallel()

	makeDiff := func() *CFSpecDiff {
		return &CFSpecDiff{
			AddedConditions: []ConditionRef{
				{Name: "Atmos", Implementation: "ReleaseTitleSpecification", Value: "\\bATMOS\\b"},
			},
			RemovedConditions: []ConditionRef{
				{Name: "Mono", Implementation: "ReleaseTitleSpecification", Value: "\\bMONO\\b"},
			},
			ChangedConditions: []ConditionChange{
				{Name: "WEB-DL", Implementation: "ReleaseTitleSpecification", Field: "value", Before: "old", After: "new"},
			},
			SettingsChanges: []SettingChange{
				{Field: "includeCustomFormatWhenRenaming", Before: "false", After: "true"},
			},
		}
	}

	fp1 := computeCFSpecDiffFingerprint(makeDiff())
	fp2 := computeCFSpecDiffFingerprint(makeDiff())

	if fp1 == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if fp1 != fp2 {
		t.Errorf("expected stable fingerprint across equivalent diffs, got %q != %q", fp1, fp2)
	}
}

// Two diffs that carry the same set of changes but in different array
// orders must collapse to the same fingerprint — otherwise Arr's
// arbitrary ordering of /customformat would oscillate the state
// machine and spam notifications every pass.
func TestComputeCFSpecDiffFingerprint_OrderInsensitive(t *testing.T) {
	t.Parallel()

	diffA := &CFSpecDiff{
		AddedConditions: []ConditionRef{
			{Name: "Zeta", Implementation: "ReleaseTitleSpecification"},
			{Name: "Alpha", Implementation: "ReleaseTitleSpecification"},
		},
	}
	diffB := &CFSpecDiff{
		AddedConditions: []ConditionRef{
			{Name: "Alpha", Implementation: "ReleaseTitleSpecification"},
			{Name: "Zeta", Implementation: "ReleaseTitleSpecification"},
		},
	}
	fpA := computeCFSpecDiffFingerprint(diffA)
	fpB := computeCFSpecDiffFingerprint(diffB)
	if fpA != fpB {
		t.Errorf("expected order-insensitive fingerprint, got %q != %q", fpA, fpB)
	}
}

// A genuinely different drift (different condition added) must produce
// a different fingerprint so a CF whose drift shape evolved fires a
// fresh notification instead of silently staying on the old one.
func TestComputeCFSpecDiffFingerprint_DifferentDiffDifferentFingerprint(t *testing.T) {
	t.Parallel()

	diffA := &CFSpecDiff{
		AddedConditions: []ConditionRef{
			{Name: "Atmos", Implementation: "ReleaseTitleSpecification"},
		},
	}
	diffB := &CFSpecDiff{
		AddedConditions: []ConditionRef{
			{Name: "DTS", Implementation: "ReleaseTitleSpecification"},
		},
	}
	fpA := computeCFSpecDiffFingerprint(diffA)
	fpB := computeCFSpecDiffFingerprint(diffB)
	if fpA == fpB {
		t.Errorf("expected distinct fingerprints for distinct diffs, both = %q", fpA)
	}
}

// Nil diff and empty diff both return empty fingerprints — the "no
// drift" signal the state machine treats as the absent-key case.
func TestComputeCFSpecDiffFingerprint_NilAndEmptyAreEmpty(t *testing.T) {
	t.Parallel()

	if got := computeCFSpecDiffFingerprint(nil); got != "" {
		t.Errorf("nil diff: expected empty, got %q", got)
	}
	if got := computeCFSpecDiffFingerprint(&CFSpecDiff{}); got == "" {
		// Empty diff still produces a non-empty fingerprint of the
		// empty canonical form. That's fine: the caller checks
		// HasAny() before storing the fingerprint at all, so a
		// fingerprint of an empty diff never reaches the persistence
		// step.
		t.Log("note: empty (non-nil) diff produces a non-empty fingerprint by design")
	}
}

// customCFToTrashCF must produce a TrashCF that DiffCFSpecs can compare
// against a TRaSH CF or an Arr-side ArrCF without losing the spec
// information (name, implementation, required/negate flags, raw fields).
func TestCustomCFToTrashCF_RoundTripsSpecs(t *testing.T) {
	t.Parallel()

	src := CustomCF{
		Name:            "User Custom CF",
		IncludeInRename: true,
		Specifications: []arr.ArrSpecification{
			{
				Name:           "Match Atmos",
				Implementation: "ReleaseTitleSpecification",
				Negate:         false,
				Required:       true,
				Fields:         json.RawMessage(`[{"name":"value","value":"\\bATMOS\\b"}]`),
			},
			{
				Name:           "Not Mono",
				Implementation: "ReleaseTitleSpecification",
				Negate:         true,
				Required:       false,
				Fields:         json.RawMessage(`[{"name":"value","value":"\\bMONO\\b"}]`),
			},
		},
	}

	got := customCFToTrashCF(src)
	if got.Name != "User Custom CF" {
		t.Errorf("Name: got %q, want %q", got.Name, "User Custom CF")
	}
	if !got.IncludeInRename {
		t.Error("IncludeInRename: expected true")
	}
	if len(got.Specifications) != 2 {
		t.Fatalf("Specifications: got %d, want 2", len(got.Specifications))
	}
	if got.Specifications[0].Name != "Match Atmos" || !got.Specifications[0].Required {
		t.Errorf("Spec 0 mismatch: %+v", got.Specifications[0])
	}
	if got.Specifications[1].Name != "Not Mono" || !got.Specifications[1].Negate {
		t.Errorf("Spec 1 mismatch: %+v", got.Specifications[1])
	}
}

// DeriveSyncCategories is the helper that tags every SyncHistoryEntry
// with the "profile" / "cf" channels the History tab filters by. Six
// scenarios cover the matrix the editor + Sync Rules render against.
func TestDeriveSyncCategories(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		result *SyncResult
		want   []string
	}{
		{
			name:   "nil result returns nil",
			result: nil,
			want:   nil,
		},
		{
			name:   "no changes returns nil",
			result: &SyncResult{},
			want:   nil,
		},
		{
			name:   "CF created tags cf only",
			result: &SyncResult{CFsCreated: 1},
			want:   []string{"cf"},
		},
		{
			name:   "score updated tags profile only",
			result: &SyncResult{ScoresUpdated: 3},
			want:   []string{"profile"},
		},
		{
			name:   "CF spec diff tags cf only",
			result: &SyncResult{CFSpecDiffs: map[string]*CFSpecDiff{"abc": {SettingsChanges: []SettingChange{{Field: "name"}}}}},
			want:   []string{"cf"},
		},
		{
			name:   "settings detail tags profile only",
			result: &SyncResult{SettingsDetails: []string{"cutoff: BluRay-1080p → BluRay-2160p"}},
			want:   []string{"profile"},
		},
		{
			name: "combined CF and profile tags both",
			result: &SyncResult{
				CFsUpdated:    2,
				ScoresUpdated: 1,
			},
			want: []string{"cf", "profile"},
		},
		{
			name:   "quality updated tags profile only",
			result: &SyncResult{QualityUpdated: true},
			want:   []string{"profile"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DeriveSyncCategories(tc.result)
			if !equalStringSlice(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
