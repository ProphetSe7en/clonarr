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

// cfNameCollision describes which existing CF a candidate name collides
// with — either a TRaSH-published CF or another custom CF in the same
// app type. Carries enough info to produce a clear error message and
// link to the colliding entry in the UI.
type cfNameCollision struct {
	Type     string // "trash" or "custom"
	Name     string
	TrashID  string // populated when Type == "trash"
	CustomID string // populated when Type == "custom"
}

// checkCustomCFNameCollision rejects names that would create a
// same-name conflict at sync time. Two CFs with the byte-identical name
// in the same app would both resolve to the same Radarr/Sonarr format
// when the engine looks them up by name, producing flip-flopping
// scores depending on Go map iteration order.
//
// We refuse the conflict at the source instead of trying to dedup at
// sync time: simpler, no silent winner-picks, and matches Radarr/Sonarr's
// own "Must be unique" rule for CF names. excludeID lets the update
// path rename a CF to its own current name (a no-op).
//
// Matching is case-sensitive — "PCOK" and "Pcok" are distinct, same as
// in Arr — and scoped per app type, so "Foo" can exist as a Radarr
// custom and a Sonarr custom simultaneously (different on-disk dirs,
// different Arr instances).
func (s *Server) checkCustomCFNameCollision(name, appType, excludeID string) *cfNameCollision {
	if ad := s.Core.Trash.GetAppData(appType); ad != nil {
		for tid, tcf := range ad.CustomFormats {
			if tcf != nil && tcf.Name == name {
				return &cfNameCollision{Type: "trash", Name: name, TrashID: tid}
			}
		}
	}
	for _, ccf := range s.Core.CustomCFs.List(appType) {
		if ccf.ID == excludeID {
			continue
		}
		if ccf.Name == name {
			return &cfNameCollision{Type: "custom", Name: name, CustomID: ccf.ID}
		}
	}
	return nil
}

