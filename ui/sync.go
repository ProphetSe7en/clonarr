package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// --- Sync Plan Types ---

// SyncPlan describes what changes would be made to an Arr instance.
type SyncPlan struct {
	InstanceID     string        `json:"instanceId"`
	InstanceName   string        `json:"instanceName"`
	ProfileName    string        `json:"profileName"`
	ArrProfileName string        `json:"arrProfileName,omitempty"`
	CreateProfile  bool          `json:"createProfile,omitempty"`
	NewProfileName string        `json:"newProfileName,omitempty"`
	CFActions      []CFAction    `json:"cfActions"`
	ScoreActions   []ScoreAction `json:"scoreActions"`
	Summary        SyncSummary   `json:"summary"`
}

// CFAction describes a CF create/update/unchanged action.
type CFAction struct {
	TrashID string `json:"trashId"`
	Name    string `json:"name"`
	Action  string `json:"action"` // "create", "update", "unchanged"
	ArrID   int    `json:"arrId,omitempty"`
}

// ScoreAction describes a score change in a profile.
type ScoreAction struct {
	CFName   string `json:"cfName"`
	ArrCFID  int    `json:"arrCfId"`
	OldScore int    `json:"oldScore"`
	NewScore int    `json:"newScore"`
	Action   string `json:"action"` // "set", "update", "unchanged"
}

// HasChanges returns true if the plan contains any create/update actions.
func (p *SyncPlan) HasChanges() bool {
	return p.Summary.CFsToCreate > 0 || p.Summary.CFsToUpdate > 0 ||
		p.Summary.ScoresToSet > 0 || p.Summary.ScoresToZero > 0 ||
		p.Summary.QualityChanged || p.CreateProfile
}

// SyncSummary holds counts of planned changes.
type SyncSummary struct {
	CFsToCreate     int  `json:"cfsToCreate"`
	CFsToUpdate     int  `json:"cfsToUpdate"`
	QualityChanged  bool `json:"qualityChanged,omitempty"` // quality items differ from TRaSH
	CFsUnchanged    int `json:"cfsUnchanged"`
	ScoresToSet     int `json:"scoresToSet"`
	ScoresUnchanged int `json:"scoresUnchanged"`
	ScoresToZero    int `json:"scoresToZero"` // extra CFs that will be zeroed out
}

// --- Sync Request ---

// SyncRequest is the input for a dry-run or apply operation.
type SyncRequest struct {
	InstanceID        string         `json:"instanceId"`
	ProfileTrashID    string         `json:"profileTrashId"`
	ImportedProfileID string         `json:"importedProfileId,omitempty"` // alternative: sync from imported/custom profile
	ArrProfileID      int            `json:"arrProfileId"`                // target Arr profile to set scores on (0 = create new)
	ProfileName       string         `json:"profileName"`                 // custom name for new profile (optional)
	SelectedCFs       []string       `json:"selectedCFs"`                 // optional: additional CF trash_ids from groups
	ScoreOverrides    map[string]int  `json:"scoreOverrides,omitempty"`    // per-CF score overrides (trash_id → score)
	QualityOverrides  map[string]bool `json:"qualityOverrides,omitempty"`  // quality item overrides (name → allowed)
	// Profile setting overrides (nil = use TRaSH default)
	Overrides *SyncOverrides `json:"overrides,omitempty"`
	// Sync behavior rules (nil = defaults: add_missing, remove_custom, reset_to_zero)
	Behavior *SyncBehavior `json:"behavior,omitempty"`
}

// SyncOverrides allows users to override specific profile settings from TRaSH defaults.
type SyncOverrides struct {
	Language              *string `json:"language,omitempty"`
	UpgradeAllowed        *bool   `json:"upgradeAllowed,omitempty"`
	MinFormatScore        *int    `json:"minFormatScore,omitempty"`
	MinUpgradeFormatScore *int    `json:"minUpgradeFormatScore,omitempty"`
	CutoffFormatScore     *int    `json:"cutoffFormatScore,omitempty"`
	CutoffQuality         *string `json:"cutoffQuality,omitempty"` // override cutoff quality group name
}

// resolveScore returns the desired score for a CF. For imported profiles, scores
// come from cfScoreOverrides. For TRaSH profiles, from TrashScores with scoreCtx.
func resolveScore(trashID string, trashCF *TrashCF, scoreCtx string, cfScoreOverrides map[string]int) int {
	if cfScoreOverrides != nil {
		if s, ok := cfScoreOverrides[trashID]; ok {
			return s
		}
	}
	if s, ok := trashCF.TrashScores[scoreCtx]; ok {
		return s
	}
	if s, ok := trashCF.TrashScores["default"]; ok {
		return s
	}
	return 0
}

// --- Build Sync Plan ---

