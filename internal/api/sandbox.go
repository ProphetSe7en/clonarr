package api

import (
	"encoding/json"
	"net/http"

	"clonarr/internal/core"
)

// handleSandboxGet returns the persisted scoring-sandbox state for the
// requested app type. An empty / never-saved file maps to a 200 with
// empty arrays so the frontend's migration path can distinguish "server
// is fresh, push localStorage now" from "server is unreachable".
func (s *Server) handleSandboxGet(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("appType")
	state, err := s.Core.Sandbox.GetState(appType)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to read sandbox state: "+err.Error())
		return
	}
	// Marshal manually so empty Titles / ScoreSets render as [] not null —
	// the frontend treats null as "no data, attempt migration", which would
	// loop on every load if the server returned null for genuinely-empty
	// state.
	if state.Titles == nil {
		state.Titles = []string{}
	}
	if state.ScoreSets == nil {
		state.ScoreSets = []json.RawMessage{}
	}
	// Defensive: legacy Results field has already been converted to
	// Titles by GetState. Clear it so a hand-edited file that still has
	// `results` can never leak it back through the API.
	state.Results = nil
	writeJSON(w, state)
}

// handleSandboxPut writes the full sandbox state for the given app type.
// Whole-document replace (not patch) keeps the protocol simple and matches
// how the frontend already manages this object: it holds the full sandbox
// model in memory, mutates locally, debounces, and uploads.
//
// Body is capped at 32MB. At ~2KB per parsed result and ~100 bytes per
// score-set entry, the cap holds well over 10,000 parsed releases — enough for
// the largest paste the chunked parser will accumulate — without letting a
// runaway payload exhaust memory. The cap is a maximum, not a reservation:
// small sandboxes still upload only a few KB.
func (s *Server) handleSandboxPut(w http.ResponseWriter, r *http.Request) {
	appType := r.PathValue("appType")
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	var state core.SandboxState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid sandbox state: "+err.Error())
		return
	}
	if err := s.Core.Sandbox.SaveState(appType, state); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save sandbox state: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
