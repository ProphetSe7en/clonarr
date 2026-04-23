package api

import (
	"clonarr/internal/core"

	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// --- Import ---

func (s *Server) handleImportProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB limit
	var req struct {
		YAML     string `json:"yaml"`
		Name     string `json:"name"`    // optional override name
		AppType  string `json:"appType"` // for TRaSH JSON detection context (from active tab)
		Includes []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"includes"` // optional include files for Recyclarr configs
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	content := strings.TrimSpace(req.YAML)
	if content == "" {
		writeError(w, 400, "content is required")
		return
	}

	// Resolve Recyclarr includes: merge include file contents into the main YAML
	if len(req.Includes) > 0 {
		includeMap := make(map[string]string)
		for _, inc := range req.Includes {
			includeMap[strings.ToLower(inc.Name)] = inc.Content
		}
		content = core.MergeRecyclarrIncludes(content, includeMap)
	}

	// Build TRaSH data map for group resolution + CF name lookup
	snap := s.Core.Trash.Snapshot()
	trashData := map[string]*core.AppData{
		"radarr": core.SnapshotAppData(snap, "radarr"),
		"sonarr": core.SnapshotAppData(snap, "sonarr"),
	}

	// Auto-detect format: try JSON first, fall back to YAML
	var profiles []core.ImportedProfile
	if strings.HasPrefix(content, "{") {
		appType := req.AppType
		ad := trashData[appType] // may be nil for "auto" — core.ParseProfileJSON handles it
		p, err := core.ParseProfileJSON([]byte(content), appType, ad, trashData)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		profiles = []core.ImportedProfile{*p}
	} else {
		var err error
		profiles, err = core.ParseRecyclarrYAML([]byte(content), trashData, req.AppType)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for i := range profiles {
		profiles[i].ImportedAt = now
		// Allow name override for single-profile imports
		if req.Name != "" && len(profiles) == 1 {
			profiles[i].Name = req.Name
		}
	}

	// Resolve CF names from TRaSH data
	for i := range profiles {
		ad := trashData[profiles[i].AppType]
		if ad == nil {
			continue
		}
		// Replace comment-based names with actual TRaSH CF names where possible
		for tid := range profiles[i].FormatItems {
			if cf, ok := ad.CustomFormats[tid]; ok {
				if profiles[i].FormatComments == nil {
					profiles[i].FormatComments = make(map[string]string)
				}
				profiles[i].FormatComments[tid] = cf.Name
			}
		}
	}

	added, skipped, err := s.Core.Profiles.Add(profiles)
	if err != nil {
		writeError(w, 500, "failed to save: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{
		"imported": added,
		"skipped":  skipped,
		"profiles": profiles,
	})
}

func (s *Server) handleGetImportedProfiles(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	profiles := s.Core.Profiles.List(appType)
	if profiles == nil {
		profiles = []core.ImportedProfile{}
	}
	writeJSON(w, profiles)
}

func (s *Server) handleImportedProfileDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profile, ok := s.Core.Profiles.Get(id)
	if !ok {
		writeError(w, 404, "profile not found")
		return
	}

	snap := s.Core.Trash.Snapshot()
	ad := core.SnapshotAppData(snap, profile.AppType)

	// If profile has a trashProfileId and TRaSH data is available, use the standard path
	if profile.TrashProfileID != "" && ad != nil {
		detailData := core.ProfileDetailData(ad, profile.TrashProfileID)
		if detailData != nil {
			writeJSON(w, map[string]any{
				"profile": map[string]any{
					"name":                  profile.Name,
					"upgradeAllowed":        profile.UpgradeAllowed,
					"cutoff":                profile.Cutoff,
					"minFormatScore":        profile.MinFormatScore,
					"cutoffFormatScore":     profile.CutoffScore,
					"minUpgradeFormatScore": profile.MinUpgradeFormatScore,
					"language":              profile.Language,
					"scoreSet":              profile.ScoreSet,
					"items":                 profile.Qualities,
				},
				"trashGroups":     detailData.Groups,
				"formatItemNames": detailData.FormatItemNames,
				"totalCoreCFs":    len(profile.FormatItems),
				"imported":        true,
				"importedRaw":     profile,
			})
			return
		}
	}

	// No trashProfileId or TRaSH lookup failed — build from imported data + TRaSH groups
	var detailData *core.ProfileDetailResult
	if ad != nil {
		detailData = core.ImportedProfileDetailData(ad, &profile)
	}

	// Build complete quality items list: imported items + all missing items from
	// TRaSH as disabled. This ensures the quality override editor shows all
	// available quality levels, not just the ones from the YAML.
	allItems := make([]core.QualityItem, len(profile.Qualities))
	copy(allItems, profile.Qualities)
	if ad != nil && len(ad.Profiles) > 0 {
		// Use first TRaSH profile's items as the complete list
		baseItems := ad.Profiles[0].Items
		importedNames := make(map[string]bool)
		for _, q := range profile.Qualities {
			importedNames[q.Name] = true
			// Also track sub-items for groups
			for _, sub := range q.Items {
				importedNames[sub] = true
			}
		}
		// Append missing items as disabled
		for _, ti := range baseItems {
			if !importedNames[ti.Name] {
				allItems = append(allItems, core.QualityItem{
					Name:    ti.Name,
					Allowed: false,
					Items:   ti.Items,
				})
			}
		}
	}

	resp := map[string]any{
		"profile": map[string]any{
			"name":                  profile.Name,
			"upgradeAllowed":        profile.UpgradeAllowed,
			"cutoff":                profile.Cutoff,
			"minFormatScore":        profile.MinFormatScore,
			"cutoffFormatScore":     profile.CutoffScore,
			"minUpgradeFormatScore": profile.MinUpgradeFormatScore,
			"language":              profile.Language,
			"scoreSet":              profile.ScoreSet,
			"items":                 allItems,
		},
		"totalCoreCFs": len(profile.FormatItems),
		"imported":     true,
		"importedRaw":  profile,
	}
	if detailData != nil {
		resp["trashGroups"] = detailData.Groups
		resp["formatItemNames"] = detailData.FormatItemNames
	}
	writeJSON(w, resp)
}

func (s *Server) handleUpdateImportedProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	profile, ok := s.Core.Profiles.Get(id)
	if !ok {
		writeError(w, 404, "profile not found")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		VariantGoldenRule string `json:"variantGoldenRule"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}

	if req.VariantGoldenRule != "" {
		v := strings.ToUpper(req.VariantGoldenRule)
		if v != "HD" && v != "UHD" {
			writeError(w, 400, "variantGoldenRule must be 'HD' or 'UHD'")
			return
		}
		profile.VariantGoldenRule = v
	}

	if err := s.Core.Profiles.Update(profile); err != nil {
		writeError(w, 500, "failed to update: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
}

func (s *Server) handleDeleteImportedProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Core.Profiles.Delete(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}
