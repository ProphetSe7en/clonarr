package api

import (
	"clonarr/internal/core"
	"clonarr/internal/utils"
	"log"
	"net/http"
	"sort"
)

// --- TRaSH ---

func (s *Server) handleTrashStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Core.Trash.Status())
}

func (s *Server) handleTrashPull(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	utils.SafeGo("manual-trash-pull", func() {
		prevCommit := s.Core.Trash.CurrentCommit()
		if err := s.Core.Trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("TRaSH pull failed: %v", err)
			s.Core.DebugLog.Logf(core.LogError, "TRaSH pull failed: %v", err)
			s.Core.Trash.SetPullError(err.Error())
		} else {
			newCommit := s.Core.Trash.CurrentCommit()
			if prevCommit != "" && newCommit != prevCommit {
				s.Core.NotifyRepoUpdate(prevCommit, newCommit)
			}
			s.Core.DebugLog.Logf(core.LogAutoSync, "TRaSH pull completed — running auto-sync")
			s.AutoSyncQualitySizes()
			s.Core.AutoSyncAfterPull()
		}
	})
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "pulling"})
}

func (s *Server) handleTrashCFs(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	cfs := make([]*core.TrashCF, 0, len(ad.CustomFormats))
	for _, cf := range ad.CustomFormats {
		cfs = append(cfs, cf)
	}
	writeJSON(w, cfs)
}

// handleTrashScoreContexts returns the distinct trash_scores context keys
// actually used in TRaSH-Guides CFs for the given s.Core. Keeps the Custom Format
// editor's context dropdown in sync with upstream without hardcoding.
func (s *Server) handleTrashScoreContexts(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []string{"default"})
		return
	}

	seen := map[string]struct{}{"default": {}}
	for _, cf := range ad.CustomFormats {
		for k := range cf.TrashScores {
			seen[k] = struct{}{}
		}
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	// Stable ordering: "default" first, then alphabetical.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "default" {
			return true
		}
		if keys[j] == "default" {
			return false
		}
		return keys[i] < keys[j]
	})
	writeJSON(w, keys)
}

func (s *Server) handleTrashCFGroups(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	groups := ad.CFGroups
	if groups == nil {
		groups = []*core.TrashCFGroup{}
	}
	writeJSON(w, groups)
}

func (s *Server) handleTrashConflicts(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}
	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil || ad.Conflicts == nil {
		writeJSON(w, core.ConflictsData{CustomFormats: [][]core.ConflictEntry{}})
		return
	}
	writeJSON(w, ad.Conflicts)
}

func (s *Server) handleTrashProfiles(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("app")
	if appType != "radarr" && appType != "sonarr" {
		writeError(w, 400, "app must be 'radarr' or 'sonarr'")
		return
	}

	ad := s.Core.Trash.GetAppData(appType)
	if ad == nil {
		writeJSON(w, []any{})
		return
	}

	type ProfileListItem struct {
		TrashID          string `json:"trashId"`
		Name             string `json:"name"`
		TrashScoreSet    string `json:"trashScoreSet,omitempty"`
		TrashDescription string `json:"trashDescription,omitempty"`
		TrashURL         string `json:"trashUrl,omitempty"`
		Group            int    `json:"group"`
		GroupName        string `json:"groupName"`
		CFCount          int    `json:"cfCount"`
	}

	groupNames := make(map[string]string) // trash_id → group name
	for _, pg := range ad.ProfileGroups {
		for _, tid := range pg.Profiles {
			groupNames[tid] = pg.Name
		}
	}

	var items []ProfileListItem
	for _, p := range ad.Profiles {
		gn := groupNames[p.TrashID]
		if gn == "" {
			gn = "Other"
		}
		items = append(items, ProfileListItem{
			TrashID:          p.TrashID,
			Name:             p.Name,
			TrashScoreSet:    p.TrashScoreSet,
			TrashDescription: p.TrashDescription,
			TrashURL:         p.TrashURL,
			Group:            p.Group,
			GroupName:        gn,
			CFCount:          len(p.FormatItems),
		})
	}
	writeJSON(w, items)
}