// BuildSyncPlan compares TRaSH profile CFs against an Arr instance and produces a plan.
// Supports both TRaSH profiles (via ProfileTrashID) and imported/custom profiles (via ImportedProfile).
// customCFs allows syncing user-created CFs (IDs starting with "custom:").
// lastSyncedCFs is the CF snapshot from the previous sync (used by "add_new" mode).
func BuildSyncPlan(ad *AppData, instance Instance, req SyncRequest, imported *ImportedProfile, customCFs []CustomCF, lastSyncedCFs []string) (*SyncPlan, error) {
	if ad == nil {
		return nil, fmt.Errorf("no TRaSH data for %s", instance.Type)
	}

	var profile *TrashQualityProfile
	var scoreCtx string

	// cfScoreOverrides: for imported profiles, scores come from the profile itself (not TRaSH)
	var cfScoreOverrides map[string]int

	customCFMap := buildCustomCFMap(customCFs)

	if imported != nil {
		// Imported/custom profile: build a minimal TRaSH profile wrapper
		// FormatItems maps CF name → trash_id for compatibility with the sync engine
		fi := make(map[string]string, len(imported.FormatItems))
		for tid := range imported.FormatItems {
			if cf, ok := ad.CustomFormats[tid]; ok {
				fi[cf.Name] = tid
			} else if ccf, ok := customCFMap[tid]; ok {
				fi[ccf.Name] = tid
			}
		}
		profile = &TrashQualityProfile{
			Name:                  imported.Name,
			UpgradeAllowed:        imported.UpgradeAllowed,
			Cutoff:                imported.Cutoff,
			MinFormatScore:        imported.MinFormatScore,
			CutoffFormatScore:     imported.CutoffScore,
			MinUpgradeFormatScore: imported.MinUpgradeFormatScore,
			Language:              imported.Language,
			Items:                 imported.Qualities,
			FormatItems:           fi,
		}
		scoreCtx = imported.ScoreSet
		cfScoreOverrides = imported.FormatItems
	} else {
		// TRaSH profile: look up by trash_id
		for _, p := range ad.Profiles {
			if p.TrashID == req.ProfileTrashID {
				profile = p
				break
			}
		}
		if profile == nil {
			return nil, fmt.Errorf("TRaSH profile %s not found", req.ProfileTrashID)
		}
		scoreCtx = profile.TrashScoreSet
	}

	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Merge per-CF score overrides from request
	if len(req.ScoreOverrides) > 0 {
		if cfScoreOverrides == nil {
			cfScoreOverrides = make(map[string]int)
		}
		for k, v := range req.ScoreOverrides {
			cfScoreOverrides[k] = v
		}
	}

	// Resolve sync behavior rules
	behavior := ResolveSyncBehavior(req.Behavior)

	// Build last-synced CF set for "add_new" mode
	lastSyncedSet := make(map[string]bool, len(lastSyncedCFs))
	for _, id := range lastSyncedCFs {
		lastSyncedSet[id] = true
	}

	// Connect to Arr instance
	client := NewArrClient(instance.URL, instance.APIKey)

	// Fetch existing CFs
	existingCFs, err := client.ListCustomFormats()
	if err != nil {
		return nil, fmt.Errorf("list CFs: %w", err)
	}

	// Build name → existing CF map (case-insensitive keys for matching)
	existingByName := make(map[string]*ArrCF)
	for i := range existingCFs {
		existingByName[strings.ToLower(existingCFs[i].Name)] = &existingCFs[i]
	}

	plan := &SyncPlan{
		InstanceID:   instance.ID,
		InstanceName: instance.Name,
		ProfileName:  profile.Name,
		CFActions:    []CFAction{},
		ScoreActions: []ScoreAction{},
	}

	// Merge core formatItems + selectedCFs into one set, keyed by trash_id (C2: no name collisions)
	allCFTrashIDs := make(map[string]bool)
	for _, cfTrashID := range profile.FormatItems {
		allCFTrashIDs[cfTrashID] = true
	}
	for _, trashID := range req.SelectedCFs {
		allCFTrashIDs[trashID] = true
	}
	log.Printf("Sync plan: %d core formatItems + %d selectedCFs = %d total CFs",
		len(profile.FormatItems), len(req.SelectedCFs), len(allCFTrashIDs))

	// Map: CF name → Arr CF ID (for score assignment)
	cfNameToArrID := make(map[string]int)

	// Compare each CF in the merged set
	for cfTrashID := range allCFTrashIDs {
		var cfName string
		var specsMatch func(existing *ArrCF) bool

		if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
			cfName = trashCF.Name
			specsMatch = func(existing *ArrCF) bool { return cfSpecsMatch(trashCF, existing) }
		} else if ccf, ok := customCFMap[cfTrashID]; ok {
			cfName = ccf.Name
			arrCF := customCFToArr(ccf)
			specsMatch = func(existing *ArrCF) bool { return arrCFSpecsMatch(arrCF, existing) }
		} else {
			log.Printf("Warning: CF %s referenced by profile but not found", cfTrashID)
			continue
		}

		existing, found := existingByName[strings.ToLower(cfName)]
		if !found {
			// Add mode: controls whether missing CFs get created
			switch behavior.AddMode {
			case "do_not_add":
				continue // skip entirely
			case "add_new":
				if lastSyncedSet[cfTrashID] {
					continue // was in last sync — user removed it, respect that
				}
				// genuinely new CF from guide, fall through to create
			}
			plan.CFActions = append(plan.CFActions, CFAction{
				TrashID: cfTrashID,
				Name:    cfName,
				Action:  "create",
			})
			plan.Summary.CFsToCreate++
		} else {
			cfNameToArrID[cfName] = existing.ID
			if specsMatch(existing) {
				plan.CFActions = append(plan.CFActions, CFAction{
					TrashID: cfTrashID,
					Name:    cfName,
					Action:  "unchanged",
					ArrID:   existing.ID,
				})
				plan.Summary.CFsUnchanged++
			} else {
				plan.CFActions = append(plan.CFActions, CFAction{
					TrashID: cfTrashID,
					Name:    cfName,
					Action:  "update",
					ArrID:   existing.ID,
				})
				plan.Summary.CFsToUpdate++
			}
		}
	}

	// Plan score assignments
	if req.ArrProfileID > 0 {
		// Update mode: compare against existing profile
		arrProfiles, err := client.ListProfiles()
		if err != nil {
			return nil, fmt.Errorf("list profiles: %w", err)
		}

		var targetProfile *ArrQualityProfile
		for i := range arrProfiles {
			if arrProfiles[i].ID == req.ArrProfileID {
				targetProfile = &arrProfiles[i]
				break
			}
		}
		if targetProfile == nil {
			return nil, fmt.Errorf("target profile no longer exists in Arr (internal ID %d) — it may have been deleted or recreated", req.ArrProfileID)
		}

		plan.ArrProfileName = targetProfile.Name

		// Build current score map
		currentScores := make(map[int]int) // Arr CF ID → score
		for _, fi := range targetProfile.FormatItems {
			currentScores[fi.Format] = fi.Score
		}

		// Plan score changes (using merged set)
		for cfTrashID := range allCFTrashIDs {
			var cfName string
			var desiredScore int

			if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
				cfName = trashCF.Name
				desiredScore = resolveScore(cfTrashID, trashCF, scoreCtx, cfScoreOverrides)
			} else if ccf, ok := customCFMap[cfTrashID]; ok {
				cfName = ccf.Name
				desiredScore = resolveScoreCustom(cfTrashID, ccf, scoreCtx, cfScoreOverrides)
			} else {
				continue
			}

			arrID, hasArrID := cfNameToArrID[cfName]
			if !hasArrID {
				plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
					CFName:   cfName,
					ArrCFID:  0,
					OldScore: 0,
					NewScore: desiredScore,
					Action:   "set",
				})
				plan.Summary.ScoresToSet++
				continue
			}

			currentScore := currentScores[arrID]
			if currentScore == desiredScore {
				plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
					CFName:   cfName,
					ArrCFID:  arrID,
					OldScore: currentScore,
					NewScore: desiredScore,
					Action:   "unchanged",
				})
				plan.Summary.ScoresUnchanged++
			} else if behavior.RemoveMode == "allow_custom" && currentScore != 0 {
				// User has a custom score and we're told to allow it — don't overwrite
				plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
					CFName:   cfName,
					ArrCFID:  arrID,
					OldScore: currentScore,
					NewScore: currentScore, // keep user's score
					Action:   "unchanged",
				})
				plan.Summary.ScoresUnchanged++
			} else {
				action := "update"
				if currentScore == 0 {
					action = "set"
				}
				plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
					CFName:   cfName,
					ArrCFID:  arrID,
					OldScore: currentScore,
					NewScore: desiredScore,
					Action:   action,
				})
				plan.Summary.ScoresToSet++
			}
		}

		// Reset mode: controls what happens to scores for CFs not in the sync set
		if behavior.ResetMode == "reset_to_zero" {
			syncedArrIDs := make(map[int]bool)
			for cfTrashID := range allCFTrashIDs {
				var cfName string
				if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
					cfName = trashCF.Name
				} else if ccf, ok := customCFMap[cfTrashID]; ok {
					cfName = ccf.Name
				}
				if cfName != "" {
					if arrID, ok := cfNameToArrID[cfName]; ok {
						syncedArrIDs[arrID] = true
					}
				}
			}
			for _, fi := range targetProfile.FormatItems {
				if fi.Score != 0 && !syncedArrIDs[fi.Format] {
					plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
						CFName:   fi.Name,
						ArrCFID:  fi.Format,
						OldScore: fi.Score,
						NewScore: 0,
						Action:   "zero",
					})
					plan.Summary.ScoresToZero++
				}
			}
		}
		// else "do_not_adjust": leave unsynced CF scores as-is

		// Compare quality items: check if TRaSH profile's allowed qualities differ from Arr
		if len(profile.Items) > 0 {
			trashAllowed := make(map[string]bool)
			for _, item := range profile.Items {
				if item.Allowed {
					trashAllowed[item.Name] = true
				}
			}
			arrAllowed := make(map[string]bool)
			for _, item := range targetProfile.Items {
				if item.Allowed {
					name := item.Name
					if name == "" && item.Quality != nil {
						name = item.Quality.Name
					}
					if name != "" {
						arrAllowed[name] = true
					}
				}
			}
			// Check for differences
			for name := range trashAllowed {
				if !arrAllowed[name] {
					plan.Summary.QualityChanged = true
					break
				}
			}
			if !plan.Summary.QualityChanged {
				for name := range arrAllowed {
					if !trashAllowed[name] {
						plan.Summary.QualityChanged = true
						break
					}
				}
			}
		}
	} else {
		// Create mode: all scores are "set" actions
		plan.CreateProfile = true
		plan.NewProfileName = req.ProfileName
		if plan.NewProfileName == "" {
			plan.NewProfileName = profile.Name
		}

		// Check that profile name doesn't already exist
		existingProfiles, err := client.ListProfiles()
		if err != nil {
			return nil, fmt.Errorf("list profiles: %w", err)
		}
		for _, ep := range existingProfiles {
			if strings.EqualFold(ep.Name, plan.NewProfileName) {
				return nil, fmt.Errorf("profile %q already exists (ID %d) — use Update mode or choose a different name", ep.Name, ep.ID)
			}
		}

		for cfTrashID := range allCFTrashIDs {
			var cfName string
			var desiredScore int

			if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
				cfName = trashCF.Name
				desiredScore = resolveScore(cfTrashID, trashCF, scoreCtx, cfScoreOverrides)
			} else if ccf, ok := customCFMap[cfTrashID]; ok {
				cfName = ccf.Name
				desiredScore = resolveScoreCustom(cfTrashID, ccf, scoreCtx, cfScoreOverrides)
			} else {
				continue
			}
			plan.ScoreActions = append(plan.ScoreActions, ScoreAction{
				CFName:   cfName,
				OldScore: 0,
				NewScore: desiredScore,
				Action:   "set",
			})
			plan.Summary.ScoresToSet++
		}
	}

	return plan, nil
}

