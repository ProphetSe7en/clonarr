package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"clonarr/internal/arr"
)

// CF spec diff engine. The Sync History "Updated: <CF>" rows show a
// commit pill (Phase 1) and a trigger chip (Phase 3). This file adds
// the field-level diff that explains WHAT actually changed inside the
// CF between two states. The same primitive (DiffCFSpecs) is intended
// to be reused by three more surfaces later: the TRaSH Last Updated
// panel's mid-week digest, the pre-pull dry-run preview, and the drift
// modal's "why does target say X" attribution.

// CFSpecDiff is the structured delta between two CF specifications.
// Every slice is nil-safe and omitempty on JSON so pre-Phase-2 history
// entries serialize without the field and the UI skips rendering.
type CFSpecDiff struct {
	AddedConditions   []ConditionRef    `json:"addedConditions,omitempty"`
	RemovedConditions []ConditionRef    `json:"removedConditions,omitempty"`
	ChangedConditions []ConditionChange `json:"changedConditions,omitempty"`
	SettingsChanges   []SettingChange   `json:"settingsChanges,omitempty"`
	ScoreChanges      []CFScoreChange     `json:"scoreChanges,omitempty"`
}

// HasAny reports whether the diff carries any change, for callers that
// want to skip persisting an empty diff alongside the existing
// CFDetails string. Nil-safe.
func (d *CFSpecDiff) HasAny() bool {
	if d == nil {
		return false
	}
	return len(d.AddedConditions) > 0 || len(d.RemovedConditions) > 0 ||
		len(d.ChangedConditions) > 0 || len(d.SettingsChanges) > 0 ||
		len(d.ScoreChanges) > 0
}

// ConditionRef points at a single Specification on a CF. Value is the
// human-readable rendering of whatever field uniquely identifies the
// spec (release-group regex string, resolution enum label, etc.).
// Negate and Required surface the per-spec flags so the UI can read
// the full intent without re-parsing the raw JSON.
type ConditionRef struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation"`
	Value          string `json:"value,omitempty"`
	Negate         bool   `json:"negate,omitempty"`
	Required       bool   `json:"required,omitempty"`
}

// ConditionChange describes a single field-level change on a spec that
// existed in both before and after. Field is "value" | "negate" |
// "required" | "implementation" (the last would mean TRaSH renamed the
// spec under the same name, which is rare but representable).
type ConditionChange struct {
	Name           string `json:"name"`
	Implementation string `json:"implementation,omitempty"`
	Field          string `json:"field"`
	Before         string `json:"before"`
	After          string `json:"after"`
}

// SettingChange covers the top-level CF settings (name,
// includeCustomFormatWhenRenaming). Plain string before/after so the
// UI doesn't need to know the field type.
type SettingChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// CFScoreChange is a per-context entry in the trash_scores map. Adding a
// new context is Before=0 with After=newvalue; removing a context is
// Before=oldvalue with After=0. That matches how the sync engine
// already treats "no score" as 0.
type CFScoreChange struct {
	Context string `json:"context"`
	Before  int    `json:"before"`
	After   int    `json:"after"`
}

