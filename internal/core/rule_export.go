package core

import "strings"

// Portable sync-rule export. A self-contained, clonarr-native representation of
// one sync rule that re-imports as a 1:1 identical rule on another machine.
// See docs/clonarr/sync-rule-import-export-plan.md.
//
// The envelope (ClonarrExport + SchemaVersion + App) identifies the file so
// import can reject Recyclarr / TRaSH / other JSON cleanly instead of
// half-importing something incompatible.

const (
	// SyncRuleExportMarker is the required value of the top-level
	// "clonarrExport" field. Import refuses any file without it.
	SyncRuleExportMarker = "sync-rule"
	// SyncRuleExportSchema is the current export schema version. Bump on any
	// breaking shape change; readers must check it for forward-compat.
	SyncRuleExportSchema = 1
)

// SyncRuleExport is the top-level export envelope.
type SyncRuleExport struct {
	ClonarrExport string            `json:"clonarrExport"` // always SyncRuleExportMarker
	SchemaVersion int               `json:"schemaVersion"` // SyncRuleExportSchema
	ExportedBy    string            `json:"exportedBy,omitempty"`
	App           string            `json:"app"` // "radarr" | "sonarr"
	BaseProfile   ExportBaseProfile `json:"baseProfile"`
	Rule          ExportedRule      `json:"rule"`
	// CustomFormats bundles the FULL definition of every non-TRaSH custom CF
	// the rule references, read from clonarr's own registry (no Arr needed).
	// TRaSH CFs are NOT bundled, they are referenced by trash_id in the rule
	// and resolved from the target's TRaSH data.
	CustomFormats []CustomCF `json:"customFormats,omitempty"`
}

// ExportBaseProfile identifies the profile the rule is built on.
type ExportBaseProfile struct {
	Source  string `json:"source"`            // "trash" | "imported"
	TrashID string `json:"trashId,omitempty"` // for trash-based rules
	Name    string `json:"name,omitempty"`    // display + validation
}

// ExportedRule is the portable subset of AutoSyncRule: everything that defines
// the profile customization, none of the instance/runtime fields (InstanceID,
// ArrProfileID, ID, Enabled, KeepArrCFIDs, LastSync*, timestamps).
type ExportedRule struct {
	SelectedCFs      []string        `json:"selectedCFs,omitempty"`
	ExcludedCFs      []string        `json:"excludedCFs,omitempty"`
	ScoreOverrides   map[string]int  `json:"scoreOverrides,omitempty"`
	QualityOverrides map[string]bool `json:"qualityOverrides,omitempty"`
	QualityStructure []QualityItem   `json:"qualityStructure,omitempty"`
	Behavior         *SyncBehavior   `json:"behavior,omitempty"`
	Overrides        *SyncOverrides  `json:"overrides,omitempty"`
	Description      string          `json:"description,omitempty"`
}

// BuildSyncRuleExport assembles the portable export for a rule. customCFByID is
// the source machine's custom CF registry keyed by ID; profileName is resolved
// by the caller from the TRaSH snapshot; version is the running clonarr version.
// Only custom CFs the rule actually references are bundled.
func BuildSyncRuleExport(rule *AutoSyncRule, app, profileName, version string, customCFByID map[string]CustomCF) SyncRuleExport {
	out := SyncRuleExport{
		ClonarrExport: SyncRuleExportMarker,
		SchemaVersion: SyncRuleExportSchema,
		ExportedBy:    version,
		App:           app,
		BaseProfile: ExportBaseProfile{
			Source:  rule.ProfileSource,
			TrashID: rule.TrashProfileID,
			Name:    profileName,
		},
		Rule: ExportedRule{
			SelectedCFs:      rule.SelectedCFs,
			ExcludedCFs:      rule.ExcludedCFs,
			ScoreOverrides:   rule.ScoreOverrides,
			QualityOverrides: rule.QualityOverrides,
			QualityStructure: rule.QualityStructure,
			Behavior:         rule.Behavior,
			Overrides:        rule.Overrides,
			Description:      rule.Description,
		},
	}

	// Bundle definitions for every custom CF the rule references. Custom CF IDs
	// carry the "custom:" prefix in SelectedCFs and as ScoreOverrides keys.
	seen := map[string]bool{}
	bundle := func(id string) {
		if !strings.HasPrefix(id, "custom:") || seen[id] {
			return
		}
		seen[id] = true
		cf, ok := customCFByID[id]
		if !ok {
			return
		}
		// Strip source-instance-specific fields; the definition itself is
		// what travels.
		cf.ArrID = 0
		cf.SourceInstance = ""
		cf.ImportedAt = ""
		out.CustomFormats = append(out.CustomFormats, cf)
	}
	for _, id := range rule.SelectedCFs {
		bundle(id)
	}
	for id := range rule.ScoreOverrides {
		bundle(id)
	}
	return out
}