// --- Execute Sync ---

// SyncResult reports what was actually applied.
type SyncResult struct {
	CFsCreated      int      `json:"cfsCreated"`
	CFsUpdated      int      `json:"cfsUpdated"`
	ScoresUpdated   int      `json:"scoresUpdated"`
	ScoresZeroed    int      `json:"scoresZeroed"`
	QualityUpdated  bool     `json:"qualityUpdated,omitempty"`
	QualityDetails  []string `json:"qualityDetails,omitempty"`  // e.g. "Remux-1080p: Enabled → Disabled"
	CFDetails       []string `json:"cfDetails,omitempty"`       // e.g. "Created: HDR", "Updated: Hulu"
	ScoreDetails    []string `json:"scoreDetails,omitempty"`    // e.g. "BHDStudio: 1000 → 2240"
	ProfileCreated  bool     `json:"profileCreated,omitempty"`
	ArrProfileID    int      `json:"arrProfileId,omitempty"`
	ArrProfileName  string   `json:"arrProfileName,omitempty"`
	Errors          []string `json:"errors"`
}

// ExecuteSyncPlan applies a previously built sync plan.
func ExecuteSyncPlan(ad *AppData, instance Instance, req SyncRequest, plan *SyncPlan, imported *ImportedProfile, customCFs []CustomCF, behavior SyncBehavior) (*SyncResult, error) {
	if ad == nil {
		return nil, fmt.Errorf("no TRaSH data for %s", instance.Type)
	}

	var profile *TrashQualityProfile
	var scoreCtx string
	var cfScoreOverrides map[string]int

	customCFMap := buildCustomCFMap(customCFs)

	if imported != nil {
		fi := make(map[string]string, len(imported.FormatItems))
		for tid := range imported.FormatItems {
			if cf, ok := ad.CustomFormats[tid]; ok {
				fi[cf.Name] = tid
			} else if ccf, ok := customCFMap[tid]; ok {
				fi[ccf.Name] = tid
			}
		}
		profile = &TrashQualityProfile{
			Name:                  imported.Name,
			UpgradeAllowed:        imported.UpgradeAllowed,
			Cutoff:                imported.Cutoff,
			MinFormatScore:        imported.MinFormatScore,
			CutoffFormatScore:     imported.CutoffScore,
			MinUpgradeFormatScore: imported.MinUpgradeFormatScore,
			Language:              imported.Language,
			Items:                 imported.Qualities,
			FormatItems:           fi,
		}
		scoreCtx = imported.ScoreSet
		cfScoreOverrides = imported.FormatItems
	} else {
		profile = findProfile(ad, req.ProfileTrashID)
		if profile == nil {
			return nil, fmt.Errorf("TRaSH profile not found")
		}
		scoreCtx = profile.TrashScoreSet
	}

	if scoreCtx == "" {
		scoreCtx = "default"
	}

	// Merge per-CF score overrides from request
	if len(req.ScoreOverrides) > 0 {
		if cfScoreOverrides == nil {
			cfScoreOverrides = make(map[string]int)
		}
		for k, v := range req.ScoreOverrides {
			cfScoreOverrides[k] = v
		}
	}

	client := NewArrClient(instance.URL, instance.APIKey)
	result := &SyncResult{Errors: []string{}}

	// Track created CF name → Arr ID for score assignment
	createdCFIDs := make(map[string]int)

	// Apply CF creates/updates
	for _, action := range plan.CFActions {
		var arrCF *ArrCF
		if trashCF, ok := ad.CustomFormats[action.TrashID]; ok {
			arrCF = trashCFToArr(trashCF)
		} else if ccf, ok := customCFMap[action.TrashID]; ok {
			arrCF = customCFToArr(ccf)
		} else {
			continue
		}

		switch action.Action {
		case "create":
			created, err := client.CreateCustomFormat(arrCF)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("create %s: %v", action.Name, err))
				continue
			}
			createdCFIDs[action.Name] = created.ID
			result.CFsCreated++
			result.CFDetails = append(result.CFDetails, "Created: "+action.Name)
			time.Sleep(100 * time.Millisecond)

		case "update":
			_, err := client.UpdateCustomFormat(action.ArrID, arrCF)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("update %s: %v", action.Name, err))
				continue
			}
			result.CFsUpdated++
			result.CFDetails = append(result.CFDetails, "Updated: "+action.Name)
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Rebuild CF name → Arr ID with newly created CFs
	existingCFs, err := client.ListCustomFormats()
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("re-fetch CFs: %v", err))
		return result, nil
	}
	nameToID := make(map[string]int)
	for _, cf := range existingCFs {
		nameToID[strings.ToLower(cf.Name)] = cf.ID
	}

	if req.ArrProfileID == 0 {
		// Create mode: verify profile name is still unique
		existingProfiles, err := client.ListProfiles()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("verify profile name: %v", err))
			return result, nil
		}
		profileName := req.ProfileName
		if profileName == "" {
			profileName = profile.Name
		}
		for _, ep := range existingProfiles {
			if strings.EqualFold(ep.Name, profileName) {
				result.Errors = append(result.Errors, fmt.Sprintf("profile %q already exists (ID %d) — created since dry-run", ep.Name, ep.ID))
				return result, nil
			}
		}

		// Build and create new quality profile
		qualityDefs, err := client.ListQualityDefinitions()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fetch quality defs: %v", err))
			return result, nil
		}

		var languages []ArrLanguage
		if instance.Type == "radarr" {
			languages, err = client.ListLanguages()
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("fetch languages: %v", err))
				return result, nil
			}
		}

		selectedCFsMap := make(map[string]bool)
		for _, id := range req.SelectedCFs {
			selectedCFsMap[id] = true
		}

		arrProfile, err := BuildArrProfile(profile, ad, qualityDefs, languages, nameToID, selectedCFsMap, req.ProfileName, cfScoreOverrides, customCFs)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("build profile: %v", err))
			return result, nil
		}

		// Apply user overrides to created profile
		if req.Overrides != nil {
			log.Printf("Sync: applying overrides to new profile — %s", overrideSummary(req.Overrides))
			if req.Overrides.UpgradeAllowed != nil {
				arrProfile.UpgradeAllowed = *req.Overrides.UpgradeAllowed
			}
			if req.Overrides.MinFormatScore != nil {
				arrProfile.MinFormatScore = *req.Overrides.MinFormatScore
			}
			if req.Overrides.MinUpgradeFormatScore != nil {
				v := *req.Overrides.MinUpgradeFormatScore
				if v < 1 {
					v = 1
				}
				arrProfile.MinUpgradeFormatScore = v
			}
			if req.Overrides.CutoffFormatScore != nil {
				arrProfile.CutoffFormatScore = *req.Overrides.CutoffFormatScore
			}
			if req.Overrides.Language != nil {
				if langs, err := client.ListLanguages(); err == nil {
					for i := range langs {
						if strings.EqualFold(langs[i].Name, *req.Overrides.Language) {
							arrProfile.Language = &langs[i]
							break
						}
					}
				}
			}
			if req.Overrides.CutoffQuality != nil && *req.Overrides.CutoffQuality != "__skip__" {
				if cid, err := resolveCutoff(*req.Overrides.CutoffQuality, arrProfile.Items); err == nil {
					arrProfile.Cutoff = cid
				}
			}
		}

		// Apply quality overrides to new profile
		if len(req.QualityOverrides) > 0 {
			for i := range arrProfile.Items {
				name := arrProfile.Items[i].Name
				if name == "" && arrProfile.Items[i].Quality != nil {
					name = arrProfile.Items[i].Quality.Name
				}
				if override, ok := req.QualityOverrides[name]; ok {
					arrProfile.Items[i].Allowed = override
				}
			}
			// Re-resolve cutoff — disabled quality can't be cutoff
			for _, item := range arrProfile.Items {
				if item.Allowed {
					name := item.Name
					if name == "" && item.Quality != nil {
						name = item.Quality.Name
					}
					if cid, err := resolveCutoff(name, arrProfile.Items); err == nil {
						arrProfile.Cutoff = cid
					}
					break
				}
			}
		}

		created, err := client.CreateProfile(arrProfile)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("create profile: %v", err))
			return result, nil
		}

		result.ProfileCreated = true
		result.ArrProfileID = created.ID
		result.ArrProfileName = created.Name
		// Count only non-zero scores as actual updates
		for _, fi := range arrProfile.FormatItems {
			if fi.Score != 0 {
				result.ScoresUpdated++
			}
		}
	} else {
		// Update mode: apply score changes to existing profile
		arrProfiles, err := client.ListProfiles()
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("fetch profiles: %v", err))
			return result, nil
		}

		var targetProfile *ArrQualityProfile
		for i := range arrProfiles {
			if arrProfiles[i].ID == req.ArrProfileID {
				targetProfile = &arrProfiles[i]
				break
			}
		}
		if targetProfile == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("target profile no longer exists in Arr (internal ID %d) — it may have been deleted or recreated", req.ArrProfileID))
			return result, nil
		}

		// Snapshot current profile-level settings for change detection
		prevMinFormatScore := targetProfile.MinFormatScore
		prevMinUpgradeFormatScore := targetProfile.MinUpgradeFormatScore
		prevCutoffFormatScore := targetProfile.CutoffFormatScore
		prevUpgradeAllowed := targetProfile.UpgradeAllowed
		var prevLanguageID int
		if targetProfile.Language != nil {
			prevLanguageID = targetProfile.Language.ID
		}

		// Update profile-level settings (cutoff, min scores, upgrade)
		targetProfile.UpgradeAllowed = profile.UpgradeAllowed
		targetProfile.MinFormatScore = profile.MinFormatScore
		targetProfile.CutoffFormatScore = profile.CutoffFormatScore
		if profile.MinUpgradeFormatScore >= 1 {
			targetProfile.MinUpgradeFormatScore = profile.MinUpgradeFormatScore
		} else if targetProfile.MinUpgradeFormatScore < 1 {
			targetProfile.MinUpgradeFormatScore = 1 // Arr requires minimum 1
		}
		// Apply user overrides (if any)
		if req.Overrides != nil {
			log.Printf("Sync: applying overrides — %s", overrideSummary(req.Overrides))
			if req.Overrides.UpgradeAllowed != nil {
				targetProfile.UpgradeAllowed = *req.Overrides.UpgradeAllowed
			}
			if req.Overrides.MinFormatScore != nil {
				targetProfile.MinFormatScore = *req.Overrides.MinFormatScore
			}
			if req.Overrides.MinUpgradeFormatScore != nil {
				v := *req.Overrides.MinUpgradeFormatScore
				if v < 1 {
					v = 1
				}
				targetProfile.MinUpgradeFormatScore = v
			}
			if req.Overrides.CutoffFormatScore != nil {
				targetProfile.CutoffFormatScore = *req.Overrides.CutoffFormatScore
			}
			if req.Overrides.Language != nil {
				// Resolve language name → Arr language object
				if langs, err := client.ListLanguages(); err == nil {
					for i := range langs {
						if strings.EqualFold(langs[i].Name, *req.Overrides.Language) {
							targetProfile.Language = &langs[i]
							break
						}
					}
				}
			}
		}
		// Detect profile-level setting changes
		var curLanguageID int
		if targetProfile.Language != nil {
			curLanguageID = targetProfile.Language.ID
		}
		profileSettingsChanged := targetProfile.MinFormatScore != prevMinFormatScore ||
			targetProfile.MinUpgradeFormatScore != prevMinUpgradeFormatScore ||
			targetProfile.CutoffFormatScore != prevCutoffFormatScore ||
			targetProfile.UpgradeAllowed != prevUpgradeAllowed ||
			curLanguageID != prevLanguageID

		// Determine cutoff name (resolved to ID later, after quality items rebuild)
		// CutoffQuality "__skip__" means don't touch cutoff at all
		cutoffName := profile.Cutoff
		skipCutoff := false
		if req.Overrides != nil && req.Overrides.CutoffQuality != nil {
			if *req.Overrides.CutoffQuality == "__skip__" {
				skipCutoff = true
			} else {
				cutoffName = *req.Overrides.CutoffQuality
			}
		}

		// Merge core formatItems + selectedCFs for score assignment
		allTrashIDs := make(map[string]bool)
		for _, cfTrashID := range profile.FormatItems {
			allTrashIDs[cfTrashID] = true
		}
		for _, cfTrashID := range req.SelectedCFs {
			allTrashIDs[cfTrashID] = true
		}

		// Build current score map for allow_custom check
		currentScores := make(map[int]int)
		for _, fi := range targetProfile.FormatItems {
			currentScores[fi.Format] = fi.Score
		}

		// Update scores in the profile
		scoreMap := make(map[int]int) // Arr CF ID → desired score
		for cfTrashID := range allTrashIDs {
			var cfName string
			var score int
			if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
				cfName = trashCF.Name
				score = resolveScore(cfTrashID, trashCF, scoreCtx, cfScoreOverrides)
			} else if ccf, ok := customCFMap[cfTrashID]; ok {
				cfName = ccf.Name
				score = resolveScoreCustom(cfTrashID, ccf, scoreCtx, cfScoreOverrides)
			} else {
				continue
			}
			arrID, ok := nameToID[strings.ToLower(cfName)]
			if !ok {
				continue
			}
			// allow_custom: preserve non-zero user scores
			if behavior.RemoveMode == "allow_custom" {
				if cur, exists := currentScores[arrID]; exists && cur != 0 && cur != score {
					continue // user has custom score, skip
				}
			}
			scoreMap[arrID] = score
		}

		// Merge into existing formatItems
		updated := false
		for i := range targetProfile.FormatItems {
			fi := &targetProfile.FormatItems[i]
			if newScore, ok := scoreMap[fi.Format]; ok {
				if fi.Score != newScore {
					result.ScoreDetails = append(result.ScoreDetails, fmt.Sprintf("%s: %d → %d", fi.Name, fi.Score, newScore))
					fi.Score = newScore
					updated = true
					result.ScoresUpdated++
				}
				delete(scoreMap, fi.Format)
			}
		}
		// Add new format items for CFs not yet in the profile
		for cfID, score := range scoreMap {
			name := ""
			for _, cf := range existingCFs {
				if cf.ID == cfID {
					name = cf.Name
					break
				}
			}
			targetProfile.FormatItems = append(targetProfile.FormatItems, ArrProfileFormatItem{
				Format: cfID,
				Name:   name,
				Score:  score,
			})
			updated = true
			result.ScoresUpdated++
		}

		// Reset mode: zero out extra CFs not in the sync set
		if behavior.ResetMode == "reset_to_zero" {
			syncedArrIDs := make(map[int]bool)
			for cfTrashID := range allTrashIDs {
				var cfName string
				if trashCF, ok := ad.CustomFormats[cfTrashID]; ok {
					cfName = trashCF.Name
				} else if ccf, ok := customCFMap[cfTrashID]; ok {
					cfName = ccf.Name
				}
				if cfName != "" {
					if arrID, ok := nameToID[strings.ToLower(cfName)]; ok {
						syncedArrIDs[arrID] = true
					}
				}
			}
			for i := range targetProfile.FormatItems {
				fi := &targetProfile.FormatItems[i]
				if fi.Score != 0 && !syncedArrIDs[fi.Format] {
					fi.Score = 0
					updated = true
					result.ScoresZeroed++
				}
			}
		}

		// Update quality items from TRaSH profile
		if len(profile.Items) > 0 {
			qualityDefs, err := client.ListQualityDefinitions()
			if err != nil {
				log.Printf("Sync: failed to fetch quality defs: %v", err)
				result.Errors = append(result.Errors, fmt.Sprintf("fetch quality defs: %v", err))
			} else {
				qualityByName := make(map[string]*ArrQualityDefinition)
				for i := range qualityDefs {
					qualityByName[qualityDefs[i].Quality.Name] = &qualityDefs[i]
				}
				// Build old allowed map for comparison
				oldAllowed := make(map[string]bool)
				for _, item := range targetProfile.Items {
					name := item.Name
					if name == "" && item.Quality != nil { name = item.Quality.Name }
					if name != "" { oldAllowed[name] = item.Allowed }
				}

				newItems, err := resolveQualityItems(profile.Items, qualityByName)
				if err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("resolve quality items: %v", err))
				} else {
					// Track quality changes
					for _, item := range newItems {
						name := item.Name
						if name == "" && item.Quality != nil { name = item.Quality.Name }
						if name == "" { continue }
						oldState, exists := oldAllowed[name]
						if exists && oldState != item.Allowed {
							if item.Allowed {
								result.QualityDetails = append(result.QualityDetails, name+": Disabled → Enabled")
							} else {
								result.QualityDetails = append(result.QualityDetails, name+": Enabled → Disabled")
							}
						}
					}
					usedQualities := make(map[int]bool)
					collectUsedQualities(newItems, usedQualities)
					unused := make([]ArrQualityItem, 0)
					for _, def := range qualityDefs {
						if !usedQualities[def.Quality.ID] {
							unused = append(unused, ArrQualityItem{
								Quality: &ArrQualityRef{ID: def.Quality.ID, Name: def.Quality.Name},
								Allowed: false,
							})
						}
					}
					for i, j := 0, len(newItems)-1; i < j; i, j = i+1, j-1 {
						newItems[i], newItems[j] = newItems[j], newItems[i]
					}
					targetProfile.Items = append(unused, newItems...)
					if len(result.QualityDetails) > 0 {
						result.QualityUpdated = true
						updated = true
					}
					log.Printf("Sync: updated quality items: %d items (%d from TRaSH, %d unused)",
						len(targetProfile.Items), len(newItems), len(unused))
				}
			}
		}

		// Apply quality overrides (user-toggled resolutions)
		if len(req.QualityOverrides) > 0 {
			for i := range targetProfile.Items {
				name := targetProfile.Items[i].Name
				if name == "" && targetProfile.Items[i].Quality != nil {
					name = targetProfile.Items[i].Quality.Name
				}
				if override, ok := req.QualityOverrides[name]; ok {
					if targetProfile.Items[i].Allowed != override {
						targetProfile.Items[i].Allowed = override
						if override {
							result.QualityDetails = append(result.QualityDetails, name+": Disabled → Enabled (override)")
						} else {
							result.QualityDetails = append(result.QualityDetails, name+": Enabled → Disabled (override)")
						}
						result.QualityUpdated = true
						updated = true
					}
				}
			}
		}

		// Resolve cutoff after quality items rebuild so the ID matches the final item set
		if !skipCutoff {
			if cutoffName == "" {
				for _, item := range targetProfile.Items {
					if item.Allowed {
						if item.Name != "" {
							cutoffName = item.Name
						} else if item.Quality != nil {
							cutoffName = item.Quality.Name
						}
						break
					}
				}
			}
			if cutoffName != "" {
				if cid, err := resolveCutoff(cutoffName, targetProfile.Items); err == nil {
					if cid != targetProfile.Cutoff {
						log.Printf("Sync: cutoff resolved %q → %d (was %d)", cutoffName, cid, targetProfile.Cutoff)
						targetProfile.Cutoff = cid
						updated = true
					}
				} else {
					log.Printf("Warning: could not resolve cutoff %q: %v", cutoffName, err)
				}
			}
		}

		if profileSettingsChanged {
			updated = true
			log.Printf("Sync: profile settings changed (minScore=%d→%d, minUpgrade=%d→%d, cutoffScore=%d→%d, upgrade=%v→%v)",
				prevMinFormatScore, targetProfile.MinFormatScore,
				prevMinUpgradeFormatScore, targetProfile.MinUpgradeFormatScore,
				prevCutoffFormatScore, targetProfile.CutoffFormatScore,
				prevUpgradeAllowed, targetProfile.UpgradeAllowed)
		}

		if updated {
			log.Printf("Sync: sending profile update to %s (quality=%v, items=%d, formatItems=%d)",
				instance.Name, result.QualityUpdated, len(targetProfile.Items), len(targetProfile.FormatItems))
			if err := client.UpdateProfile(targetProfile); err != nil {
				log.Printf("Sync: profile update failed: %v", err)
				result.Errors = append(result.Errors, fmt.Sprintf("update profile scores: %v", err))
			} else {
				log.Printf("Sync: profile update successful")
			}
		} else {
			log.Printf("Sync: no profile changes to send")
		}
	}

	return result, nil
}

