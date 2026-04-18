package core

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"gopkg.in/yaml.v3"
)

// recyclarrConfig represents a Recyclarr YAML configuration file (v7 + v8).
type recyclarrConfig struct {
	Radarr map[string]recyclarrInstance `yaml:"radarr"`
	Sonarr map[string]recyclarrInstance `yaml:"sonarr"`
}

type recyclarrInstance struct {
	QualityDefinition struct {
		Type string `yaml:"type"`
	} `yaml:"quality_definition"`

	QualityProfiles []recyclarrQualityProfile `yaml:"quality_profiles"`
	CustomFormats   []recyclarrCFEntry        `yaml:"custom_formats"`

	// v8: custom_format_groups (guide-backed group references)
	CustomFormatGroups *recyclarrCFGroups `yaml:"custom_format_groups"`
}

type recyclarrQualityProfile struct {
	Name    string `yaml:"name"`
	TrashID string `yaml:"trash_id"` // v8: guide-backed profile reference

	ResetUnmatchedScores *struct {
		Enabled bool     `yaml:"enabled"`
		Except  []string `yaml:"except"`
	} `yaml:"reset_unmatched_scores"`

	ScoreSet string `yaml:"score_set"` // v8: named score set from TRaSH

	Upgrade *struct {
		Allowed      bool   `yaml:"allowed"`
		UntilQuality string `yaml:"until_quality"`
		UntilScore   int    `yaml:"until_score"`
	} `yaml:"upgrade"`

	MinFormatScore        *int `yaml:"min_format_score"`
	MinUpgradeFormatScore *int `yaml:"min_upgrade_format_score"` // v8

	QualitySort string                  `yaml:"quality_sort"`
	Qualities   []recyclarrQualityEntry `yaml:"qualities"`
}

type recyclarrQualityEntry struct {
	Name      string   `yaml:"name"`
	Enabled   *bool    `yaml:"enabled"` // v8: explicit enable/disable
	Qualities []string `yaml:"qualities"`
}

type recyclarrCFEntry struct {
	TrashIDs       []string                   `yaml:"trash_ids"`
	AssignScoresTo []recyclarrScoreAssignment `yaml:"assign_scores_to"`
}

type recyclarrScoreAssignment struct {
	Name    string `yaml:"name"`
	TrashID string `yaml:"trash_id"` // v8: reference profile by trash_id instead of name
	Score   *int   `yaml:"score"`    // pointer to distinguish "score: 0" from "no score set"
}

// v8: custom_format_groups
type recyclarrCFGroups struct {
	Skip []string              `yaml:"skip"`
	Add  []recyclarrCFGroupAdd `yaml:"add"`
}

type recyclarrCFGroupAdd struct {
	TrashID        string                     `yaml:"trash_id"`
	AssignScoresTo []recyclarrScoreAssignment `yaml:"assign_scores_to"`
	SelectAll      bool                       `yaml:"select_all"`
	Select         []string                   `yaml:"select"`
	Exclude        []string                   `yaml:"exclude"`
}

