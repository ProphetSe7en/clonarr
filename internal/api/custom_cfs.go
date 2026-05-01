package api

import (
	"clonarr/internal/arr"
	"clonarr/internal/core"

	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// --- Custom CF Handlers ---

// checkCustomCFNameTaken rejects names that would duplicate another
// custom CF in the same app. Radarr/Sonarr enforce byte-exact name
// uniqueness within an app, so two customs with the same name can't
// coexist in Arr regardless of what clonarr does — this guard catches
// the conflict at create/update time so the user gets an immediate
// error instead of a confusing failure during sync.
//
// Sharing a name with a TRaSH-published CF is allowed. The user owns
// their naming choices. The cross-usage problem (a TRaSH CF and a
// custom CF with the same name both used in profiles syncing to the
// same Arr instance) is a separate concern handled at sync-plan time,
// not at create/update time — flagging it here would block legitimate
// names that the user never intends to combine with the TRaSH version.
//
// Matching is case-sensitive — "PCOK" and "Pcok" are distinct, same as
// in Arr — and scoped per app type, so "Foo" can exist as a Radarr
// custom and a Sonarr custom simultaneously. excludeID lets the update
// path rename a CF to its own current name (a no-op).
func (s *Server) checkCustomCFNameTaken(name, appType, excludeID string) *core.CustomCF {
	for _, ccf := range s.Core.CustomCFs.List(appType) {
		if ccf.ID == excludeID {
			continue
		}
		if ccf.Name == name {
			return &ccf
		}
	}
	return nil
}

// validateCFSpecifications mirrors Arr's own server-side checks so we
// fail fast before a sync attempt produces a confusing 400. The two
// validations Arr enforces and that we can't catch any other way:
//
//   - At least one specification on the CF (an empty CF can never
//     match anything in Arr; saving one surfaces as a "specifications
//     are required" 400 on the next sync).
//   - Every specification has a non-empty trimmed name. Whitespace-only
//     names slip past simple length checks but Arr rejects them with
//     "Condition name(s) cannot be empty or consist of only spaces".
//
// Returning a non-empty string signals validation failure; the caller
// uses the string as the 400 body. Empty return = passes.
func validateCFSpecifications(cf core.CustomCF) string {
	if len(cf.Specifications) == 0 {
		return fmt.Sprintf("Custom format %q has no conditions. Add at least one condition (release-title regex, source, resolution, etc.) before saving.", cf.Name)
	}
	for i, spec := range cf.Specifications {
		if strings.TrimSpace(spec.Name) == "" {
			return fmt.Sprintf("Condition #%d on custom format %q has no name. Every condition needs a name (e.g. \"Match WEB-DL\") before the CF can be saved.", i+1, cf.Name)
		}
	}
	return ""
}

// writeCustomCollisionError emits a 409 with a clear message pointing
// at the existing custom CF that owns the name. Machine-readable code
// `name_collision_existing` lets the UI surface the conflict next to
// the offending field.
func writeCustomCollisionError(w http.ResponseWriter, existing *core.CustomCF, appType string) {
	writeJSONStatus(w, http.StatusConflict, map[string]any{
		"error":      fmt.Sprintf("Another custom CF named %q already exists for %s. Sonarr/Radarr require unique CF names within an app — pick a different name.", existing.Name, appType),
		"code":       "name_collision_existing",
		"name":       existing.Name,
		"existingId": existing.ID,
	})
}

func (s *Server) handleListCustomCFs(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	cfs := s.Core.CustomCFs.List(appType)
	if cfs == nil {
		cfs = []core.CustomCF{}
	}
	writeJSON(w, cfs)
}

func (s *Server) handleCreateCustomCFs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var req struct {
		CFs []core.CustomCF `json:"cfs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	if len(req.CFs) == 0 {
		writeError(w, 400, "No custom formats provided")
		return
	}
	op := s.Core.DebugLog.BeginOp(core.OpCF, core.SourceManualCreate, fmt.Sprintf("count=%d appType=%s", len(req.CFs), req.CFs[0].AppType))
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	// Validate and assign IDs
	now := time.Now().UTC().Format(time.RFC3339)
	// Track names from this request to catch in-batch duplicates (e.g.
	// importing a JSON with two entries of the same name).
	seenInBatch := make(map[string]bool)
	for i := range req.CFs {
		req.CFs[i].Name = strings.TrimSpace(req.CFs[i].Name)
		if req.CFs[i].Name == "" {
			endResult = "error: validation (empty name)"
			writeError(w, 400, "CF name is required")
			return
		}
		if req.CFs[i].AppType != "radarr" && req.CFs[i].AppType != "sonarr" {
			endResult = "error: validation (invalid app type)"
			writeError(w, 400, "Invalid app type for CF: "+req.CFs[i].Name)
			return
		}
		if specErr := validateCFSpecifications(req.CFs[i]); specErr != "" {
			endResult = "error: validation (specification)"
			writeError(w, 400, specErr)
			return
		}
		// Reject if name already exists in another custom CF for this app
		// (case-sensitive). Sharing a name with a TRaSH-published CF is
		// allowed — the user owns their naming choices. The cross-usage
		// collision (TRaSH+custom with the same name in different profiles
		// syncing to the same Arr instance) is detected at sync-plan time.
		batchKey := req.CFs[i].AppType + "|" + req.CFs[i].Name
		if seenInBatch[batchKey] {
			endResult = "error: name_collision_batch"
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error": fmt.Sprintf("Two custom CFs in this batch share the name %q for %s — names must be unique.", req.CFs[i].Name, req.CFs[i].AppType),
				"code":  "name_collision_batch",
				"name":  req.CFs[i].Name,
			})
			return
		}
		seenInBatch[batchKey] = true
		if existing := s.checkCustomCFNameTaken(req.CFs[i].Name, req.CFs[i].AppType, ""); existing != nil {
			endResult = "error: name_collision_existing"
			writeCustomCollisionError(w, existing, req.CFs[i].AppType)
			return
		}
		if req.CFs[i].Category == "" {
			req.CFs[i].Category = "Custom"
		}
		// Always generate ID server-side — the ID is used as a filename,
		// so accepting client-supplied IDs would allow path traversal.
		req.CFs[i].ID = core.GenerateCustomID()
		if req.CFs[i].ImportedAt == "" {
			req.CFs[i].ImportedAt = now
		}
	}

	added, err := s.Core.CustomCFs.Add(req.CFs)
	if err != nil {
		endResult = fmt.Sprintf("error: storage failed: %v", err)
		writeError(w, 500, "Failed to save custom CFs: "+err.Error())
		return
	}
	endResult = fmt.Sprintf("ok | added %d CFs", added)
	writeJSON(w, map[string]any{"added": added, "total": len(req.CFs)})
}

func (s *Server) handleDeleteCustomCF(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// The ID contains "custom:" prefix which has a colon — reconstruct from path
	// PathValue("id") captures everything after /api/custom-cfs/
	if !strings.HasPrefix(id, "custom:") {
		// Try to find by raw id (the part after custom:)
		id = "custom:" + id
	}
	op := s.Core.DebugLog.BeginOp(core.OpCF, core.SourceManualDelete, "id="+id)
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	if err := s.Core.CustomCFs.Delete(id); err != nil {
		endResult = fmt.Sprintf("error: %v", err)
		writeError(w, 404, err.Error())
		return
	}
	endResult = "ok | deleted"
	writeJSON(w, map[string]string{"status": "deleted"})
}

func (s *Server) handleUpdateCustomCF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "custom:") {
		id = "custom:" + id
	}
	op := s.Core.DebugLog.BeginOp(core.OpCF, core.SourceManualEdit, "id="+id)
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	var cf core.CustomCF
	if err := json.NewDecoder(r.Body).Decode(&cf); err != nil {
		endResult = "error: invalid request body"
		writeError(w, 400, "Invalid request body")
		return
	}
	cf.ID = id

	// Trim before validation so whitespace-only names hit the empty-name
	// guard cleanly instead of slipping through the collision check.
	cf.Name = strings.TrimSpace(cf.Name)
	if cf.Name == "" {
		endResult = "error: validation (empty name)"
		writeError(w, 400, "CF name is required")
		return
	}
	if cf.AppType != "radarr" && cf.AppType != "sonarr" {
		endResult = "error: validation (invalid app type)"
		writeError(w, 400, "Invalid app type")
		return
	}
	if specErr := validateCFSpecifications(cf); specErr != "" {
		endResult = "error: validation (specification)"
		writeError(w, 400, specErr)
		return
	}
	// Reject rename only if another custom CF in the same app already
	// owns this name (case-sensitive). Excludes self so renaming
	// "PCOK" → "PCOK" passes through. Sharing a name with TRaSH is
	// allowed — see helper docstring.
	if existing := s.checkCustomCFNameTaken(cf.Name, cf.AppType, cf.ID); existing != nil {
		endResult = "error: name_collision_existing"
		writeCustomCollisionError(w, existing, cf.AppType)
		return
	}
	if cf.Category == "" {
		cf.Category = "Custom"
	}

	if err := s.Core.CustomCFs.Update(cf); err != nil {
		endResult = fmt.Sprintf("error: %v", err)
		writeError(w, 404, err.Error())
		return
	}
	endResult = fmt.Sprintf("ok | updated to %q", cf.Name)
	writeJSON(w, map[string]string{"status": "updated"})
}

func (s *Server) handleImportCFsFromInstance(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	var req struct {
		InstanceID string   `json:"instanceId"`
		CFNames    []string `json:"cfNames"`  // which CFs to import (by name)
		Category   string   `json:"category"` // target category
		AppType    string   `json:"appType"`  // "radarr" or "sonarr"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}

	if req.AppType != "radarr" && req.AppType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}

	inst, ok := s.Core.Config.GetInstance(req.InstanceID)
	if !ok {
		writeError(w, 404, "Instance not found")
		return
	}
	op := s.Core.DebugLog.BeginOp(core.OpCF, core.SourceManualImportInst, fmt.Sprintf("instance=%s appType=%s requested=%d", inst.Name, req.AppType, len(req.CFNames)))
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	// Fetch all CFs from instance
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
		endResult = fmt.Sprintf("error: fetch from %s failed: %v", inst.Name, err)
		writeError(w, 502, "Failed to fetch CFs from instance: "+err.Error())
		return
	}

	// Build lookup of requested names
	wantedNames := make(map[string]bool)
	for _, name := range req.CFNames {
		wantedNames[name] = true
	}

	// Filter and convert
	category := req.Category
	if category == "" {
		category = "Custom"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	var toImport []core.CustomCF
	var skippedCollisions []string
	for _, acf := range arrCFs {
		if len(wantedNames) > 0 && !wantedNames[acf.Name] {
			continue
		}
		// Skip CFs whose name duplicates an existing custom in clonarr
		// (case-sensitive, per app type). Sharing a name with a TRaSH
		// CF is allowed — the user controls naming. The cross-usage
		// collision (TRaSH+custom in different profiles syncing to the
		// same Arr instance) is detected at sync-plan time, not here.
		if existing := s.checkCustomCFNameTaken(acf.Name, req.AppType, ""); existing != nil {
			skippedCollisions = append(skippedCollisions, acf.Name)
			continue
		}
		toImport = append(toImport, core.CustomCF{
			ID:              core.GenerateCustomID(),
			Name:            acf.Name,
			AppType:         req.AppType,
			Category:        category,
			ArrID:           acf.ID,
			IncludeInRename: acf.IncludeCustomFormatWhenRenaming,
			Specifications:  acf.Specifications,
			SourceInstance:  inst.Name,
			ImportedAt:      now,
		})
	}

	if len(toImport) == 0 {
		if len(skippedCollisions) > 0 {
			endResult = fmt.Sprintf("error: name_collision_all_skipped (%d)", len(skippedCollisions))
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":             fmt.Sprintf("All %d requested CFs have names that match a custom CF you've already imported. Rename the source CFs in Arr or pick different CFs to import.", len(skippedCollisions)),
				"code":              "name_collision_all_skipped",
				"skippedCollisions": skippedCollisions,
			})
			return
		}
		endResult = "error: no matching CFs found in instance"
		writeError(w, 400, "No matching CFs found in instance")
		return
	}

	added, err := s.Core.CustomCFs.Add(toImport)
	if err != nil {
		endResult = fmt.Sprintf("error: storage failed: %v", err)
		writeError(w, 500, "Failed to save imported CFs: "+err.Error())
		return
	}
	endResult = fmt.Sprintf("ok | added %d, skipped %d", added, len(skippedCollisions))

	writeJSON(w, map[string]any{
		"added":             added,
		"total":             len(toImport),
		"skipped":           len(toImport) - added,
		"skippedCollisions": skippedCollisions,
	})
}

// --- CF Schema ---

// cfSchemaCache caches CF schema per app type to avoid repeated Arr API calls.
var cfSchemaCache sync.Map // appType → json.RawMessage

// handleCFSchema returns the CF specification schema (available implementations + field definitions).
// Proxied from the first connected instance of the requested app type, cached in memory.
func (s *Server) handleCFSchema(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	// Check cache first
	if cached, ok := cfSchemaCache.Load(appType); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cached.([]byte))
		return
	}

	// Find first instance of this type
	cfg := s.Core.Config.Get()
	var inst *core.Instance
	for i := range cfg.Instances {
		if cfg.Instances[i].Type == appType {
			inst = &cfg.Instances[i]
			break
		}
	}
	if inst == nil {
		writeError(w, 404, "No "+appType+" instance configured")
		return
	}

	// Fetch schema from Arr API
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	data, status, err := client.DoRequest("GET", "/customformat/schema", nil)
	if err != nil {
		writeError(w, 502, "Failed to fetch schema: "+err.Error())
		return
	}
	if status != 200 {
		writeError(w, 502, fmt.Sprintf("Arr returned HTTP %d", status))
		return
	}

	// Cache and return
	// NOTE: Cache is never explicitly invalidated because the CF schema (available implementations
	// and field definitions) comes from the Arr instance, not the TRaSH repo. It only changes
	// when the Arr software itself is updated, which is rare and a restart clears it.
	cfSchemaCache.Store(appType, data)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