// writeCollisionError translates a cfNameCollision into a 409 JSON
// response with a per-type message + machine-readable code so the UI
// can surface the conflict next to the offending field.
func writeCollisionError(w http.ResponseWriter, col *cfNameCollision, appType string) {
	body := map[string]any{
		"name": col.Name,
	}
	switch col.Type {
	case "trash":
		body["error"] = fmt.Sprintf("Name %q is already used by a TRaSH-published CF for %s. Pick a different name — sharing a name with a TRaSH CF causes flip-flopping scores at sync time.", col.Name, appType)
		body["code"] = "name_collision_trash"
		body["trashId"] = col.TrashID
	default: // "custom"
		body["error"] = fmt.Sprintf("Another custom CF named %q already exists for %s. Pick a different name.", col.Name, appType)
		body["code"] = "name_collision_existing"
		body["existingId"] = col.CustomID
	}
	writeJSONStatus(w, http.StatusConflict, body)
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

	// Validate and assign IDs
	now := time.Now().UTC().Format(time.RFC3339)
	// Track names from this request to catch in-batch duplicates (e.g.
	// importing a JSON with two entries of the same name).
	seenInBatch := make(map[string]bool)
	for i := range req.CFs {
		req.CFs[i].Name = strings.TrimSpace(req.CFs[i].Name)
		if req.CFs[i].Name == "" {
			writeError(w, 400, "CF name is required")
			return
		}
		if req.CFs[i].AppType != "radarr" && req.CFs[i].AppType != "sonarr" {
			writeError(w, 400, "Invalid app type for CF: "+req.CFs[i].Name)
			return
		}
		// Reject if name already exists in clonarr storage (case-sensitive,
		// per app type). Collision with TRaSH names by intent is allowed —
		// user picks the name knowing it will overwrite TRaSH's version on
		// sync, or picks a different name to coexist.
		batchKey := req.CFs[i].AppType + "|" + req.CFs[i].Name
		if seenInBatch[batchKey] {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error": fmt.Sprintf("Two custom CFs in this batch share the name %q for %s — names must be unique.", req.CFs[i].Name, req.CFs[i].AppType),
				"code":  "name_collision_batch",
				"name":  req.CFs[i].Name,
			})
			return
		}
		seenInBatch[batchKey] = true
		if col := s.checkCustomCFNameCollision(req.CFs[i].Name, req.CFs[i].AppType, ""); col != nil {
			writeCollisionError(w, col, req.CFs[i].AppType)
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
		writeError(w, 500, "Failed to save custom CFs: "+err.Error())
		return
	}
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

	if err := s.Core.CustomCFs.Delete(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

func (s *Server) handleUpdateCustomCF(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
	id := r.PathValue("id")
	if !strings.HasPrefix(id, "custom:") {
		id = "custom:" + id
	}

	var cf core.CustomCF
	if err := json.NewDecoder(r.Body).Decode(&cf); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	cf.ID = id

	// Trim before validation so whitespace-only names hit the empty-name
	// guard cleanly instead of slipping through the collision check.
	cf.Name = strings.TrimSpace(cf.Name)
	if cf.Name == "" {
		writeError(w, 400, "CF name is required")
		return
	}
	if cf.AppType != "radarr" && cf.AppType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	// Reject rename to a name already used by another custom CF or any
	// TRaSH-published CF in the same app type (case-sensitive). Excludes
	// self so renaming "PCOK" to "PCOK" passes through. TRaSH-name
	// rejection prevents the score-flip-flop class of bug — see helper
	// docstring for details.
	if col := s.checkCustomCFNameCollision(cf.Name, cf.AppType, cf.ID); col != nil {
		writeCollisionError(w, col, cf.AppType)
		return
	}
	if cf.Category == "" {
		cf.Category = "Custom"
	}

	if err := s.Core.CustomCFs.Update(cf); err != nil {
		writeError(w, 404, err.Error())
		return
	}
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

	// Fetch all CFs from instance
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	arrCFs, err := client.ListCustomFormats()
	if err != nil {
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
	var skippedTrashCollisions []string
	for _, acf := range arrCFs {
		if len(wantedNames) > 0 && !wantedNames[acf.Name] {
			continue
		}
		// Skip CFs whose name collides with an existing custom or any
		// TRaSH-published CF (case-sensitive, per app type). Surfaced in
		// the response so the user knows which were skipped and why —
		// custom-collision usually means re-importing the same CF;
		// trash-collision means the user's Arr CF shares a name with
		// TRaSH's, which would cause flip-flopping at sync time.
		if col := s.checkCustomCFNameCollision(acf.Name, req.AppType, ""); col != nil {
			if col.Type == "trash" {
				skippedTrashCollisions = append(skippedTrashCollisions, acf.Name)
			} else {
				skippedCollisions = append(skippedCollisions, acf.Name)
			}
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
		totalSkipped := len(skippedCollisions) + len(skippedTrashCollisions)
		if totalSkipped > 0 {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":                  fmt.Sprintf("All %d requested CFs have names that collide with existing CFs (%d with TRaSH, %d with customs). Rename the source CFs in your Arr instance, or pick different CFs to import.", totalSkipped, len(skippedTrashCollisions), len(skippedCollisions)),
				"code":                   "name_collision_all_skipped",
				"skippedCollisions":      skippedCollisions,
				"skippedTrashCollisions": skippedTrashCollisions,
			})
			return
		}
		writeError(w, 400, "No matching CFs found in instance")
		return
	}

	added, err := s.Core.CustomCFs.Add(toImport)
	if err != nil {
		writeError(w, 500, "Failed to save imported CFs: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"added":                  added,
		"total":                  len(toImport),
		"skipped":                len(toImport) - added,
		"skippedCollisions":      skippedCollisions,
		"skippedTrashCollisions": skippedTrashCollisions,
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