// ImportedProfile is a user-imported or custom profile stored as per-file JSON.
// Fields align with TRaSH quality profile format for future TRaSH JSON export.
type ImportedProfile struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	AppType               string            `json:"appType"`                  // "radarr" or "sonarr"
	Source                string            `json:"source,omitempty"`         // "import" or "custom" (empty = import for backwards compat)
	QualityType           string            `json:"qualityType,omitempty"`    // quality definition type (e.g. "movie", "series")
	TrashProfileID        string            `json:"trashProfileId,omitempty"` // v8: guide-backed profile trash_id
	ScoreSet              string            `json:"scoreSet,omitempty"`       // v8: named score set from TRaSH (e.g. "sqp-1-1080p")
	UpgradeAllowed        bool              `json:"upgradeAllowed"`
	Cutoff                string            `json:"cutoff,omitempty"` // until_quality name
	CutoffScore           int               `json:"cutoffScore"`      // until_score
	MinFormatScore        int               `json:"minFormatScore"`
	MinUpgradeFormatScore int               `json:"minUpgradeFormatScore,omitempty"` // v8: min score delta for upgrade
	ResetUnmatchedScores  bool              `json:"resetUnmatchedScores,omitempty"`
	ResetExcept           []string          `json:"resetExcept,omitempty"` // CF names excluded from score reset
	Language              string            `json:"language,omitempty"`    // preferred language
	Qualities             []QualityItem     `json:"qualities,omitempty"`
	FormatItems           map[string]int    `json:"formatItems"`                 // trash_id -> score
	FormatComments        map[string]string `json:"formatComments,omitempty"`    // trash_id -> CF name
	FormatGroups          map[string]string `json:"formatGroups,omitempty"`      // trash_id -> group name (TRaSH CF group membership)
	RequiredCFs           []string          `json:"requiredCFs,omitempty"`       // trash_ids marked as required
	DefaultOnCFs          []string          `json:"defaultOnCFs,omitempty"`      // trash_ids that are optional but default-on (recommended)
	BaselineCFs           []string          `json:"baselineCFs,omitempty"`       // CFs from TRaSH template defaults (core + default groups)
	CoreCFIds             []string          `json:"coreCFIds,omitempty"`         // CFs from TRaSH profile coreCFs (for TRaSH JSON export)
	FormatItemCFs         map[string]bool   `json:"formatItemCFs,omitempty"`     // CFs in formatItems (required/mandatory)
	EnabledGroups         map[string]bool   `json:"enabledGroups,omitempty"`     // group trash_ids that are included
	CfStateOverrides      map[string]string `json:"cfStateOverrides,omitempty"`  // per-CF state overrides (required/optional)
	VariantGoldenRule     string            `json:"variantGoldenRule,omitempty"` // builder: HD/UHD/none
	VariantMisc           string            `json:"variantMisc,omitempty"`       // builder: Standard/SQP/none
	TrashDescription      string            `json:"trashDescription,omitempty"`  // dev mode: profile description for TRaSH export
	GroupNum              int               `json:"groupNum,omitempty"`          // dev mode: profile group number
	ImportedAt            string            `json:"importedAt"`
}

// FileItem interface methods for ImportedProfile.
func (p *ImportedProfile) GetID() string      { return p.ID }
func (p *ImportedProfile) GetName() string    { return p.Name }
func (p *ImportedProfile) SetName(n string)   { p.Name = n }
func (p *ImportedProfile) GetAppType() string { return p.AppType }

// sanitizeName strips HTML tags from names to prevent XSS when rendered with x-html.
func sanitizeName(s string) string {
	if !strings.ContainsAny(s, "<>&") {
		return s
	}
	return html.EscapeString(s)
}

// parseRecyclarrYAML parses a Recyclarr YAML config and extracts profiles.
// If trashData is provided (keyed by "radarr"/"sonarr"), v8 custom_format_groups are resolved.
// Supports three YAML layouts:
//  1. Full config:     radarr: { instance_name: { quality_profiles: ... } }
//  2. Named instance:  instance_name: { quality_profiles: ... }
//  3. Flat instance:   quality_profiles: [ ... ]
//
// Layouts 2 and 3 use hintAppType ("radarr"/"sonarr") to determine the app type.
// detectAppType infers radarr/sonarr from a Recyclarr instance's quality_definition.type.
// Returns "radarr" for "movie", "sonarr" for "series"/"anime", or the hint if undetectable.
func detectAppType(inst recyclarrInstance, hint string) string {
	switch strings.ToLower(inst.QualityDefinition.Type) {
	case "movie":
		return "radarr"
	case "series", "anime":
		return "sonarr"
	}
	if hint != "" && hint != "auto" && hint != "select" {
		return hint
	}
	return "radarr"
}

