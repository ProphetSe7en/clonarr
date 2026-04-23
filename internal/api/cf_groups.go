package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// --- CF Group Handlers ---
//
// CF groups are user-created TRaSH-style custom-format bundles. The UI lets
// the user build them visually in the CF Group Builder and save them to
// Clonarr's local storage so they can be re-opened, edited, and downloaded
// again later without rebuilding from scratch.
//
// Storage layout matches CustomCF: /config/custom/json/{appType}/cf-groups/*.json
// so Radarr and Sonarr groups with the same name never collide on disk.

func (s *Server) handleListCFGroups(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	groups := s.Core.CFGroups.List(appType)
	if groups == nil {
		groups = []core.CFGroup{}
	}
	writeJSON(w, groups)
}

func (s *Server) handleCreateCFGroup(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB

	var g core.CFGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}

	// Force server-side values for fields that affect storage/identity. The
	// client is not trusted to choose its own filename (ID) or to overwrite
	// the URL-scoped appType.
	g.ID = core.GenerateCFGroupID()
	g.AppType = appType
	now := time.Now().UTC().Format(time.RFC3339)
	g.CreatedAt = now
	g.UpdatedAt = now

	if strings.TrimSpace(g.Name) == "" {
		writeError(w, 400, "Group name is required")
		return
	}
	if strings.TrimSpace(g.TrashID) == "" {
		writeError(w, 400, "trash_id is required (compute MD5 of the name)")
		return
	}
	if g.QualityProfiles.Include == nil {
		g.QualityProfiles.Include = map[string]string{}
	}
	if g.CustomFormats == nil {
		g.CustomFormats = []core.CFGroupCF{}
	}

	saved, err := s.Core.CFGroups.Add(g)
	if err != nil {
		writeError(w, 500, "Failed to save cf-group: "+err.Error())
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleUpdateCFGroup(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	id := r.PathValue("id")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB

	var g core.CFGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}

	// Load the existing record to get CreatedAt and to verify the ID exists
	// under this app type. Cross-app-type moves aren't supported — matches the
	// CustomCF contract.
	existing, ok := s.Core.CFGroups.Get(id)
	if !ok {
		writeError(w, 404, "cf-group not found")
		return
	}
	if existing.AppType != appType {
		writeError(w, 400, "cf-group belongs to "+existing.AppType+", not "+appType)
		return
	}

	g.ID = id
	g.AppType = appType
	g.CreatedAt = existing.CreatedAt
	g.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if strings.TrimSpace(g.Name) == "" {
		writeError(w, 400, "Group name is required")
		return
	}
	if strings.TrimSpace(g.TrashID) == "" {
		writeError(w, 400, "trash_id is required")
		return
	}
	if g.QualityProfiles.Include == nil {
		g.QualityProfiles.Include = map[string]string{}
	}
	if g.CustomFormats == nil {
		g.CustomFormats = []core.CFGroupCF{}
	}

	if err := s.Core.CFGroups.Update(g); err != nil {
		writeError(w, 500, "Failed to update cf-group: "+err.Error())
		return
	}
	writeJSON(w, g)
}

func (s *Server) handleDeleteCFGroup(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	id := r.PathValue("id")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "Invalid app type")
		return
	}

	// Verify the group belongs to the URL-scoped appType before deleting, so
	// `DELETE /api/cf-groups/radarr/<sonarr-id>` can't silently remove the
	// Sonarr group.
	existing, ok := s.Core.CFGroups.Get(id)
	if !ok {
		writeError(w, 404, "cf-group not found")
		return
	}
	if existing.AppType != appType {
		writeError(w, 400, "cf-group belongs to "+existing.AppType+", not "+appType)
		return
	}

	if err := s.Core.CFGroups.Delete(id); err != nil {
		writeError(w, 500, "Failed to delete cf-group: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}
