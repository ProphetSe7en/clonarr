package api

import (
	"net/http"

	"clonarr/internal/core"
	"clonarr/internal/utils"
)

// handleLocalSourceSeed copies the current guide clone into the local source
// folder, so the user starts with a complete, identical set to edit. When Local
// mode is active it reloads afterwards so the seeded data is parsed immediately.
func (s *Server) handleLocalSourceSeed(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.Trash.SeedLocalSource(); err != nil {
		writeError(w, 400, "Seed failed: "+err.Error())
		return
	}
	if s.Core.Trash.LocalSourceEnabled() {
		utils.SafeGo("local-source-seed-reload", func() {
			_ = s.Core.RunPullOnly(core.SourceManualPull)
		})
	}
	writeJSON(w, map[string]string{"status": "seeded"})
}

// handleLocalSourceClean empties the local source folder. The in-memory snapshot
// is left as-is until the next reload/seed; the UI then shows the empty state.
func (s *Server) handleLocalSourceClean(w http.ResponseWriter, r *http.Request) {
	if err := s.Core.Trash.CleanLocalSource(); err != nil {
		writeError(w, 400, "Clean failed: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "cleaned"})
}