// mergeRecyclarrIncludes resolves `include: - config: filename` references in Recyclarr YAML.
// Uses semantic YAML merge: parses both main and include files, merges quality_profiles and
// custom_formats arrays per instance. Returns the merged YAML as a string.
func MergeRecyclarrIncludes(mainYAML string, includeFiles map[string]string) string {
	// Parse main config to find instances and their includes
	var raw map[string]map[string]map[string]interface{} // sonarr/radarr → instance → fields
	if err := yaml.Unmarshal([]byte(mainYAML), &raw); err != nil {
		return mainYAML // fallback to original if unparseable
	}

	// For each instance, find its include list and merge the referenced files
	for _, instances := range raw {
		for instName, inst := range instances {
			includeList, ok := inst["include"]
			if !ok {
				continue
			}
			includes, ok := includeList.([]interface{})
			if !ok {
				continue
			}

			// Collect filenames
			for _, inc := range includes {
				incMap, ok := inc.(map[string]interface{})
				if !ok {
					continue
				}
				fname, _ := incMap["config"].(string)
				if fname == "" {
					continue
				}
				content, ok := includeFiles[strings.ToLower(fname)]
				if !ok {
					continue
				}

				// Parse include file as a YAML fragment
				var fragment map[string]interface{}
				if err := yaml.Unmarshal([]byte(content), &fragment); err != nil {
					continue
				}

				// Merge quality_profiles
				if qp, ok := fragment["quality_profiles"]; ok {
					if qpList, ok := qp.([]interface{}); ok {
						existing, _ := inst["quality_profiles"].([]interface{})
						inst["quality_profiles"] = append(existing, qpList...)
					}
				}

				// Merge custom_formats
				if cf, ok := fragment["custom_formats"]; ok {
					if cfList, ok := cf.([]interface{}); ok {
						existing, _ := inst["custom_formats"].([]interface{})
						inst["custom_formats"] = append(existing, cfList...)
					}
				}
			}

			// Remove include key after merging
			delete(inst, "include")
			instances[instName] = inst
		}
	}

	// Re-serialize to YAML
	merged, err := yaml.Marshal(raw)
	if err != nil {
		return mainYAML
	}
	return string(merged)
}

func ParseRecyclarrYAML(yamlContent []byte, trashData map[string]*AppData, hintAppType string) ([]ImportedProfile, error) {
	// --- Layout 1: full config with radarr/sonarr top-level keys ---
	var cfg recyclarrConfig
	if err := yaml.Unmarshal(yamlContent, &cfg); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	var profiles []ImportedProfile

	for _, inst := range cfg.Radarr {
		var ad *AppData
		if trashData != nil {
			ad = trashData["radarr"]
		}
		ps := extractProfiles(inst, "radarr", ad)
		profiles = append(profiles, ps...)
	}
	for _, inst := range cfg.Sonarr {
		var ad *AppData
		if trashData != nil {
			ad = trashData["sonarr"]
		}
		ps := extractProfiles(inst, "sonarr", ad)
		profiles = append(profiles, ps...)
	}

	if len(profiles) > 0 {
		return profiles, nil
	}

	// --- Layout 3: flat instance (quality_profiles at top level) ---
	var flat recyclarrInstance
	if err := yaml.Unmarshal(yamlContent, &flat); err == nil && len(flat.QualityProfiles) > 0 {
		appType := detectAppType(flat, hintAppType)
		var ad *AppData
		if trashData != nil {
			ad = trashData[appType]
		}
		profiles = extractProfiles(flat, appType, ad)
		if len(profiles) > 0 {
			return profiles, nil
		}
	}

	// --- Layout 2: named instance (instance_name: { quality_profiles: ... }) ---
	var named map[string]recyclarrInstance
	if err := yaml.Unmarshal(yamlContent, &named); err == nil {
		for _, inst := range named {
			appType := detectAppType(inst, hintAppType)
			var ad *AppData
			if trashData != nil {
				ad = trashData[appType]
			}
			ps := extractProfiles(inst, appType, ad)
			profiles = append(profiles, ps...)
		}
		if len(profiles) > 0 {
			return profiles, nil
		}
	}

	return nil, fmt.Errorf("no profiles found in YAML — ensure the file has quality_profiles and custom_formats sections")
}

