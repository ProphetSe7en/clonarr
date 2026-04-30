package api

import (
	"clonarr/internal/auth"
	"clonarr/internal/core"
	"html/template"
	"net/http"
	"sync"
)

// IndexHandler renders index.html as a Go template so BasePath can be injected
// at serve time. Registered at "GET /{$}" (exact root match) so Go 1.22+
// ServeMux prefers it over the catch-all FileServer for GET /.
type IndexHandler struct {
	Tmpl     *template.Template
	BasePath string
}

func (h *IndexHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = h.Tmpl.Execute(w, map[string]any{"BasePath": h.BasePath})
}

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
