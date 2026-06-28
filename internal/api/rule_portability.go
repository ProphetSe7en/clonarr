package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// errImportCFNotStored is returned when a bundled custom format could not be
// persisted (e.g. an unexpected same-name duplicate), so the caller aborts
// rather than building a rule that references a non-existent CF.
var errImportCFNotStored = errors.New("a bundled custom format could not be saved (possible duplicate name)")

// Portable sync-rule import/export endpoints. See
// docs/clonarr/sync-rule-import-export-plan.md. Kept in its own file (not
// autosync.go) so the feature stays self-contained and easy to extend.

// handleExportRule returns a self-contained, clonarr-native export of one sync
// rule: the portable rule fields, the base profile reference, and the full
// definitions of any non-TRaSH custom CFs the rule uses. Re-imports 1:1.
//
//	GET /api/auto-sync/rules/{id}/export
func (s *Server) handleExportRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, 400, "Missing rule ID")
		return
	}

	cfg := s.Core.Config.Get()
	var rule *core.AutoSyncRule
	for i := range cfg.AutoSync.Rules {
		if cfg.AutoSync.Rules[i].ID == id {
			rule = &cfg.AutoSync.Rules[i]
			break
		}
	}
	if rule == nil {
		writeError(w, 404, "Sync rule not found")
		return
	}

	inst, ok := s.Core.Config.GetInstance(rule.InstanceID)
	if !ok {
		writeError(w, 404, "Instance for this rule not found")
		return
	}

	// Resolve the base profile's display name from the TRaSH snapshot (for
	// display + import-time validation). Empty for imported-profile rules.
	profileName := ""
	if rule.TrashProfileID != "" {
		if ad := s.Core.Trash.GetAppData(inst.Type); ad != nil {
			for _, p := range ad.Profiles {
				if p.TrashID == rule.TrashProfileID {
					profileName = p.Name
					break
				}
			}
		}
	}

	// Custom CF registry keyed by ID, so the builder can bundle definitions
	// for the custom CFs this rule references. No Arr instance needed.
	customByID := make(map[string]core.CustomCF)
	for _, cf := range s.Core.CustomCFs.List(inst.Type) {
		customByID[cf.ID] = cf
	}

	export := core.BuildSyncRuleExport(rule, inst.Type, profileName, s.Core.Version, customByID)
	writeJSON(w, export)
}

// handleImportRule validates a portable sync-rule export, recreates its bundled
// custom CFs (resolving name collisions per the user's choices), remaps the
// rule's custom CF references to the new local ids, and returns the resolved
// export for the frontend to load into the profile editor. The user then names
// it and runs Apply & Sync to create the rule. Custom CF creation happens here;
// the rule itself is created via the normal editor -> Apply & Sync flow.
//
//	POST /api/auto-sync/rules/import
//	body: { content: "<export json>", app: "radarr"|"sonarr", resolutions: {"<cf name lower>": "skip"|"rename"|"replace"} }
func (s *Server) handleImportRule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB cap on an import payload
	var req struct {
		Content     string            `json:"content"`
		App         string            `json:"app"`
		Resolutions map[string]string `json:"resolutions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request")
		return
	}

	exp, err := core.ValidateSyncRuleImport([]byte(req.Content), req.App)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	cfg := s.Core.Config.Get()

	// Pre-flight everything that must be true for the import to fully succeed
	// BEFORE mutating the custom CF registry. Creating/replacing CFs first and
	// only then discovering the base profile or instance is missing leaves a
	// half-import behind (orphaned or clobbered CFs, no rule). Re-importing then
	// trips fresh collisions on the just-created CFs.
	if exp.BaseProfile.TrashID == "" {
		writeError(w, 422, "This export has no base TRaSH-Guides profile and cannot be imported.")
		return
	}
	baseFound := false
	if ad := s.Core.Trash.GetAppData(exp.App); ad != nil {
		for _, p := range ad.Profiles {
			if p.TrashID == exp.BaseProfile.TrashID {
				baseFound = true
				break
			}
		}
	}
	if !baseFound {
		name := exp.BaseProfile.Name
		if name == "" {
			name = exp.BaseProfile.TrashID
		}
		writeError(w, 422, "The base profile this rule is built on ("+name+") is not available here. Pull TRaSH-Guides data for "+exp.App+", then import again.")
		return
	}
	hasInstance := false
	for _, inst := range cfg.Instances {
		if inst.Type == exp.App {
			hasInstance = true
			break
		}
	}
	if !hasInstance {
		writeError(w, 422, "Add a "+exp.App+" instance before importing this rule.")
		return
	}

	// Validate every bundled custom CF the same way the manual create path does
	// (these definitions come from an untrusted file): non-empty name, at least
	// one named condition. Reject up front so we never persist a malformed CF.
	for _, cf := range exp.CustomFormats {
		c := cf
		c.AppType = exp.App
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" {
			writeError(w, 400, "An imported custom format has no name.")
			return
		}
		if specErr := validateCFSpecifications(c); specErr != "" {
			writeError(w, 400, specErr)
			return
		}
	}

	existing := s.Core.CustomCFs.List(exp.App)

	// For the Replace warning: which existing rules reference a given local CF.
	usedBy := func(localID string) []string {
		var out []string
		for i := range cfg.AutoSync.Rules {
			ru := &cfg.AutoSync.Rules[i]
			used := false
			for _, id := range ru.SelectedCFs {
				if id == localID {
					used = true
					break
				}
			}
			if !used {
				if _, ok := ru.ScoreOverrides[localID]; ok {
					used = true
				}
			}
			if used {
				label := ru.TrashProfileID
				if inst, ok := s.Core.Config.GetInstance(ru.InstanceID); ok {
					label = inst.Name + " #" + strconv.Itoa(ru.ArrProfileID)
				}
				out = append(out, label)
			}
		}
		return out
	}

	collisions := core.FindImportCollisions(exp, existing, usedBy)
	var unresolved []core.ImportCollision
	for _, c := range collisions {
		if req.Resolutions[strings.ToLower(c.Name)] == "" {
			unresolved = append(unresolved, c)
		}
	}
	if len(unresolved) > 0 {
		writeJSONStatus(w, 409, map[string]any{"collisions": unresolved})
		return
	}

	// normalize fills the app type and a default category on a bundled CF before
	// it is persisted (mirrors the manual create path).
	normalize := func(cf core.CustomCF) core.CustomCF {
		cf.AppType = exp.App
		cf.Name = strings.TrimSpace(cf.Name)
		if strings.TrimSpace(cf.Category) == "" {
			cf.Category = "Custom"
		}
		return cf
	}
	addCF := func(cf core.CustomCF) (string, error) {
		cfs := []core.CustomCF{normalize(cf)}
		n, e := s.Core.CustomCFs.Add(cfs)
		if e != nil {
			return "", e
		}
		if n != 1 {
			// Add silently skips same-name duplicates; a 0 here would otherwise
			// leave a remap pointing at a CF that was never persisted.
			return "", errImportCFNotStored
		}
		return cfs[0].ID, nil
	}
	updateCF := func(cf core.CustomCF) error { return s.Core.CustomCFs.Update(normalize(cf)) }

	remap, err := core.ApplyImport(exp, existing, req.Resolutions, addCF, updateCF)
	if err != nil {
		writeError(w, 500, "Failed to import custom formats: "+err.Error())
		return
	}
	core.RemapRuleCustomRefs(&exp.Rule, remap)

	writeJSON(w, map[string]any{"export": exp, "baseProfileFound": true})
}
