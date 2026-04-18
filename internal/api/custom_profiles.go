package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"time"
)

// --- Custom Profiles ---

func (s *Server) handleQualityPresets(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []core.QualityPreset{})
		return
	}
	writeJSON(w, core.QualityPresets(ad))
}

func (s *Server) handleAllCFsCategorized(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, core.CFPickerData{})
		return
	}
	customCFs := s.Core.CustomCFs.List(appType)
	writeJSON(w, core.AllCFsCategorized(ad, customCFs))
}

func (s *Server) handleCreateCustomProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var p core.ImportedProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}
	if p.Name == "" {
		writeError(w, 400, "name is required")
		return
	}
	if p.AppType != "radarr" && p.AppType != "sonarr" {
		writeError(w, 400, "appType must be 'radarr' or 'sonarr'")
		return
	}

	p.ID = core.GenerateID()
	p.Source = "custom"
	p.ImportedAt = time.Now().UTC().Format(time.RFC3339)

	if _, _, err := s.Core.Profiles.Add([]core.ImportedProfile{p}); err != nil {
		writeError(w, 500, "Failed to save: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, p)
}

func (s *Server) handleUpdateCustomProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := s.Core.Profiles.Get(id)
	if !ok {
		writeError(w, 404, "Profile not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var p core.ImportedProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeError(w, 400, "Invalid JSON")
		return
	}

	p.ID = id
	if p.Source == "" {
		p.Source = existing.Source
	}
	if p.ImportedAt == "" {
		p.ImportedAt = existing.ImportedAt
	}

	if err := s.Core.Profiles.Update(p); err != nil {
		writeError(w, 500, "Failed to save: "+err.Error())
		return
	}
	writeJSON(w, p)
}
