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
// with credentials masked.
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

// handleCreateNotificationAgent adds a new notification agent.
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

// handleUpdateNotificationAgent replaces a notification agent by ID.
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
func (s *Server) handleDeleteNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Core.Config.DeleteNotificationAgent(id); err != nil {
		writeError(w, 404, err.Error())
		return
	}
	writeJSON(w, map[string]string{"status": "deleted"})
}

// handleTestNotificationAgentInline tests agent credentials sent inline in the
// request body without requiring a saved agent ID. Used by the add-agent modal.
func (s *Server) handleTestNotificationAgentInline(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeJSON[core.NotificationAgent](w, r, 4096)
	if !ok {
		return
	}
	s.runNotificationAgentTest(w, req)
}

// handleTestNotificationAgent fires test messages for an existing saved agent.
func (s *Server) handleTestNotificationAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, ok := s.Core.Config.GetNotificationAgent(id)
	if !ok {
		writeError(w, 404, "Notification agent not found")
		return
	}
	s.runNotificationAgentTest(w, existing)
}

// runNotificationAgentTest executes the test logic for any notification agent
// and writes the JSON response. Shared by both the inline and saved-agent test handlers.
func (s *Server) runNotificationAgentTest(w http.ResponseWriter, req core.NotificationAgent) {
	results, err := core.TestNotificationAgent(s.Core, req)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	writeJSON(w, map[string]any{"results": results})
}