// --- Profile Builder ---

// BuildArrProfile converts a TRaSH quality profile into a full Arr quality profile
// ready for creation via the API.
func BuildArrProfile(
	profile *TrashQualityProfile,
	ad *AppData,
	qualityDefs []ArrQualityDefinition,
	languages []ArrLanguage,
	cfNameToID map[string]int,
	selectedCFs map[string]bool,
	profileName string,
	cfScoreOverrides map[string]int,
	customCFs []CustomCF,
) (*ArrQualityProfile, error) {
	// Build quality name → definition map
	qualityByName := make(map[string]*ArrQualityDefinition)
	for i := range qualityDefs {
		qualityByName[qualityDefs[i].Quality.Name] = &qualityDefs[i]
	}

	// Resolve quality items from TRaSH profile
	items, err := resolveQualityItems(profile.Items, qualityByName)
	if err != nil {
		return nil, fmt.Errorf("resolve quality items: %w", err)
	}

	// Add unused qualities as allowed: false (Arr requires complete list)
	// Arr API: items[0] = lowest priority (bottom of UI), items[last] = highest (top of UI)
	// So: unused qualities first, then profile items in reverse order (highest priority last)
	usedQualities := make(map[int]bool)
	collectUsedQualities(items, usedQualities)
	unused := make([]ArrQualityItem, 0)
	for _, def := range qualityDefs {
		if !usedQualities[def.Quality.ID] {
			unused = append(unused, ArrQualityItem{
				Quality: &ArrQualityRef{
					ID:         def.Quality.ID,
					Name:       def.Quality.Name,
					Source:     def.Quality.Source,
					Resolution: def.Quality.Resolution,
					Modifier:   def.Quality.Modifier,
				},
				Items:   []ArrQualityItem{},
				Allowed: false,
			})
		}
	}
	// Reverse profile items so first-in-YAML (highest priority) ends up last in array (top of UI)
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	items = append(unused, items...)

	// Resolve cutoff name → numeric ID
	// If cutoff is empty, default to the first allowed quality group/item
	cutoffName := profile.Cutoff
	if cutoffName == "" {
		for _, item := range items {
			if item.Allowed {
				if item.Name != "" {
					cutoffName = item.Name
				} else if item.Quality != nil {
					cutoffName = item.Quality.Name
				}
				break
			}
		}
	}
	cutoffID, err := resolveCutoff(cutoffName, items)
	if err != nil {
		return nil, fmt.Errorf("resolve cutoff: %w", err)
	}

	// Build FormatItems from CFs + scores
	scoreCtx := profile.TrashScoreSet
	if scoreCtx == "" {
		scoreCtx = "default"
	}

	allTrashIDs := make(map[string]bool)
	for _, trashID := range profile.FormatItems {
		allTrashIDs[trashID] = true
	}
	for trashID := range selectedCFs {
		allTrashIDs[trashID] = true
	}

	customCFMap := buildCustomCFMap(customCFs)

	formatItems := make([]ArrProfileFormatItem, 0)
	for trashID := range allTrashIDs {
		var cfName string
		var score int

		if cf, ok := ad.CustomFormats[trashID]; ok {
			cfName = cf.Name
			score = resolveScore(trashID, cf, scoreCtx, cfScoreOverrides)
		} else if ccf, ok := customCFMap[trashID]; ok {
			cfName = ccf.Name
			score = resolveScoreCustom(trashID, ccf, scoreCtx, cfScoreOverrides)
		} else {
			continue
		}

		arrID, ok := cfNameToID[strings.ToLower(cfName)]
		if !ok {
			continue
		}
		formatItems = append(formatItems, ArrProfileFormatItem{
			Format: arrID,
			Name:   cfName,
			Score:  score,
		})
	}

	// Radarr requires ALL CFs present in FormatItems — add missing ones with score 0
	includedCFs := make(map[int]bool)
	for _, fi := range formatItems {
		includedCFs[fi.Format] = true
	}
	for cfName, cfID := range cfNameToID {
		if !includedCFs[cfID] {
			formatItems = append(formatItems, ArrProfileFormatItem{
				Format: cfID,
				Name:   cfName,
				Score:  0,
			})
		}
	}

	// Resolve language (Radarr only)
	var lang *ArrLanguage
	if languages != nil {
		langByName := make(map[string]*ArrLanguage)
		for i := range languages {
			langByName[strings.ToLower(languages[i].Name)] = &languages[i]
		}
		langName := profile.Language
		if langName == "" {
			langName = "Original"
		}
		if l, ok := langByName[strings.ToLower(langName)]; ok {
			lang = l
		} else {
			log.Printf("Warning: language %q not found, falling back to Original", langName)
			if l, ok := langByName["original"]; ok {
				lang = l
			} else if l, ok := langByName["any"]; ok {
				lang = l
			}
		}
	}

	name := profileName
	if name == "" {
		name = profile.Name
	}

	minUpgrade := profile.MinUpgradeFormatScore
	if minUpgrade < 1 {
		minUpgrade = 1 // Arr requires minimum 1
	}

	return &ArrQualityProfile{
		Name:                  name,
		UpgradeAllowed:        profile.UpgradeAllowed,
		Cutoff:                cutoffID,
		MinFormatScore:        profile.MinFormatScore,
		CutoffFormatScore:     profile.CutoffFormatScore,
		MinUpgradeFormatScore: minUpgrade,
		FormatItems:           formatItems,
		Items:                 items,
		Language:              lang,
	}, nil
}