// extractProfiles extracts ImportedProfile structs from a Recyclarr instance config.
// If ad is non-nil, v8 guide-backed profiles and custom_format_groups are resolved.
func extractProfiles(inst recyclarrInstance, appType string, ad *AppData) []ImportedProfile {
	// Build profile lookup for guide-backed trash_id resolution
	var trashProfileByID map[string]*TrashQualityProfile
	if ad != nil {
		trashProfileByID = make(map[string]*TrashQualityProfile)
		for _, p := range ad.Profiles {
			trashProfileByID[p.TrashID] = p
		}
	}

	// v8: Resolve guide-backed profile names before processing CFs.
	// This lets assign_scores_to reference guide-resolved names.
	for i := range inst.QualityProfiles {
		qp := &inst.QualityProfiles[i]
		if qp.TrashID != "" && qp.Name == "" && trashProfileByID != nil {
			if tp, ok := trashProfileByID[qp.TrashID]; ok {
				qp.Name = tp.Name
			}
		}
	}

	// Build per-profile CF score maps from custom_formats entries
	profileScores := make(map[string]map[string]int)
	profileComments := make(map[string]map[string]string)

	for _, cfEntry := range inst.CustomFormats {
		for _, assign := range cfEntry.AssignScoresTo {
			profileName := resolveAssignName(assign, trashProfileByID)
			if profileName == "" {
				continue
			}
			if _, ok := profileScores[profileName]; !ok {
				profileScores[profileName] = make(map[string]int)
				profileComments[profileName] = make(map[string]string)
			}
			for _, tid := range cfEntry.TrashIDs {
				cleanID, comment := parseTrashIDComment(tid)
				score := 0
				if assign.Score != nil {
					score = *assign.Score
				} else if ad != nil {
					// No explicit score — resolve from TRaSH CF data
					if trashCF, ok := ad.CustomFormats[cleanID]; ok {
						scoreSet := "default"
						for _, qp := range inst.QualityProfiles {
							resolvedName := qp.Name
							if resolvedName == "" && qp.TrashID != "" {
								if tp, ok := trashProfileByID[qp.TrashID]; ok {
									resolvedName = tp.Name
								}
							}
							if resolvedName == profileName && qp.ScoreSet != "" {
								scoreSet = qp.ScoreSet
								break
							}
						}
						if s, ok := trashCF.TrashScores[scoreSet]; ok {
							score = s
						} else if s, ok := trashCF.TrashScores["default"]; ok {
							score = s
						}
					}
				}
				profileScores[profileName][cleanID] = score
				if comment != "" {
					profileComments[profileName][cleanID] = comment
				}
			}
		}
	}

	// Track CF → group membership (for TRaSH JSON export)
	profileFormatGroups := make(map[string]map[string]string) // profileName → trash_id → group name

	// Resolve v8 custom_format_groups (requires TRaSH data)
	resolveCustomFormatGroups(inst, ad, profileScores, profileComments, profileFormatGroups)

	// Resolve group membership for CFs from custom_formats section (using TRaSH data)
	if ad != nil {
		cfToGroup := make(map[string]string) // trash_id → group name
		for _, g := range ad.CFGroups {
			for _, cf := range g.CustomFormats {
				cfToGroup[cf.TrashID] = g.Name
			}
		}
		for profileName, scores := range profileScores {
			if _, ok := profileFormatGroups[profileName]; !ok {
				profileFormatGroups[profileName] = make(map[string]string)
			}
			for tid := range scores {
				if gn, ok := cfToGroup[tid]; ok {
					if _, exists := profileFormatGroups[profileName][tid]; !exists {
						profileFormatGroups[profileName][tid] = gn
					}
				}
			}
		}
	}

	var profiles []ImportedProfile

	for _, qp := range inst.QualityProfiles {
		name := qp.Name
		if name == "" {
			continue // skip profiles without a name
		}

		// v8: Resolve guide-backed profile — populate qualities, cutoff, language, scores from TRaSH
		var trashProfile *TrashQualityProfile
		if qp.TrashID != "" && trashProfileByID != nil {
			trashProfile = trashProfileByID[qp.TrashID]
		}

		scores := profileScores[name]
		if scores == nil {
			scores = make(map[string]int)
		}
		comments := profileComments[name]

		// v8: Merge guide-backed CF scores (from formatItems + trash_scores)
		if trashProfile != nil && ad != nil {
			scoreSet := qp.ScoreSet
			if scoreSet == "" {
				scoreSet = trashProfile.TrashScoreSet
			}
			if scoreSet == "" {
				scoreSet = "default"
			}
			// Resolve scores from TRaSH CFs referenced by the guide profile
			for cfName, cfTrashID := range trashProfile.FormatItems {
				if _, exists := scores[cfTrashID]; exists {
					continue // explicit custom_formats / groups take precedence
				}
				if cf, ok := ad.CustomFormats[cfTrashID]; ok {
					// Use score from the matching score_set context
					if s, ok := cf.TrashScores[scoreSet]; ok {
						scores[cfTrashID] = s
					} else if s, ok := cf.TrashScores["default"]; ok {
						scores[cfTrashID] = s
					}
					if comments == nil {
						comments = make(map[string]string)
					}
					comments[cfTrashID] = cfName
				}
			}
		}

		// Build qualities list — prefer YAML-specified, fall back to guide
		var qualities []QualityItem
		if len(qp.Qualities) > 0 {
			for _, q := range qp.Qualities {
				enabled := true
				if q.Enabled != nil {
					enabled = *q.Enabled
				}
				qi := QualityItem{
					Name:    q.Name,
					Allowed: enabled,
				}
				if len(q.Qualities) > 0 {
					qi.Items = q.Qualities
				}
				qualities = append(qualities, qi)
			}
		} else if trashProfile != nil {
			qualities = trashProfile.Items
		}

		p := ImportedProfile{
			ID:             GenerateID(),
			Name:           sanitizeName(name),
			AppType:        appType,
			QualityType:    inst.QualityDefinition.Type,
			TrashProfileID: qp.TrashID,
			ScoreSet:       qp.ScoreSet,
			Qualities:      qualities,
			FormatItems:    scores,
		}
		if len(comments) > 0 {
			p.FormatComments = comments
		}
		if fg := profileFormatGroups[name]; len(fg) > 0 {
			p.FormatGroups = fg
		}

		// v8: Use guide profile defaults when YAML doesn't specify them
		if qp.Upgrade != nil {
			p.UpgradeAllowed = qp.Upgrade.Allowed
			p.Cutoff = qp.Upgrade.UntilQuality
			p.CutoffScore = qp.Upgrade.UntilScore
		} else if trashProfile != nil {
			p.UpgradeAllowed = trashProfile.UpgradeAllowed
			p.Cutoff = trashProfile.Cutoff
			p.CutoffScore = trashProfile.CutoffFormatScore
		}
		if qp.MinFormatScore != nil {
			p.MinFormatScore = *qp.MinFormatScore
		} else if trashProfile != nil {
			p.MinFormatScore = trashProfile.MinFormatScore
		}
		if qp.MinUpgradeFormatScore != nil {
			p.MinUpgradeFormatScore = *qp.MinUpgradeFormatScore
		} else if trashProfile != nil {
			p.MinUpgradeFormatScore = trashProfile.MinUpgradeFormatScore
		}

		// v8: Language from guide profile
		if trashProfile != nil && p.Language == "" {
			p.Language = trashProfile.Language
		}

		// v8: ScoreSet from guide profile
		if trashProfile != nil && p.ScoreSet == "" {
			p.ScoreSet = trashProfile.TrashScoreSet
		}

		// Preserve reset_unmatched_scores settings
		if qp.ResetUnmatchedScores != nil {
			p.ResetUnmatchedScores = qp.ResetUnmatchedScores.Enabled
			p.ResetExcept = qp.ResetUnmatchedScores.Except
		}

		profiles = append(profiles, p)
	}

	// Attach CF scores from assign_scores_to to matching quality_profiles.
	// Profiles referenced only in assign_scores_to (not in quality_profiles)
	// are skipped — they likely come from included files we don't have access to.
	// Their scores are already merged into matching profiles above (line 328).

	return profiles
}

