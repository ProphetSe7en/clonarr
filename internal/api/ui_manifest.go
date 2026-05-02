// ui_manifest.go aggregates UI metadata that the frontend consumes once at
// load time so dropdowns, modal forms, and category-color CSS variables can
// be driven entirely by the backend. Adding a new sync mode, auth mode, or
// notification provider becomes a one-place Go change — the manifest picks
// it up automatically and the frontend re-renders without HTML edits.
//
// Only static configuration is exposed here. Per-instance live data (Arr
// profiles, custom formats fetched from a running Sonarr/Radarr) keeps its
// dedicated endpoints; this is metadata about Clonarr's own option space.

package api

import (
	"clonarr/internal/core"
	"clonarr/internal/core/agents"
	"net/http"
)

// UIManifest is the response payload of GET /api/ui/manifest.
//
// All fields are JSON-marshalable enum/metadata structures; the frontend
// uses them to render <option> lists, set CSS custom properties for
// category colors, and lay out the notification-agent modal generically.
type UIManifest struct {
	AppTypes              []core.EnumValue       `json:"appTypes"`
	SyncBehaviorAddModes  []core.EnumValue       `json:"syncBehaviorAddModes"`
	SyncBehaviorRemoveModes []core.EnumValue     `json:"syncBehaviorRemoveModes"`
	SyncBehaviorResetModes []core.EnumValue      `json:"syncBehaviorResetModes"`
	AuthModes             []core.EnumValue       `json:"authModes"`
	AuthRequiredModes     []core.EnumValue       `json:"authRequiredModes"`
	PullIntervalPresets   []core.EnumValue       `json:"pullIntervalPresets"`
	SessionTTLBounds      core.IntBounds         `json:"sessionTtlBounds"`
	CFCategories          []core.CategoryMeta    `json:"cfCategories"`
	ProfileGroups         []core.CategoryMeta    `json:"profileGroups"`
	NotificationAgents    []agents.AgentTypeMeta `json:"notificationAgents"`
}

// handleGetUIManifest returns the static UI manifest.
//
// The payload is small (~3 KB JSON), depends only on compile-time
// constants, and never changes during process lifetime. We still serve it
// fresh on each request rather than computing once because http.ServeMux
// already costs less than the JSON encode itself.
func (s *Server) handleGetUIManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(w, UIManifest{
		AppTypes:                core.AppTypes,
		SyncBehaviorAddModes:    core.SyncBehaviorAddModes,
		SyncBehaviorRemoveModes: core.SyncBehaviorRemoveModes,
		SyncBehaviorResetModes:  core.SyncBehaviorResetModes,
		AuthModes:               core.AuthModes,
		AuthRequiredModes:       core.AuthRequiredModes,
		PullIntervalPresets:     core.PullIntervalPresets,
		SessionTTLBounds:        core.SessionTTLBounds,
		CFCategories:            core.CFCategories,
		ProfileGroups:           core.ProfileGroups,
		NotificationAgents:      agents.AllAgentTypeMeta(),
	})
}
