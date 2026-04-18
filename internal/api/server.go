package api

import (
	"clonarr/internal/core"
	"net/http"
)

// Server wraps the core application and provides HTTP handlers.
type Server struct {
	Core *core.App
}

// NewServer creates a new API server instance and registers all routes.
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