// resolveQualityItems converts TRaSH quality items to Arr format using quality definitions.
func resolveQualityItems(trashItems []QualityItem, qualityByName map[string]*ArrQualityDefinition) ([]ArrQualityItem, error) {
	items := make([]ArrQualityItem, 0, len(trashItems))
	groupID := 1000

	for _, ti := range trashItems {
		if len(ti.Items) == 0 {
			// Single quality
			def, ok := qualityByName[ti.Name]
			if !ok {
				return nil, fmt.Errorf("quality %q not found in definitions", ti.Name)
			}
			items = append(items, ArrQualityItem{
				Quality: &ArrQualityRef{
					ID:         def.Quality.ID,
					Name:       def.Quality.Name,
					Source:     def.Quality.Source,
					Resolution: def.Quality.Resolution,
					Modifier:   def.Quality.Modifier,
				},
				Items:   []ArrQualityItem{},
				Allowed: ti.Allowed,
			})
		} else {
			// Quality group
			nested := make([]ArrQualityItem, 0, len(ti.Items))
			for _, subName := range ti.Items {
				def, ok := qualityByName[subName]
				if !ok {
					return nil, fmt.Errorf("quality %q not found in definitions (group %q)", subName, ti.Name)
				}
				nested = append(nested, ArrQualityItem{
					Quality: &ArrQualityRef{
						ID:         def.Quality.ID,
						Name:       def.Quality.Name,
						Source:     def.Quality.Source,
						Resolution: def.Quality.Resolution,
						Modifier:   def.Quality.Modifier,
					},
					Items:   []ArrQualityItem{},
					Allowed: true,
				})
			}
			items = append(items, ArrQualityItem{
				ID:      groupID,
				Name:    ti.Name,
				Items:   nested,
				Allowed: ti.Allowed,
			})
			groupID++
		}
	}

	return items, nil
}

