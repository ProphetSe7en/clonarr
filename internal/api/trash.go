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
		op := s.Core.DebugLog.BeginOp(core.OpTrash, core.SourceManualPull, "url="+cfg.TrashRepo.URL+" branch="+cfg.TrashRepo.Branch)
		endResult := "error: unknown"
		defer func() { op.End(endResult) }()
		prevCommit := s.Core.Trash.CurrentCommit()
		if err := s.Core.Trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("TRaSH pull failed: %v", err)
			s.Core.DebugLog.Logf(core.LogError, "TRaSH pull failed: %v", err)
			s.Core.Trash.SetPullError(err.Error())
			endResult = "error: pull failed"
			return
		}
		newCommit := s.Core.Trash.CurrentCommit()
		commitChanged := prevCommit != "" && newCommit != prevCommit
		if commitChanged {
			s.Core.NotifyRepoUpdate(prevCommit, newCommit)
			// Surface what actually changed in the upstream repo so users can verify
			// pulls did what they expected (added CFs, removed orphans, etc.). One
			// summary line per app + up to 15 detail lines, then "...and N more".
			if diff, err := s.Core.Trash.DiffPull(prevCommit, newCommit); err != nil {
				s.Core.DebugLog.Logf(core.LogTrash, "Pull diff failed: %v (commit %s → %s)", err, shortCommit(prevCommit), shortCommit(newCommit))
			} else {
				s.Core.DebugLog.Logf(core.LogTrash, "Pull completed — commit %s → %s", shortCommit(prevCommit), shortCommit(newCommit))
				if len(diff.Changes) == 0 {
					s.Core.DebugLog.Logf(core.LogTrash, "No JSON file changes in this commit (only includes/cf-descriptions or other non-data files)")
				} else {
					for _, app := range []string{"radarr", "sonarr"} {
						if sum := diff.SummaryByApp(app); sum != "" {
							appLabel := "Radarr"
							if app == "sonarr" {
								appLabel = "Sonarr"
							}
							s.Core.DebugLog.Logf(core.LogTrash, "%s — %s", appLabel, sum)
						}
					}
					for _, line := range diff.DetailLines(15) {
						s.Core.DebugLog.Logf(core.LogTrash, "  %s", line)
					}
				}
			}
		} else {
			s.Core.DebugLog.Logf(core.LogTrash, "Pull completed — no upstream changes")
		}
		s.Core.DebugLog.Logf(core.LogAutoSync, "Running auto-sync")
		s.AutoSyncQualitySizes()
		// AutoSyncAfterPull opens its own AUTOSYNC operation; it is not a
		// child of this TRASH op so the trace clearly separates the pull
		// from the rules it triggers.
		s.Core.AutoSyncAfterPull(core.SourceManualPull)
		if commitChanged {
			endResult = "ok | new commit " + shortCommit(newCommit)
		} else {
			endResult = "ok | no change"
		}
	})
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]string{"status": "pulling"})
}

// shortCommit returns the first 7 characters of a git commit hash for
// inclusion in human-readable log messages. Returns the full string if
// it's already short.
func shortCommit(hash string) string {
	if len(hash) <= 7 {
		return hash
	}
	return hash[:7]
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
