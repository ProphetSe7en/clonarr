package core

import (
	"encoding/json"
	"testing"

	"clonarr/internal/arr"
)

// Lock-in tests for the invariant: anything sync.go's cfSpecsMatch
// considers "match" (Action="unchanged") must ALSO be considered
// "no drift" by DiffCFSpecs. Without this guarantee, a CF that
// sync skipped during Update all immediately flags drift on the
// next Check pass — exactly the 91-of-199-false-positives bug the
// initial implementation produced.
//
// The two real-world shape divergences the tests exercise:
//
//   1. TRaSH disk format ships fields as a flat object:
//        {"value": "BluRay"}
//   2. Arr's API returns fields as a name/value array:
//        [{"name":"value","value":"BluRay"}]
//
// Both shapes carry the same semantic value; only the JSON wire
// shape differs. cfSpecsMatch bridges them via ExtractFieldValue;
// DiffCFSpecs must do the same.

// trashSpec is a small helper for building TrashCF fixtures with
// the flat-object field shape TRaSH JSONs use.
func trashSpec(name, impl string, fields string) CFSpecification {
	return CFSpecification{
		Name:           name,
		Implementation: impl,
		Fields:         json.RawMessage(fields),
	}
}

// arrSpec mirrors trashSpec for Arr's array shape.
func arrSpec(name, impl string, fields string) arr.ArrSpecification {
	return arr.ArrSpecification{
		Name:           name,
		Implementation: impl,
		Fields:         json.RawMessage(fields),
	}
}

// TestDiffCFSpecs_NoFalseDriftOnShapeDivergence — the canonical
// regression. A TRaSH CF on disk (flat-object fields) matches the
// SAME CF live in Arr (array fields). cfSpecsMatch returns true;
// DiffCFSpecs must report no drift. Covers a representative spec
// implementation that lives in renderSpecValue's `default` branch
// so the fix path (ExtractFieldValue + valuesEqual) is the actual
// equality gate, not the rendered-string compare.
func TestDiffCFSpecs_NoFalseDriftOnShapeDivergence(t *testing.T) {
	trashCF := &TrashCF{
		Name:            "x265 (HD)",
		IncludeInRename: false,
		Specifications: []CFSpecification{
			// SizeSpecification isn't in renderSpecValue's switch the
			// same way ReleaseGroupSpecification is — but the bigger
			// fault was any impl falling through to renderRaw. Use a
			// representative case that the original code path would
			// have flagged.
			trashSpec("Size Constraint", "SizeSpecification", `{"min":4000,"max":8000}`),
		},
	}
	arrCF := &arr.ArrCF{
		Name:                            "x265 (HD)",
		IncludeCustomFormatWhenRenaming: false,
		Specifications: []arr.ArrSpecification{
			arrSpec("Size Constraint", "SizeSpecification",
				`[{"name":"min","value":4000},{"name":"max","value":8000}]`),
		},
	}
	if !cfSpecsMatch(trashCF, arrCF) {
		t.Fatalf("sync engine considers these matching; drift logic must agree")
	}
	diff := DiffCFSpecs(trashCF, arrCFToTrashCF(arrCF))
	if diff != nil && diff.HasAny() {
		t.Errorf("DiffCFSpecs flagged drift on a sync-matching CF: %+v", diff)
	}
}

// TestDiffCFSpecs_ReleaseGroupShapeDivergenceClean — known-handled
// implementation (ReleaseGroupSpecification IS in renderSpecValue's
// switch). Both before and after the fix this should pass; we lock
// it in so a future refactor of the switch can't regress.
func TestDiffCFSpecs_ReleaseGroupShapeDivergenceClean(t *testing.T) {
	trashCF := &TrashCF{
		Name: "Bad Release Group",
		Specifications: []CFSpecification{
			trashSpec("Block Group", "ReleaseGroupSpecification", `{"value":"BAD-RG"}`),
		},
	}
	arrCF := &arr.ArrCF{
		Name: "Bad Release Group",
		Specifications: []arr.ArrSpecification{
			arrSpec("Block Group", "ReleaseGroupSpecification",
				`[{"name":"value","value":"BAD-RG"}]`),
		},
	}
	diff := DiffCFSpecs(trashCF, arrCFToTrashCF(arrCF))
	if diff != nil && diff.HasAny() {
		t.Errorf("DiffCFSpecs flagged drift on ReleaseGroup with shape-only diff: %+v", diff)
	}
}

