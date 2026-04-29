// notifications.go implements the REST API endpoints for managing notification
// agents. These handlers provide full CRUD for the AutoSync.NotificationAgents
// slice and support both inline and saved-agent testing.
//
// Routes (registered in router.go):
//
//	GET    /api/notification-agents           → handleListNotificationAgents
//	POST   /api/notification-agents           → handleCreateNotificationAgent
//	PUT    /api/notification-agents/{id}      → handleUpdateNotificationAgent
//	DELETE /api/notification-agents/{id}      → handleDeleteNotificationAgent
//	POST   /api/notification-agents/test      → handleTestNotificationAgentInline
//	POST   /api/notification-agents/{id}/test → handleTestNotificationAgent

package api

import (
	"net/http"

	"clonarr/internal/core"
)

// maskAgentConfig returns a copy of NotificationConfig with credentials masked.
func maskAgentConfig(agentType string, nc core.NotificationConfig) core.NotificationConfig {
	return core.MaskNotificationAgentConfig(agentType, nc)
}

// preserveAgentConfig applies preserveIfMasked to each credential field,
// keeping the stored value when the UI returns the masked placeholder.
func preserveAgentConfig(agentType string, incoming, existing core.NotificationConfig) core.NotificationConfig {
	return core.PreserveNotificationAgentConfig(agentType, incoming, existing)
}

// validateAgentConfig checks that required fields are present for the agent type.
func validateAgentConfig(agent core.NotificationAgent) error {
	return core.ValidateNotificationAgent(agent)
}

// handleListNotificationAgents returns all configured notification agents
// with credentials masked so webhook URLs and tokens are never exposed to
// the frontend.
//
// Response: JSON array of core.NotificationAgent.
func (s *Server) handleListNotificationAgents(w http.ResponseWriter, r *http.Request) {
	cfg := s.Core.Config.Get()
	agents := cfg.AutoSync.NotificationAgents
	if agents == nil {
		agents = []core.NotificationAgent{}
	}
	for i, a := range agents {
		agents[i].Config = maskAgentConfig(a.Type, a.Config)
	}
	writeJSON(w, agents)
}

// handleCreateNotificationAgent validates and persists a new notification agent.
// The agent receives a server-generated ID. Multiple agents of the same provider
// type are allowed (e.g. two Discord channels).
//
// Request:  JSON core.NotificationAgent (max 8 KiB).
// Response: JSON core.NotificationAgent with masked credentials.
func (s *Server) handleCreateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	agent, ok := decodeJSON[core.NotificationAgent](w, r, 8192)
	if !ok {
		return
	}
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	created, err := s.Core.Config.AddNotificationAgent(agent)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	created.Config = maskAgentConfig(created.Type, created.Config)
	writeJSON(w, created)
}

// handleUpdateNotificationAgent replaces an existing notification agent by ID.
// Credential fields that arrive as masked placeholders are transparently
// restored to their stored values via preserveAgentConfig.
//
// Request:  JSON core.NotificationAgent (max 8 KiB).
// Response: JSON core.NotificationAgent with masked credentials.
func (s *Server) handleUpdateNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agent, ok := decodeJSON[core.NotificationAgent](w, r, 8192)
	if !ok {
		return
	}
	existing, found := s.Core.Config.GetNotificationAgent(id)
	if !found {
		writeError(w, 404, "Notification agent not found")
		return
	}
	agent.Config = preserveAgentConfig(agent.Type, agent.Config, existing.Config)
	if err := validateAgentConfig(agent); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated, err := s.Core.Config.UpdateNotificationAgent(id, agent)
	if err != nil {
		writeError(w, 404, err.Error())
		return
	}
	updated.Config = maskAgentConfig(updated.Type, updated.Config)
	writeJSON(w, updated)
}

// handleDeleteNotificationAgent removes a notification agent by ID.
//
// Response: JSON {"status": "deleted"}.
func (s *Server) handleDeleteNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Core.Config.DeleteNotificationAgent(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleTestNotificationAgentInline tests agent credentials sent inline in the
// request body without requiring a saved agent ID. Used by the add-agent modal
// so users can verify connectivity before committing the configuration.
//
// Request:  JSON core.NotificationAgent (max 4 KiB).
// Response: JSON {"results": []agents.TestResult}.
func (s *Server) handleTestNotificationAgentInline(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[core.NotificationAgent](w, r, 4096)
	if !ok {
		return
	}
	s.runNotificationAgentTest(w, r, req)
}

// handleTestNotificationAgent fires test messages for an already-saved agent.
// Looks up the agent by path parameter {id} and delegates to the shared test runner.
//
// Response: JSON {"results": []agents.TestResult}.
func (s *Server) handleTestNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := s.Core.Config.GetNotificationAgent(id)
	if !ok {
		writeError(w, 404, "Notification agent not found")
		return
	}
	s.runNotificationAgentTest(w, r, existing)
}

// runNotificationAgentTest executes the test logic for any notification agent
// and writes the JSON response. Shared by both the inline and saved-agent test handlers.
func (s *Server) runNotificationAgentTest(w http.ResponseWriter, r *http.Request, req core.NotificationAgent) {
	results, err := core.TestNotificationAgent(r.Context(), s.Core, req)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeJSON(w, map[string]any{"results": results})
}