// resolveAssignName resolves the profile name from a score assignment.
// v8 allows referencing profiles by trash_id instead of name.
func resolveAssignName(assign recyclarrScoreAssignment, trashProfileByID map[string]*TrashQualityProfile) string {
	if assign.Name != "" {
		return assign.Name
	}
	if assign.TrashID != "" && trashProfileByID != nil {
		if tp, ok := trashProfileByID[assign.TrashID]; ok {
			return tp.Name
		}
	}
	return ""
}

// resolveCustomFormatGroups expands v8 custom_format_groups references into
// individual CF scores using TRaSH group data. Must be called after parsing
// when TRaSH data is available.
func resolveCustomFormatGroups(inst recyclarrInstance, ad *AppData, profileScores map[string]map[string]int, profileComments map[string]map[string]string, profileFormatGroups map[string]map[string]string) {
	if inst.CustomFormatGroups == nil || ad == nil {
		return
	}

	// Build profile lookup for trash_id resolution in assign_scores_to
	trashProfileByID := make(map[string]*TrashQualityProfile)
	for _, p := range ad.Profiles {
		trashProfileByID[p.TrashID] = p
	}

	// Build group lookup: trash_id → TrashCFGroup
	groupByID := make(map[string]*TrashCFGroup)
	for _, g := range ad.CFGroups {
		groupByID[g.TrashID] = g
	}

	// Build skip set
	skipSet := make(map[string]bool)
	for _, id := range inst.CustomFormatGroups.Skip {
		skipSet[id] = true
	}

	for _, add := range inst.CustomFormatGroups.Add {
		if skipSet[add.TrashID] {
			continue
		}
		group, ok := groupByID[add.TrashID]
		if !ok {
			continue
		}

		// Determine which CFs to include from this group
		selectSet := make(map[string]bool)
		excludeSet := make(map[string]bool)
		for _, id := range add.Select {
			selectSet[id] = true
		}
		for _, id := range add.Exclude {
			excludeSet[id] = true
		}

		for _, cf := range group.CustomFormats {
			// Apply select/exclude filters
			if excludeSet[cf.TrashID] {
				continue
			}
			// If neither selectAll nor explicit select list, skip all (require opt-in)
			if !add.SelectAll && !selectSet[cf.TrashID] {
				continue
			}

			// Assign scores to each referenced profile
			for _, assign := range add.AssignScoresTo {
				profileName := resolveAssignName(assign, trashProfileByID)
				if profileName == "" {
					continue
				}
				if _, ok := profileScores[profileName]; !ok {
					profileScores[profileName] = make(map[string]int)
					profileComments[profileName] = make(map[string]string)
				}
				// Only set if not already set by custom_formats (explicit scores take precedence)
				if _, exists := profileScores[profileName][cf.TrashID]; !exists {
					score := 0
					if assign.Score != nil {
						score = *assign.Score
					} else {
						// No explicit score — resolve from TRaSH CF data using profile's score_set
						if trashCF, ok := ad.CustomFormats[cf.TrashID]; ok {
							// Find score_set from the target profile
							scoreSet := "default"
							for _, qp := range inst.QualityProfiles {
								resolvedName := qp.Name
								if resolvedName == "" && qp.TrashID != "" {
									if tp, ok := trashProfileByID[qp.TrashID]; ok {
										resolvedName = tp.Name
									}
								}
								if resolvedName == profileName && qp.ScoreSet != "" {
									scoreSet = qp.ScoreSet
									break
								}
							}
							if s, ok := trashCF.TrashScores[scoreSet]; ok {
								score = s
							} else if s, ok := trashCF.TrashScores["default"]; ok {
								score = s
							}
						}
					}
					profileScores[profileName][cf.TrashID] = score
					if cf.Name != "" {
						profileComments[profileName][cf.TrashID] = cf.Name
					}
				}
				// Track group membership
				if profileFormatGroups != nil {
					if _, ok := profileFormatGroups[profileName]; !ok {
						profileFormatGroups[profileName] = make(map[string]string)
					}
					profileFormatGroups[profileName][cf.TrashID] = group.Name
				}
			}
		}
	}
}