// DiffCFSpecs returns the structured delta between two CF spec
// snapshots. Matching strategy: specs match by Name (case-sensitive),
// settings match by struct field, scores match by context key. Nil
// inputs are treated as empty CFs so "added entirely" / "removed
// entirely" cases work without a separate code path.
func DiffCFSpecs(before, after *TrashCF) *CFSpecDiff {
	if before == nil && after == nil {
		return nil
	}
	if before == nil {
		before = &TrashCF{}
	}
	if after == nil {
		after = &TrashCF{}
	}
	d := &CFSpecDiff{}

	// Top-level settings. Only emit a SettingChange when the field
	// actually moved, otherwise we'd persist a no-op delta on every
	// sync.
	if before.Name != after.Name {
		d.SettingsChanges = append(d.SettingsChanges, SettingChange{
			Field: "name", Before: before.Name, After: after.Name,
		})
	}
	if before.IncludeInRename != after.IncludeInRename {
		d.SettingsChanges = append(d.SettingsChanges, SettingChange{
			Field:  "includeCustomFormatWhenRenaming",
			Before: strconv.FormatBool(before.IncludeInRename),
			After:  strconv.FormatBool(after.IncludeInRename),
		})
	}

	// Score map deltas. Walk both maps so we catch additions, removals,
	// and changes in one pass. Sort keys at the end so callers and
	// snapshot tests get a deterministic ordering.
	scoreKeys := map[string]struct{}{}
	for k := range before.TrashScores {
		scoreKeys[k] = struct{}{}
	}
	for k := range after.TrashScores {
		scoreKeys[k] = struct{}{}
	}
	for k := range scoreKeys {
		bv := before.TrashScores[k]
		av := after.TrashScores[k]
		if bv != av {
			d.ScoreChanges = append(d.ScoreChanges, CFScoreChange{
				Context: k, Before: bv, After: av,
			})
		}
	}
	sort.Slice(d.ScoreChanges, func(i, j int) bool {
		return d.ScoreChanges[i].Context < d.ScoreChanges[j].Context
	})

	// Specifications - build name-keyed indices on each side. Diffing
	// by name keeps the comparison stable across order-only TRaSH
	// reformats (a common chore commit category).
	beforeByName := map[string]CFSpecification{}
	for _, s := range before.Specifications {
		beforeByName[s.Name] = s
	}
	afterByName := map[string]CFSpecification{}
	for _, s := range after.Specifications {
		afterByName[s.Name] = s
	}
	for name, bs := range beforeByName {
		as, present := afterByName[name]
		if !present {
			d.RemovedConditions = append(d.RemovedConditions, conditionRefFromSpec(bs))
			continue
		}
		// Same name on both sides: compare per-field. Implementation
		// changes are recorded as their own ConditionChange so the
		// caller can see what flipped without re-parsing.
		if bs.Implementation != as.Implementation {
			d.ChangedConditions = append(d.ChangedConditions, ConditionChange{
				Name: name, Field: "implementation",
				Before: bs.Implementation, After: as.Implementation,
			})
		}
		if bs.Negate != as.Negate {
			d.ChangedConditions = append(d.ChangedConditions, ConditionChange{
				Name: name, Implementation: as.Implementation,
				Field:  "negate",
				Before: strconv.FormatBool(bs.Negate),
				After:  strconv.FormatBool(as.Negate),
			})
		}
		if bs.Required != as.Required {
			d.ChangedConditions = append(d.ChangedConditions, ConditionChange{
				Name: name, Implementation: as.Implementation,
				Field:  "required",
				Before: strconv.FormatBool(bs.Required),
				After:  strconv.FormatBool(as.Required),
			})
		}
		// Equality gate uses ExtractFieldValue + valuesEqual — the
		// SAME comparison sync.go's cfSpecsMatch uses to decide if a CF
		// needs pushing. Using renderSpecValue here previously caused
		// false-positive drift: renderSpecValue's `default` case
		// (any spec implementation not in its switch) falls through to
		// renderRaw which re-marshals the raw bytes. TRaSH disk shape
		// {"value":X} and Arr live shape [{"name":"value","value":X}]
		// then produce different strings even when the underlying
		// value is identical, and every such CF instantly flags drift
		// the moment any Arr-only spec-type ships (typical: new Sonarr
		// implementation, future Radarr addition).
		//
		// Aligning to sync's comparator guarantees the invariant:
		// any CF sync would skip with Action="unchanged" must
		// ALSO be considered "no drift" by detection. Strings shown
		// in the Before/After fields still use renderSpecValue so the
		// History UI keeps its human-readable rendering — those are
		// only computed when an actual change is detected.
		beforeRaw := ExtractFieldValue(bs.Fields)
		afterRaw := ExtractFieldValue(as.Fields)
		if !valuesEqual(beforeRaw, afterRaw) {
			d.ChangedConditions = append(d.ChangedConditions, ConditionChange{
				Name: name, Implementation: as.Implementation,
				Field:  "value",
				Before: renderSpecValue(bs.Implementation, bs.Fields),
				After:  renderSpecValue(as.Implementation, as.Fields),
			})
		}
	}
	for name, as := range afterByName {
		if _, present := beforeByName[name]; !present {
			d.AddedConditions = append(d.AddedConditions, conditionRefFromSpec(as))
		}
	}

	// Stable orderings for both added and removed so the UI shows the
	// same list across re-renders and snapshot diffs stay quiet.
	sort.Slice(d.AddedConditions, func(i, j int) bool {
		return d.AddedConditions[i].Name < d.AddedConditions[j].Name
	})
	sort.Slice(d.RemovedConditions, func(i, j int) bool {
		return d.RemovedConditions[i].Name < d.RemovedConditions[j].Name
	})
	sort.Slice(d.ChangedConditions, func(i, j int) bool {
		if d.ChangedConditions[i].Name != d.ChangedConditions[j].Name {
			return d.ChangedConditions[i].Name < d.ChangedConditions[j].Name
		}
		return d.ChangedConditions[i].Field < d.ChangedConditions[j].Field
	})
	sort.Slice(d.SettingsChanges, func(i, j int) bool {
		return d.SettingsChanges[i].Field < d.SettingsChanges[j].Field
	})

	if !d.HasAny() {
		return nil
	}
	return d
}

