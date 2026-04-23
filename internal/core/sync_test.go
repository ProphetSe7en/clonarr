package core

import (
	"clonarr/internal/arr"
	"encoding/json"
	"testing"
)

// --- ResolveSyncBehavior ---

func TestResolveSyncBehavior_Nil(t *testing.T) {
	got := ResolveSyncBehavior(nil)
	want := DefaultSyncBehavior()
	if got != want {
		t.Errorf("nil input: got %+v, want %+v", got, want)
	}
}

func TestResolveSyncBehavior_Full(t *testing.T) {
	b := &SyncBehavior{
		AddMode:    "do_not_add",
		RemoveMode: "allow_custom",
		ResetMode:  "do_not_adjust",
	}
	got := ResolveSyncBehavior(b)
	if got.AddMode != "do_not_add" || got.RemoveMode != "allow_custom" || got.ResetMode != "do_not_adjust" {
		t.Errorf("full input: got %+v", got)
	}
}

func TestResolveSyncBehavior_Partial(t *testing.T) {
	b := &SyncBehavior{
		AddMode: "add_new",
		// RemoveMode and ResetMode omitted
	}
	got := ResolveSyncBehavior(b)
	if got.AddMode != "add_new" {
		t.Errorf("expected AddMode 'add_new', got %q", got.AddMode)
	}
	if got.RemoveMode != "remove_custom" {
		t.Errorf("expected default RemoveMode 'remove_custom', got %q", got.RemoveMode)
	}
	if got.ResetMode != "reset_to_zero" {
		t.Errorf("expected default ResetMode 'reset_to_zero', got %q", got.ResetMode)
	}
}

func TestResolveSyncBehavior_Empty(t *testing.T) {
	b := &SyncBehavior{}
	got := ResolveSyncBehavior(b)
	want := DefaultSyncBehavior()
	if got != want {
		t.Errorf("empty input: got %+v, want %+v", got, want)
	}
}

// --- ConvertFieldsToArr ---

func TestConvertFieldsToArr_ObjectFormat(t *testing.T) {
	// TRaSH format: {"value": "test123"}
	input := json.RawMessage(`{"value": "test123"}`)
	result := ConvertFieldsToArr(input)

	var arr []map[string]any
	if err := json.Unmarshal(result, &arr); err != nil {
		t.Fatalf("expected array, got error: %v (raw: %s)", err, string(result))
	}
	if len(arr) != 1 {
		t.Fatalf("expected 1 element, got %d", len(arr))
	}
	if arr[0]["name"] != "value" || arr[0]["value"] != "test123" {
		t.Errorf("unexpected result: %+v", arr[0])
	}
}

func TestConvertFieldsToArr_ArrayFormat(t *testing.T) {
	// Already in Arr format: [{"name":"value","value":"test123"}]
	input := json.RawMessage(`[{"name":"value","value":"test123"}]`)
	result := ConvertFieldsToArr(input)

	// Should pass through unchanged
	if string(result) != string(input) {
		t.Errorf("expected passthrough, got %s", string(result))
	}
}

func TestConvertFieldsToArr_Malformed(t *testing.T) {
	// Invalid JSON should return as-is
	input := json.RawMessage(`not-json`)
	result := ConvertFieldsToArr(input)
	if string(result) != string(input) {
		t.Errorf("expected passthrough for malformed input, got %s", string(result))
	}
}

func TestConvertFieldsToArr_MultipleKeys(t *testing.T) {
	// Object with multiple keys: should produce sorted array
	input := json.RawMessage(`{"zeta": 1, "alpha": 2}`)
	result := ConvertFieldsToArr(input)

	var arr []map[string]any
	if err := json.Unmarshal(result, &arr); err != nil {
		t.Fatalf("expected array: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
	// Keys should be sorted
	if arr[0]["name"] != "alpha" {
		t.Errorf("expected first key 'alpha', got %v", arr[0]["name"])
	}
	if arr[1]["name"] != "zeta" {
		t.Errorf("expected second key 'zeta', got %v", arr[1]["name"])
	}
}

// --- resolveScore ---

func TestResolveScore_OverridePrecedence(t *testing.T) {
	cf := &TrashCF{
		TrashScores: map[string]int{
			"default": 100,
			"sqp-1":   200,
		},
	}
	overrides := map[string]int{"test-id": 999}

	// Override takes precedence
	got := resolveScore("test-id", cf, "sqp-1", overrides)
	if got != 999 {
		t.Errorf("expected override 999, got %d", got)
	}
}

func TestResolveScore_ScoreContext(t *testing.T) {
	cf := &TrashCF{
		TrashScores: map[string]int{
			"default": 100,
			"sqp-1":   200,
		},
	}

	got := resolveScore("test-id", cf, "sqp-1", nil)
	if got != 200 {
		t.Errorf("expected score context 200, got %d", got)
	}
}

func TestResolveScore_DefaultFallback(t *testing.T) {
	cf := &TrashCF{
		TrashScores: map[string]int{
			"default": 100,
		},
	}

	got := resolveScore("test-id", cf, "nonexistent", nil)
	if got != 100 {
		t.Errorf("expected default fallback 100, got %d", got)
	}
}

func TestResolveScore_ZeroWhenNoMatch(t *testing.T) {
	cf := &TrashCF{
		TrashScores: map[string]int{},
	}

	got := resolveScore("test-id", cf, "any", nil)
	if got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
}

// --- customCFToArr ---

func TestCustomCFToArr_Basic(t *testing.T) {
	cf := &CustomCF{
		ID:              "custom:abc123",
		Name:            "Test CF",
		IncludeInRename: true,
		Specifications: []arr.ArrSpecification{
			{
				Name:           "spec1",
				Implementation: "ReleaseTitleSpecification",
				Negate:         false,
				Required:       true,
				Fields:         json.RawMessage(`{"value": "test-regex"}`),
			},
		},
	}

	result := customCFToArr(cf)

	if result.Name != "Test CF" {
		t.Errorf("expected name 'Test CF', got %q", result.Name)
	}
	if !result.IncludeCustomFormatWhenRenaming {
		t.Error("expected IncludeCustomFormatWhenRenaming=true")
	}
	if len(result.Specifications) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(result.Specifications))
	}

	// Fields should be converted from object to array format
	var fields []map[string]any
	if err := json.Unmarshal(result.Specifications[0].Fields, &fields); err != nil {
		t.Fatalf("expected array fields: %v", err)
	}
	if len(fields) != 1 || fields[0]["name"] != "value" {
		t.Errorf("unexpected fields: %s", string(result.Specifications[0].Fields))
	}
}