// parseProfileJSON auto-detects and parses either a TRaSH quality profile JSON
// or a Clonarr export JSON into an ImportedProfile.
//
// Detection: TRaSH profiles have "trash_id" + "formatItems" as map[string]string (name→trashId).
// Clonarr exports have "id" + "formatItems" as map[string]int (trashId→score).
func ParseProfileJSON(data []byte, appType string, ad *AppData, trashData map[string]*AppData) (*ImportedProfile, error) {
	// Probe the JSON to detect format
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	if _, ok := probe["trash_id"]; ok {
		// Auto-detect app type by matching trash_id against TRaSH profiles
		if appType == "" || appType == "select" || appType == "auto" {
			var trashID string
			json.Unmarshal(probe["trash_id"], &trashID)
			if trashID != "" && trashData != nil {
				for _, tryType := range []string{"radarr", "sonarr"} {
					if tryAd := trashData[tryType]; tryAd != nil {
						for _, p := range tryAd.Profiles {
							if p.TrashID == trashID {
								appType = tryType
								ad = tryAd
								break
							}
						}
					}
					if appType != "" && appType != "select" && appType != "auto" {
						break
					}
				}
			}
			if appType == "" || appType == "select" || appType == "auto" {
				appType = "radarr" // fallback
			}
		}
		return parseTrashProfileJSON(data, appType, ad)
	}
	if _, ok := probe["id"]; ok {
		if _, hasApp := probe["appType"]; hasApp {
			return parseClonarrExportJSON(data, ad)
		}
	}
	return nil, fmt.Errorf("unrecognized JSON format — expected TRaSH profile (has trash_id) or Clonarr export (has id + appType)")
}