// collectUsedQualities recursively collects all quality IDs from resolved items.
func collectUsedQualities(items []ArrQualityItem, used map[int]bool) {
	for _, item := range items {
		if item.Quality != nil {
			used[item.Quality.ID] = true
		}
		collectUsedQualities(item.Items, used)
	}
}

// resolveCutoff finds the numeric ID for a cutoff name within resolved quality items.
// For groups: returns the group ID. For singles: returns the quality ID.
func resolveCutoff(cutoffName string, items []ArrQualityItem) (int, error) {
	for _, item := range items {
		if item.Name == cutoffName && item.ID > 0 {
			return item.ID, nil
		}
		if item.Quality != nil && item.Quality.Name == cutoffName {
			return item.Quality.ID, nil
		}
	}
	return 0, fmt.Errorf("cutoff %q not found in resolved items", cutoffName)
}

// --- Helpers ---

// cfSpecsMatch compares a TRaSH CF against an existing Arr CF.
// Returns true if they're functionally equivalent.
func cfSpecsMatch(trash *TrashCF, arr *ArrCF) bool {
	if trash.Name != arr.Name {
		return false
	}
	if trash.IncludeInRename != arr.IncludeCustomFormatWhenRenaming {
		return false
	}
	if len(trash.Specifications) != len(arr.Specifications) {
		return false
	}

	// Build map of arr specs by name for matching
	arrSpecs := make(map[string]*ArrSpecification)
	for i := range arr.Specifications {
		arrSpecs[arr.Specifications[i].Name] = &arr.Specifications[i]
	}

	for _, trashSpec := range trash.Specifications {
		arrSpec, ok := arrSpecs[trashSpec.Name]
		if !ok {
			return false
		}
		if trashSpec.Implementation != arrSpec.Implementation {
			return false
		}
		if trashSpec.Negate != arrSpec.Negate {
			return false
		}
		if trashSpec.Required != arrSpec.Required {
			return false
		}

		// Compare field values
		trashVal := extractFieldValue(trashSpec.Fields)
		arrVal := extractFieldValue(arrSpec.Fields)
		if !valuesEqual(trashVal, arrVal) {
			return false
		}
	}

	return true
}