func conditionRefFromSpec(s CFSpecification) ConditionRef {
	return ConditionRef{
		Name:           s.Name,
		Implementation: s.Implementation,
		Value:          renderSpecValue(s.Implementation, s.Fields),
		Negate:         s.Negate,
		Required:       s.Required,
	}
}

// rawJSONEqual returns true when two json.RawMessage payloads decode
// to the same value, ignoring whitespace and key ordering. Compared
// at the decoded-value level so a re-pretty-print on TRaSH's side
// doesn't show up as a spurious "changed" entry.
func rawJSONEqual(a, b json.RawMessage) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	// Re-marshal sorted (json package marshals maps with sorted keys
	// natively for map[string]X via reflection - so for the typical
	// "fields" object case, this produces a stable representation).
	abytes, err1 := json.Marshal(av)
	bbytes, err2 := json.Marshal(bv)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(abytes) == string(bbytes)
}

// renderSpecValue produces a human-readable string for the
// distinguishing fields of a single Specification, given its
// implementation type. Used both when listing added/removed specs and
// when describing a value-level change. Unknown implementations fall
// back to a "{...}"-style raw rendering so future TRaSH additions do
// not silently produce empty strings.
func renderSpecValue(impl string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch impl {
	case "ReleaseGroupSpecification", "ReleaseTitleSpecification", "EditionSpecification":
		return decodeStringField(raw, "value")
	case "ResolutionSpecification":
		return renderEnum(raw, "value", resolutionLabels)
	case "SourceSpecification":
		return renderEnum(raw, "value", sourceLabels)
	case "QualityModifierSpecification":
		return renderEnum(raw, "value", qualityModifierLabels)
	case "ReleaseTypeSpecification":
		return renderEnum(raw, "value", releaseTypeLabels)
	case "IndexerFlagSpecification":
		return renderEnum(raw, "value", indexerFlagLabels)
	case "LanguageSpecification":
		return renderEnum(raw, "value", languageLabels)
	case "SizeSpecification":
		return renderSizeSpec(raw)
	default:
		return renderRaw(raw)
	}
}

// extractField finds the value of a named field in a Specification's
// Fields payload, supporting both shapes the codebase sees in the
// wild:
//
//	{"value":"X","negate":false}         (TRaSH flat-object form)
//	[{"name":"value","value":"X"}, ...]  (Arr API name/value array)
//
// Returns nil when the field is absent or the shape isn't recognised.
func extractField(raw json.RawMessage, name string) any {
	if len(raw) == 0 {
		return nil
	}
	// Try flat-object form first - the TRaSH JSONs we ship use it and
	// the diff engine sees that shape on the "after" side every sync.
	obj := map[string]any{}
	if err := json.Unmarshal(raw, &obj); err == nil {
		if v, ok := obj[name]; ok {
			return v
		}
		return nil
	}
	// Fall back to Arr's array-of-field-objects shape - that's what
	// the "before" snapshot looks like after a fresh ListCustomFormats.
	arr := []map[string]any{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		for _, f := range arr {
			fname, _ := f["name"].(string)
			if fname == name {
				return f["value"]
			}
		}
	}
	return nil
}

