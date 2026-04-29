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

// ensureCustomPrefix returns name with a leading "!" if it doesn't already
// have one. Custom CFs are stored with this prefix to make collision with
// TRaSH guides CFs structurally impossible — TRaSH never uses "!" in CF
// names (verified against the entire upstream catalog), so a "!"-prefixed
// custom is always distinguishable from anything TRaSH publishes.
//
// Trims leading whitespace before checking the prefix so " PCOK" still
// becomes "!PCOK" rather than " PCOK" (would skip prefix unintentionally).
func ensureCustomPrefix(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.HasPrefix(trimmed, "!") {
		return trimmed
	}
	return "!" + trimmed
}

// checkCustomCFNameTaken returns the existing CF if a custom CF with this exact
// name already exists for the given app type, excluding the CF whose ID matches
// excludeID (used for the update path so renaming a CF to its own name doesn't
// trip the check). Match is case-sensitive — "!PCOK" and "!pcok" are distinct.
//
// Layer 1 of the name-collision safeguards. Pairs with the "!" prefix
// enforcement above which makes TRaSH-vs-custom collisions impossible by
// design; this only catches duplicate-within-custom-storage cases.
func (s *Server) checkCustomCFNameTaken(name, appType, excludeID string) (*core.CustomCF, bool) {
	for _, ccf := range s.Core.CustomCFs.List(appType) {
		if ccf.ID == excludeID {
			continue
		}
		if ccf.Name == name {
			return &ccf, true
		}
	}
	return nil, false
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
		// Validate before prefix — TrimSpace("  ") + "!" would otherwise
		// produce a "!" name from whitespace-only input.
		if strings.TrimSpace(req.CFs[i].Name) == "" {
			writeError(w, 400, "CF name is required")
			return
		}
		if req.CFs[i].AppType != "radarr" && req.CFs[i].AppType != "sonarr" {
			writeError(w, 400, "Invalid app type for CF: "+req.CFs[i].Name)
			return
		}
		// Force "!" prefix on every custom CF so it cannot collide with a
		// TRaSH-published CF of the same canonical name.
		req.CFs[i].Name = ensureCustomPrefix(req.CFs[i].Name)
		// Layer 1: reject if name already exists (case-sensitive, per app type).
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
		if existing, taken := s.checkCustomCFNameTaken(req.CFs[i].Name, req.CFs[i].AppType, ""); taken {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":      fmt.Sprintf("A custom CF named %q already exists for %s. Pick a different name.", req.CFs[i].Name, req.CFs[i].AppType),
				"code":       "name_collision_existing",
				"name":       req.CFs[i].Name,
				"existingId": existing.ID,
			})
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

	// Validate before prefix — strings.TrimSpace("  ") + "!"-prefix would
	// otherwise produce a "!" name from whitespace-only input.
	if strings.TrimSpace(cf.Name) == "" {
		writeError(w, 400, "CF name is required")
		return
	}
	if cf.AppType != "radarr" && cf.AppType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	// Force "!" prefix on rename — keeps the structural collision-proofing
	// guarantee even when the user edits an existing CF's name.
	cf.Name = ensureCustomPrefix(cf.Name)
	// Layer 1: reject rename to a name already used by another custom CF in the
	// same app type. Excludes self so renaming "!PCOK" to "!PCOK" passes through.
	if existing, taken := s.checkCustomCFNameTaken(cf.Name, cf.AppType, cf.ID); taken {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error":      fmt.Sprintf("Another custom CF named %q already exists for %s. Pick a different name.", cf.Name, cf.AppType),
			"code":       "name_collision_existing",
			"name":       cf.Name,
			"existingId": existing.ID,
		})
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
	for _, acf := range arrCFs {
		if len(wantedNames) > 0 && !wantedNames[acf.Name] {
			continue
		}
		prefixedName := ensureCustomPrefix(acf.Name)
		// Layer 1: skip CFs whose prefixed name collides with an existing
		// custom CF. Surfaced in the response so the user knows which were
		// skipped (rare — usually means re-importing something already
		// imported).
		if _, taken := s.checkCustomCFNameTaken(prefixedName, req.AppType, ""); taken {
			skippedCollisions = append(skippedCollisions, prefixedName)
			continue
		}
		toImport = append(toImport, core.CustomCF{
			ID:             core.GenerateCustomID(),
			Name:           prefixedName,
			AppType:        req.AppType,
			Category:       category,
			ArrID:          acf.ID,
			Specifications: acf.Specifications,
			SourceInstance: inst.Name,
			ImportedAt:     now,
		})
	}

	if len(toImport) == 0 {
		if len(skippedCollisions) > 0 {
			writeJSONStatus(w, http.StatusConflict, map[string]any{
				"error":             fmt.Sprintf("All %d requested CFs have names that already exist as custom CFs. Rename or delete the existing ones first, or pick different CFs to import.", len(skippedCollisions)),
				"code":              "name_collision_all_skipped",
				"skippedCollisions": skippedCollisions,
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