// valuesEqual compares two values extracted from JSON (handles type differences).
func valuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Normalize to JSON and compare bytes
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func findProfile(ad *AppData, trashID string) *TrashQualityProfile {
	for _, p := range ad.Profiles {
		if p.TrashID == trashID {
			return p
		}
	}
	return nil
}

// --- Custom CF helpers for sync ---

// customCFToArr converts a CustomCF (specs already in Arr format) to an ArrCF for create/update.
func customCFToArr(cf *CustomCF) *ArrCF {
	// Convert specification fields to Arr format (TRaSH {"value":X} → Arr [{"name":"value","value":X}])
	specs := make([]ArrSpecification, len(cf.Specifications))
	for i, spec := range cf.Specifications {
		specs[i] = ArrSpecification{
			Name:           spec.Name,
			Implementation: spec.Implementation,
			Negate:         spec.Negate,
			Required:       spec.Required,
			Fields:         convertFieldsToArr(spec.Fields),
		}
	}
	return &ArrCF{
		Name:                            cf.Name,
		IncludeCustomFormatWhenRenaming: cf.IncludeInRename,
		Specifications:                  specs,
	}
}

// arrCFSpecsMatch compares two ArrCFs for functional equivalence (for custom CF comparison).
func arrCFSpecsMatch(a, b *ArrCF) bool {
	if a.Name != b.Name {
		return false
	}
	if a.IncludeCustomFormatWhenRenaming != b.IncludeCustomFormatWhenRenaming {
		return false
	}
	if len(a.Specifications) != len(b.Specifications) {
		return false
	}

	bSpecs := make(map[string]*ArrSpecification)
	for i := range b.Specifications {
		bSpecs[b.Specifications[i].Name] = &b.Specifications[i]
	}

	for _, aSpec := range a.Specifications {
		bSpec, ok := bSpecs[aSpec.Name]
		if !ok {
			return false
		}
		if aSpec.Implementation != bSpec.Implementation {
			return false
		}
		if aSpec.Negate != bSpec.Negate {
			return false
		}
		if aSpec.Required != bSpec.Required {
			return false
		}
		aVal := extractFieldValue(aSpec.Fields)
		bVal := extractFieldValue(bSpec.Fields)
		if !valuesEqual(aVal, bVal) {
			return false
		}
	}
	return true
}

// buildCustomCFMap builds a lookup map from custom CF ID to CustomCF.
func buildCustomCFMap(customCFs []CustomCF) map[string]*CustomCF {
	m := make(map[string]*CustomCF, len(customCFs))
	for i := range customCFs {
		m[customCFs[i].ID] = &customCFs[i]
	}
	return m
}

// resolveScoreCustom returns the desired score for a custom CF.
// Checks cfScoreOverrides first, then the CF's own TrashScores (dev mode).
func resolveScoreCustom(cfID string, cf *CustomCF, scoreCtx string, cfScoreOverrides map[string]int) int {
	if cfScoreOverrides != nil {
		if s, ok := cfScoreOverrides[cfID]; ok {
			return s
		}
	}
	if cf.TrashScores != nil {
		if s, ok := cf.TrashScores[scoreCtx]; ok {
			return s
		}
		if s, ok := cf.TrashScores["default"]; ok {
			return s
		}
	}
	return 0
}