// parseTrashProfileJSON converts a TRaSH quality profile JSON into an ImportedProfile.
func parseTrashProfileJSON(data []byte, appType string, ad *AppData) (*ImportedProfile, error) {
	var tp TrashQualityProfile
	if err := json.Unmarshal(data, &tp); err != nil {
		return nil, fmt.Errorf("parse TRaSH profile: %w", err)
	}
	if tp.TrashID == "" || tp.Name == "" {
		return nil, fmt.Errorf("TRaSH profile missing trash_id or name")
	}

	scoreCtx := tp.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Resolve formatItems: TRaSH uses name→trashId, we need trashId→score
	formatItems := make(map[string]int)
	formatComments := make(map[string]string)
	for cfName, trashID := range tp.FormatItems {
		var score int
		if ad != nil {
			if cf, ok := ad.CustomFormats[trashID]; ok {
				if s, ok := cf.TrashScores[scoreCtx]; ok {
					score = s
				} else if s, ok := cf.TrashScores["default"]; ok {
					score = s
				}
				cfName = cf.Name // use canonical name
			}
		}
		formatItems[trashID] = score
		formatComments[trashID] = cfName
	}

	// Convert quality items
	var qualities []QualityItem
	for _, item := range tp.Items {
		qualities = append(qualities, item)
	}

	// Resolve formatGroups from TRaSH CF group data
	var formatGroups map[string]string
	if ad != nil {
		formatGroups = make(map[string]string)
		cfToGroup := make(map[string]string)
		for _, g := range ad.CFGroups {
			for _, cf := range g.CustomFormats {
				cfToGroup[cf.TrashID] = g.Name
			}
		}
		for tid := range formatItems {
			if gn, ok := cfToGroup[tid]; ok {
				formatGroups[tid] = gn
			}
		}
	}

	profile := &ImportedProfile{
		ID:                    GenerateID(),
		Name:                  sanitizeName(tp.Name),
		AppType:               appType,
		Source:                "import",
		TrashProfileID:        tp.TrashID,
		ScoreSet:              tp.TrashScoreSet,
		UpgradeAllowed:        tp.UpgradeAllowed,
		Cutoff:                tp.Cutoff,
		CutoffScore:           tp.CutoffFormatScore,
		MinFormatScore:        tp.MinFormatScore,
		MinUpgradeFormatScore: tp.MinUpgradeFormatScore,
		Language:              tp.Language,
		Qualities:             qualities,
		FormatItems:           formatItems,
		FormatComments:        formatComments,
		FormatGroups:          formatGroups,
		TrashDescription:      tp.TrashDescription,
		GroupNum:              tp.Group,
	}
	return profile, nil
}

// parseClonarrExportJSON parses a Clonarr export JSON (same structure as ImportedProfile).
// If ad is provided, resolves missing formatGroups from TRaSH CF group data.
func parseClonarrExportJSON(data []byte, ad *AppData) (*ImportedProfile, error) {
	var profile ImportedProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("parse Clonarr export: %w", err)
	}
	if profile.Name == "" {
		return nil, fmt.Errorf("Clonarr export missing name")
	}
	// Generate new ID to avoid collisions
	profile.ID = GenerateID()
	profile.Name = sanitizeName(profile.Name)
	profile.Source = "import"

	// Resolve missing formatGroups from TRaSH data
	if ad != nil && len(profile.FormatItems) > 0 {
		if profile.FormatGroups == nil {
			profile.FormatGroups = make(map[string]string)
		}
		cfToGroup := make(map[string]string)
		for _, g := range ad.CFGroups {
			for _, cf := range g.CustomFormats {
				cfToGroup[cf.TrashID] = g.Name
			}
		}
		for tid := range profile.FormatItems {
			if _, exists := profile.FormatGroups[tid]; !exists {
				if gn, ok := cfToGroup[tid]; ok {
					profile.FormatGroups[tid] = gn
				}
			}
		}
	}

	return &profile, nil
}

// parseTrashIDComment extracts the clean trash_id and optional inline comment.
// Input: "496f355514737f7d83bf7aa4d24f8169 # TrueHD Atmos" → ("496f...", "TrueHD Atmos")
func parseTrashIDComment(raw string) (string, string) {
	if idx := strings.Index(raw, "#"); idx >= 0 {
		return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	}
	return strings.TrimSpace(raw), ""
}
