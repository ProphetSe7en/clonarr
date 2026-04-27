package api

import (
	"clonarr/internal/auth"
	"clonarr/internal/core"
	"net/http"
	"sync"
)

// Server wraps the core application and provides HTTP handlers.
type Server struct {
	Core      *core.App
	AuthStore *auth.Store
	// updateConfigMu serializes handleUpdateConfig so the read-modify-write
	// of AuthStore.Config() → UpdateConfig() cannot lose updates when two
	// admins save concurrently. Core.Config.Update is already closure-under-
	// lock, but the auth live-reload block reads the auth store's config,
	// modifies a copy, and writes it back — classic lost-update window.
	updateConfigMu sync.Mutex
}

// NewServer creates a new API server instance.
func NewServer(app *core.App) *Server {
	s := &Server{
		Core: app,
	}
	return s
}

// RegisterRoutes registers all API routes on the given mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.registerRoutes(mux)
}