func TestCustomCFToArr_EmptySpecs(t *testing.T) {
	cf := &CustomCF{
		Name:           "Empty",
		Specifications: nil,
	}

	result := customCFToArr(cf)
	if len(result.Specifications) != 0 {
		t.Errorf("expected 0 specs, got %d", len(result.Specifications))
	}
}

// --- fingerprintArrItems / fingerprintTrashItems ---
// These functions are the core of the structure-drift detection that closes the
// five blindspots the set-based quality diff used to miss: reorder items,
// reorder groups, move a quality into/out of a group with the same allowed state,
// and structural changes that leave the allowed set untouched.

// helper: build a flat Arr quality item
func arrQ(id int, name string, allowed bool) arr.ArrQualityItem {
	return arr.ArrQualityItem{
		Quality: &arr.ArrQualityRef{ID: id, Name: name},
		Items:   []arr.ArrQualityItem{},
		Allowed: allowed,
	}
}

// helper: build an Arr group
func arrG(groupID int, name string, allowed bool, members ...arr.ArrQualityItem) arr.ArrQualityItem {
	return arr.ArrQualityItem{
		ID:      groupID,
		Name:    name,
		Items:   members,
		Allowed: allowed,
	}
}

func TestFingerprintArrItems_FlatOnly(t *testing.T) {
	items := []arr.ArrQualityItem{
		arrQ(1, "DVD", false),
		arrQ(3, "Bluray-1080p", true),
		arrQ(4, "WEB 1080p", true),
	}
	got := fingerprintArrItems(items)
	want := `Q:"DVD"=false|Q:"Bluray-1080p"=true|Q:"WEB 1080p"=true`
	if got != want {
		t.Errorf("fingerprint mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestFingerprintArrItems_ReorderChangesFingerprint(t *testing.T) {
	a := []arr.ArrQualityItem{arrQ(1, "A", true), arrQ(2, "B", true), arrQ(3, "C", false)}
	b := []arr.ArrQualityItem{arrQ(3, "C", false), arrQ(1, "A", true), arrQ(2, "B", true)}
	if fingerprintArrItems(a) == fingerprintArrItems(b) {
		t.Error("reorder must produce different fingerprints (set-based diff blindspot)")
	}
}

func TestFingerprintArrItems_WithGroup(t *testing.T) {
	items := []arr.ArrQualityItem{
		arrQ(1, "SDTV", false),
		arrG(1000, "WEB 1080p", true,
			arrQ(10, "WEBDL-1080p", true),
			arrQ(11, "WEBRip-1080p", true),
		),
		arrQ(20, "Remux-1080p", true),
	}
	got := fingerprintArrItems(items)
	want := `Q:"SDTV"=false|G:"WEB 1080p"=true["WEBDL-1080p","WEBRip-1080p"]|Q:"Remux-1080p"=true`
	if got != want {
		t.Errorf("fingerprint mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestFingerprintArrItems_GroupMemberReorderChangesFingerprint(t *testing.T) {
	a := []arr.ArrQualityItem{
		arrG(1000, "WEB 1080p", true,
			arrQ(10, "WEBDL-1080p", true),
			arrQ(11, "WEBRip-1080p", true),
		),
	}
	b := []arr.ArrQualityItem{
		arrG(1000, "WEB 1080p", true,
			arrQ(11, "WEBRip-1080p", true),
			arrQ(10, "WEBDL-1080p", true),
		),
	}
	if fingerprintArrItems(a) == fingerprintArrItems(b) {
		t.Error("group member reorder must produce different fingerprints")
	}
}

// Group names are user-editable. A malicious (or just creative) name containing
// the format's reserved delimiters must not collide with a different structure.
func TestFingerprintArrItems_DelimiterInjectionSafe(t *testing.T) {
	// A flat quality literally named `foo"|G:"bar=true[`
	a := []arr.ArrQualityItem{arrQ(1, `foo"|G:"bar=true[`, true)}
	// A group with the innocuous name `foo` containing quality `bar`
	b := []arr.ArrQualityItem{
		arrG(1000, "foo", true, arrQ(10, "bar", true)),
	}
	if fingerprintArrItems(a) == fingerprintArrItems(b) {
		t.Errorf("delimiter injection collision:\n  a: %s\n  b: %s", fingerprintArrItems(a), fingerprintArrItems(b))
	}
}

func TestFingerprintArrItems_ExtractFromGroupChangesFingerprint(t *testing.T) {
	// Before: WEBDL-1080p is inside the group, allowed=true
	a := []arr.ArrQualityItem{
		arrG(1000, "WEB 1080p", true,
			arrQ(10, "WEBDL-1080p", true),
			arrQ(11, "WEBRip-1080p", true),
		),
	}
	// After: WEBDL-1080p extracted from the group, still allowed=true
	// Set-based diff sees the same {WEBDL-1080p, WEBRip-1080p, WEB 1080p}
	// allowed set and misses this. Fingerprint must catch it.
	b := []arr.ArrQualityItem{
		arrQ(10, "WEBDL-1080p", true),
		arrG(1000, "WEB 1080p", true,
			arrQ(11, "WEBRip-1080p", true),
		),
	}
	if fingerprintArrItems(a) == fingerprintArrItems(b) {
		t.Error("extracting a quality from a group with same allowed-state must produce different fingerprints (set-based diff blindspot)")
	}
}

func TestFingerprintTrashItems_MatchesArrFingerprint(t *testing.T) {
	// Both representations should produce the identical canonical string
	// so plan-phase can compare desired (TRaSH) vs current (Arr) directly.
	trash := []QualityItem{
		{Name: "SDTV", Allowed: false},
		{Name: "WEB 1080p", Allowed: true, Items: []string{"WEBDL-1080p", "WEBRip-1080p"}},
		{Name: "Remux-1080p", Allowed: true},
	}
	arr := []arr.ArrQualityItem{
		arrQ(1, "SDTV", false),
		arrG(1000, "WEB 1080p", true,
			arrQ(10, "WEBDL-1080p", true),
			arrQ(11, "WEBRip-1080p", true),
		),
		arrQ(20, "Remux-1080p", true),
	}
	if fingerprintTrashItems(trash) != fingerprintArrItems(arr) {
		t.Errorf("representations diverged:\n  trash: %s\n  arr:   %s", fingerprintTrashItems(trash), fingerprintArrItems(arr))
	}
}

func TestFilterArrItemsToDesired_DropsUnusedTail(t *testing.T) {
	// Typical live Arr profile: lots of disallowed "unused" qualities at the top,
	// followed by the actually-configured items. TRaSH only knows about the latter.
	arr := []arr.ArrQualityItem{
		arrQ(1, "REGIONAL", false),    // unused
		arrQ(2, "CAM", false),         // unused
		arrQ(3, "TELECINE", false),    // unused
		arrQ(10, "WEBDL-1080p", true), // in desired
		arrG(1000, "WEB 1080p", true, // group always kept
			arrQ(11, "WEBRip-1080p", true),
		),
	}
	desired := []QualityItem{
		{Name: "WEBDL-1080p", Allowed: true},
		{Name: "WEB 1080p", Allowed: true, Items: []string{"WEBRip-1080p"}},
	}
	filtered := filterArrItemsToDesired(arr, desired)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered items (WEBDL + group), got %d", len(filtered))
	}
	if fingerprintArrItems(filtered) != fingerprintTrashItems(desired) {
		t.Errorf("after filtering, fingerprints should match:\n  got:  %s\n  want: %s",
			fingerprintArrItems(filtered), fingerprintTrashItems(desired))
	}
}

func TestCutoffIDToName_FlatAndGroup(t *testing.T) {
	items := []arr.ArrQualityItem{
		arrQ(1, "SDTV", false),
		arrG(1000, "WEB 1080p", true, arrQ(10, "WEBDL-1080p", true)),
		arrQ(20, "Remux-1080p", true),
	}
	cases := []struct {
		id   int
		want string
	}{
		{1, "SDTV"},
		{1000, "WEB 1080p"},
		{20, "Remux-1080p"},
		{0, "(none)"},
		{999, "#999"}, // fallback for unknown
	}
	for _, c := range cases {
		got := cutoffIDToName(c.id, items)
		if got != c.want {
			t.Errorf("cutoffIDToName(%d): got %q, want %q", c.id, got, c.want)
		}
	}
}