func decodeStringField(raw json.RawMessage, key string) string {
	v := extractField(raw, key)
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func renderEnum(raw json.RawMessage, key string, labels map[int]string) string {
	v := extractField(raw, key)
	if v == nil {
		return ""
	}
	// JSON numbers decode to float64 through the any path.
	switch n := v.(type) {
	case float64:
		i := int(n)
		if label, ok := labels[i]; ok {
			return label
		}
		return fmt.Sprintf("%d (unknown)", i)
	case string:
		return n
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func renderSizeSpec(raw json.RawMessage) string {
	minV := extractField(raw, "min")
	maxV := extractField(raw, "max")
	if minV == nil && maxV == nil {
		return ""
	}
	return fmt.Sprintf("min=%v max=%v", minV, maxV)
}

func renderRaw(raw json.RawMessage) string {
	// Re-marshal compact so output is stable for snapshot tests and
	// fits inline in the History UI.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// --- Enum label tables ---
//
// Each map mirrors Radarr/Sonarr's internal enum values for the
// matching Specification. Values that are not present in the map fall
// through to "<N> (unknown)" so a future TRaSH/Arr enum addition does
// not silently produce empty UI strings. Keep these in sync with Arr
// upstream when running into an "unknown" rendering in the wild.

var resolutionLabels = map[int]string{
	360:  "360p",
	480:  "480p",
	540:  "540p",
	576:  "576p",
	720:  "720p",
	1080: "1080p",
	2160: "2160p",
}

var sourceLabels = map[int]string{
	0: "Unknown",
	1: "CAM",
	2: "Telesync",
	3: "Telecine",
	4: "Workprint",
	5: "DVD",
	6: "TV",
	7: "WEBDL",
	8: "WEBRip",
	9: "Bluray",
}

var qualityModifierLabels = map[int]string{
	0: "None",
	1: "Regional",
	2: "Screener",
	3: "Rawhd",
	4: "Brdisk",
	5: "Remux",
}

var releaseTypeLabels = map[int]string{
	0: "Unknown",
	1: "Single Episode",
	2: "Multi Episode",
	3: "Season Pack",
}

var indexerFlagLabels = map[int]string{
	1:  "G_Freeleech",
	2:  "G_Halfleech",
	4:  "G_DoubleUpload",
	8:  "PTP_Golden",
	16: "PTP_Approved",
	32: "HDB_Internal",
	64: "AHD_Internal",
}

var languageLabels = map[int]string{
	-2: "Original",
	-1: "Any",
	0:  "Unknown",
	1:  "English",
	2:  "French",
	3:  "Spanish",
	4:  "German",
	5:  "Italian",
	6:  "Danish",
	7:  "Dutch",
	8:  "Japanese",
	9:  "Icelandic",
	10: "Chinese",
	11: "Russian",
	12: "Polish",
	13: "Vietnamese",
	14: "Swedish",
	15: "Norwegian",
	16: "Finnish",
	17: "Turkish",
	18: "Portuguese",
	19: "Flemish",
	20: "Greek",
	21: "Korean",
	22: "Hungarian",
	23: "Hebrew",
	24: "Lithuanian",
	25: "Czech",
	26: "Hindi",
	27: "Romanian",
	28: "Thai",
	29: "Bulgarian",
	30: "Portuguese (Brazil)",
	31: "Arabic",
	32: "Ukrainian",
}

// ArrCFToTrashCFExported is the public wrapper for arrCFToTrashCF
// so the api package's diff-detail handler can reuse the conversion
// without exporting the lowercase implementation.
func ArrCFToTrashCFExported(a *arr.ArrCF) *TrashCF {
	return arrCFToTrashCF(a)
}

// arrCFToTrashCF converts an Arr-side CustomFormat into the
// diff-engine's TrashCF shape. The Arr API doesn't surface
// trash_scores - scores live on profile FormatItems instead - so the
// returned TrashCF has only Name, IncludeInRename, and Specifications
// populated. Used in ExecuteSyncPlan to feed both the live Arr-side
// "before" snapshot and the target "after" body into DiffCFSpecs.
func arrCFToTrashCF(a *arr.ArrCF) *TrashCF {
	if a == nil {
		return nil
	}
	specs := make([]CFSpecification, len(a.Specifications))
	for i, s := range a.Specifications {
		specs[i] = CFSpecification{
			Name:           s.Name,
			Implementation: s.Implementation,
			Negate:         s.Negate,
			Required:       s.Required,
			Fields:         s.Fields,
		}
	}
	return &TrashCF{
		Name:            a.Name,
		IncludeInRename: a.IncludeCustomFormatWhenRenaming,
		Specifications:  specs,
	}
}