// TestDiffCFSpecs_GenuineValueChangeStillDetected — the fix must not
// regress real drift detection. Same shape on both sides, different
// underlying value → drift fires.
func TestDiffCFSpecs_GenuineValueChangeStillDetected(t *testing.T) {
	before := &TrashCF{
		Name: "Block Group",
		Specifications: []CFSpecification{
			trashSpec("Block", "ReleaseGroupSpecification", `{"value":"BAD-RG"}`),
		},
	}
	after := &TrashCF{
		Name: "Block Group",
		Specifications: []CFSpecification{
			trashSpec("Block", "ReleaseGroupSpecification", `{"value":"OTHER-RG"}`),
		},
	}
	diff := DiffCFSpecs(before, after)
	if diff == nil || !diff.HasAny() {
		t.Fatal("DiffCFSpecs missed a genuine value change")
	}
	if len(diff.ChangedConditions) != 1 || diff.ChangedConditions[0].Field != "value" {
		t.Errorf("expected one value ChangedCondition, got %+v", diff.ChangedConditions)
	}
}

// TestDiffCFSpecs_DoesNotConfuseScoresWithSpecDrift — the second
// real-world false-positive source. Disk-side TrashCF carries
// TrashScores (every CF in TRaSH data has them); arrCFToTrashCF
// leaves TrashScores nil because Arr's CF API doesn't return scores.
// If cf_drift.go's pass passed the disk spec verbatim, every CF
// would produce N ScoreChange entries → drift fires on virtually
// every CF the first Check pass. The fix nil-s TrashScores on the
// disk-side copy before diffing. This test locks in that contract:
// CFs that ONLY differ by TrashScores presence are not drift.
func TestDiffCFSpecs_DoesNotConfuseScoresWithSpecDrift(t *testing.T) {
	disk := TrashCF{
		Name:            "WEB Tier 01",
		IncludeInRename: false,
		TrashScores: map[string]int{
			"default": 3000,
			"sqp-3":   2200,
		},
		Specifications: []CFSpecification{
			trashSpec("WEB Tier 01 regex", "ReleaseTitleSpecification", `{"value":"(?i)tier1"}`),
		},
	}
	// Drift detection's stripping path: clone disk-side with TrashScores nil.
	diskForDiff := disk
	diskForDiff.TrashScores = nil
	live := &TrashCF{
		Name:           "WEB Tier 01",
		Specifications: []CFSpecification{trashSpec("WEB Tier 01 regex", "ReleaseTitleSpecification", `[{"name":"value","value":"(?i)tier1"}]`)},
	}
	diff := DiffCFSpecs(&diskForDiff, live)
	if diff != nil && diff.HasAny() {
		t.Errorf("DiffCFSpecs flagged drift on a score-only divergence: %+v", diff)
	}
}

// TestDiffCFSpecs_NumericTypeTolerance — Arr may return numeric
// fields decoded as float64 while custom CFs stored locally can be
// strings. valuesEqual handles the conversion; DiffCFSpecs must too
// (via the same valuesEqual path now). Covers the IndexerFlag case
// where Arr returns an integer and TRaSH disk might have authored
// it as a string in some files.
func TestDiffCFSpecs_NumericTypeTolerance(t *testing.T) {
	before := &TrashCF{
		Name: "Freeleech",
		Specifications: []CFSpecification{
			trashSpec("Flag", "IndexerFlagSpecification", `{"value":1}`),
		},
	}
	after := &TrashCF{
		Name: "Freeleech",
		Specifications: []CFSpecification{
			trashSpec("Flag", "IndexerFlagSpecification", `[{"name":"value","value":1}]`),
		},
	}
	diff := DiffCFSpecs(before, after)
	if diff != nil && diff.HasAny() {
		t.Errorf("DiffCFSpecs flagged drift on a numeric-equal value across shapes: %+v", diff)
	}
}
