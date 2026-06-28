package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Portable sync-rule import. Validates a clonarr export envelope, recreates the
// bundled custom CFs (with per-CF collision resolution), and returns the rule
// with its custom CF references remapped to the local custom CF ids so the
// editor can load it. See docs/clonarr/sync-rule-import-export-plan.md.

// ImportCollision describes a bundled custom CF whose name already exists in the
// local registry, so the user must choose how to resolve it.
type ImportCollision struct {
	Name        string   `json:"name"`
	UsedByRules []string `json:"usedByRules,omitempty"` // existing rule descriptions using the local CF (Replace impact)
}

// ValidateSyncRuleImport parses + validates a portable export. It rejects any
// file that is not a clonarr sync-rule export (wrong/missing marker, unknown
// schema, or app mismatch) so users cannot feed in Recyclarr / TRaSH / random
// JSON. expectedApp is the app the user is importing into ("radarr"/"sonarr").
func ValidateSyncRuleImport(data []byte, expectedApp string) (*SyncRuleExport, error) {
	var exp SyncRuleExport
	if err := json.Unmarshal(data, &exp); err != nil {
		return nil, fmt.Errorf("this file is not valid JSON")
	}
	if exp.ClonarrExport != SyncRuleExportMarker {
		return nil, fmt.Errorf("this is not a Clonarr sync-rule export (try a file exported from Clonarr's Sync Rules)")
	}
	if exp.SchemaVersion <= 0 || exp.SchemaVersion > SyncRuleExportSchema {
		return nil, fmt.Errorf("this export was made by a newer Clonarr (schema v%d); update Clonarr to import it", exp.SchemaVersion)
	}
	if exp.App != "radarr" && exp.App != "sonarr" {
		return nil, fmt.Errorf("export is missing a valid app type")
	}
	if expectedApp != "" && exp.App != expectedApp {
		return nil, fmt.Errorf("this is a %s rule; switch to %s to import it", exp.App, exp.App)
	}
	return &exp, nil
}

// FindImportCollisions returns the bundled custom CFs whose names already exist
// in the local registry for the export's app. ruleDescriber maps a local custom
// CF id to the list of rule descriptions that reference it (for the Replace
// warning); pass nil to skip that.
func FindImportCollisions(exp *SyncRuleExport, existing []CustomCF, usedBy func(localID string) []string) []ImportCollision {
	byName := make(map[string]CustomCF, len(existing))
	for _, cf := range existing {
		byName[strings.ToLower(cf.Name)] = cf
	}
	var out []ImportCollision
	for _, cf := range exp.CustomFormats {
		if local, ok := byName[strings.ToLower(cf.Name)]; ok {
			c := ImportCollision{Name: cf.Name}
			if usedBy != nil {
				c.UsedByRules = usedBy(local.ID)
			}
			out = append(out, c)
		}
	}
	return out
}

// ApplyImport recreates the export's custom CFs into the store using per-CF
// resolutions (keyed by lower-cased CF name: "skip" | "rename" | "replace";
// absent = create when no collision). It returns an id remap (export custom id
// -> local custom id) so the caller can rewrite the rule's references. The
// store mutations (add/delete) are performed via the provided callbacks so this
// stays free of API/transaction concerns.
func ApplyImport(
	exp *SyncRuleExport,
	existing []CustomCF,
	resolutions map[string]string,
	addCF func(CustomCF) (string, error), // returns the stored id
	updateCF func(CustomCF) error, // replaces an existing CF in place, keeping its id
) (idRemap map[string]string, err error) {
	byName := make(map[string]CustomCF, len(existing))
	for _, cf := range existing {
		byName[strings.ToLower(cf.Name)] = cf
	}
	idRemap = make(map[string]string)
	for _, cf := range exp.CustomFormats {
		srcID := cf.ID
		nameKey := strings.ToLower(cf.Name)
		local, collides := byName[nameKey]
		if !collides {
			// Fresh CF: create with a NEW local id (avoid any cross-machine id
			// collision); the remap rewrites the rule's references.
			fresh := cf
			fresh.ID = ""
			newID, addErr := addCF(fresh)
			if addErr != nil {
				return nil, addErr
			}
			idRemap[srcID] = newID
			continue
		}
		switch resolutions[nameKey] {
		case "replace":
			// Overwrite the existing definition in place, keeping its id (so
			// other rules using it pick up the new definition). An in-place
			// Update is atomic - no delete-then-add window where a crash or a
			// failed add could leave the in-use CF permanently gone.
			repl := cf
			repl.ID = local.ID
			if updErr := updateCF(repl); updErr != nil {
				return nil, updErr
			}
			idRemap[srcID] = local.ID
		case "rename":
			renamed := cf
			renamed.ID = "" // force a new id
			renamed.Name = uniqueCFName(cf.Name, byName)
			newID, addErr := addCF(renamed)
			if addErr != nil {
				return nil, addErr
			}
			byName[strings.ToLower(renamed.Name)] = CustomCF{ID: newID, Name: renamed.Name}
			idRemap[srcID] = newID
		default: // "skip" (or unspecified): reuse the existing local CF.
			idRemap[srcID] = local.ID
		}
	}
	return idRemap, nil
}

// RemapRuleCustomRefs rewrites a rule's custom CF references (SelectedCFs and
// ScoreOverrides keys) from exported ids to local ids per idRemap. References
// not in the remap (TRaSH CFs, or custom CFs that failed to import) are left
// as-is.
func RemapRuleCustomRefs(rule *ExportedRule, idRemap map[string]string) {
	if rule == nil || len(idRemap) == 0 {
		return
	}
	for i, id := range rule.SelectedCFs {
		if to, ok := idRemap[id]; ok {
			rule.SelectedCFs[i] = to
		}
	}
	if rule.ScoreOverrides != nil {
		remapped := make(map[string]int, len(rule.ScoreOverrides))
		for id, score := range rule.ScoreOverrides {
			if to, ok := idRemap[id]; ok {
				remapped[to] = score
			} else {
				remapped[id] = score
			}
		}
		rule.ScoreOverrides = remapped
	}
}

// uniqueCFName returns name with an "(imported)" suffix, bumping a counter until
// it does not collide with any existing name (case-insensitive).
func uniqueCFName(name string, taken map[string]CustomCF) string {
	candidate := name + " (imported)"
	n := 2
	for {
		if _, ok := taken[strings.ToLower(candidate)]; !ok {
			return candidate
		}
		candidate = fmt.Sprintf("%s (imported %d)", name, n)
		n++
	}
}
