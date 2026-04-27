package agents

import (
	"io"
	"net/http"
	"strings"
)

// Agent describes one configured notification provider instance.
// Users may create multiple Agent entries, including multiple entries of the
// same Type (e.g. two Discord webhooks for different channels). Agents are
// persisted in the config's AutoSync.NotificationAgents slice.
type Agent struct {
	ID      string `json:"id"`      // stable unique identifier for updates/deletes
	Name    string `json:"name"`    // user-defined label, e.g. "Discord #alerts"
	Type    string `json:"type"`    // registered provider type, e.g. "discord" | "gotify" | "pushover"
	Enabled bool   `json:"enabled"` // false keeps config saved but skips delivery
	Events  Events `json:"events"`  // event subscription flags
	Config  Config `json:"config"`  // provider-specific credentials and options
}

// Events controls which application events trigger notifications for an agent.
// Each flag corresponds to a distinct event category in Clonarr's lifecycle.
// When a flag is false the agent is silently skipped for that event type.
type Events struct {
	OnSyncSuccess bool `json:"onSyncSuccess"` // auto-sync applied changes successfully
	OnSyncFailure bool `json:"onSyncFailure"` // auto-sync encountered an error
	OnCleanup     bool `json:"onCleanup"`     // stale rules/history removed during startup cleanup
	OnRepoUpdate  bool `json:"onRepoUpdate"`  // TRaSH Guides repository pulled new commits
	OnChangelog   bool `json:"onChangelog"`   // new weekly changelog section detected in TRaSH updates.txt
}

// Config holds credentials and options for all supported providers.
// This is a union struct — each provider uses only the fields relevant to its
// own Type. Fields are omitempty so unrelated providers do not bloat the JSON
// persisted to clonarr.json.
//
// When adding a new provider, append its fields here with an omitempty tag and
// a grouping comment. Then implement MaskConfig and PreserveConfig in the
// provider to handle credential round-trips with the UI.
type Config struct {
	// Discord — webhook URLs for embed-based notifications.
	DiscordWebhook        string `json:"discordWebhook,omitempty"`        // primary webhook (sync, cleanup, errors)
	DiscordWebhookUpdates string `json:"discordWebhookUpdates,omitempty"` // optional separate channel for repo/changelog events

	// Gotify — self-hosted push notification server.
	GotifyURL              string `json:"gotifyUrl,omitempty"`              // base server URL (e.g. https://gotify.example.com)
	GotifyToken            string `json:"gotifyToken,omitempty"`            // application token for message submission
	GotifyPriorityCritical bool   `json:"gotifyPriorityCritical,omitempty"` // enable delivery for SeverityCritical
	GotifyPriorityWarning  bool   `json:"gotifyPriorityWarning,omitempty"`  // enable delivery for SeverityWarning
	GotifyPriorityInfo     bool   `json:"gotifyPriorityInfo,omitempty"`     // enable delivery for SeverityInfo
	GotifyCriticalValue    *int   `json:"gotifyCriticalValue,omitempty"`    // Gotify priority int for critical (nil = 0)
	GotifyWarningValue     *int   `json:"gotifyWarningValue,omitempty"`     // Gotify priority int for warning (nil = 0)
	GotifyInfoValue        *int   `json:"gotifyInfoValue,omitempty"`        // Gotify priority int for info (nil = 0)

	// Pushover — third-party push notification service.
	PushoverUserKey  string `json:"pushoverUserKey,omitempty"`  // user/group key from Pushover dashboard
	PushoverAppToken string `json:"pushoverAppToken,omitempty"` // application API token from Pushover dashboard
}

// TestResult captures the outcome of one provider-specific test channel.
type TestResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status == "error"
}

const (
	statusOK    = "ok"
	statusError = "error"
)

// Severity indicates the semantic importance of an outgoing notification.
// Providers may use this to set visual styling (color, priority level) or to
// gate delivery entirely (e.g. Gotify skips severities the user has disabled).
type Severity string

const (
	SeverityInfo     Severity = "info"     // routine success events (auto-sync applied)
	SeverityWarning  Severity = "warning"  // notable but non-critical events (cleanup, changelog)
	SeverityCritical Severity = "critical" // errors requiring user attention (sync failure)
)

// Route indicates which logical channel an agent should use.
// Currently only Discord distinguishes between routes (main webhook vs.
// updates webhook). Providers that do not support multiple channels simply
// ignore this value and deliver to their single endpoint.
type Route string

const (
	RouteDefault Route = "default" // primary channel (sync results, cleanup, errors)
	RouteUpdates Route = "updates" // secondary channel (repo updates, changelog)
)

// Payload is the provider-agnostic message contract for outbound notifications.
// A single Payload is created by the caller (autosync, cleanup, repo-update)
// and dispatched to every matching agent. Providers may use TypeMessages for
// per-platform formatting overrides (e.g. Gotify needs markdown bullets while
// Discord uses embed descriptions).
type Payload struct {
	Title        string            // short title, e.g. "Clonarr: Auto-Sync Applied"
	Message      string            // default provider message body (markdown)
	TypeMessages map[string]string // optional per-provider body override keyed by provider type (e.g. {"gotify": "..."})
	Color        int               // embed accent color (hex int) for providers that support it (Discord)
	Severity     Severity          // semantic importance — providers may map to priority levels or colors
	Route        Route             // logical delivery channel — multi-channel providers use this to pick an endpoint
}

// messageFor returns the provider-specific message override when present.
func (p Payload) messageFor(agentType string) string {
	if len(p.TypeMessages) == 0 {
		return p.Message
	}
	if msg, ok := p.TypeMessages[strings.ToLower(strings.TrimSpace(agentType))]; ok && msg != "" {
		return msg
	}
	return p.Message
}

// severityOrDefault returns SeverityInfo when payload severity is unset.
func (p Payload) severityOrDefault() Severity {
	if p.Severity == "" {
		return SeverityInfo
	}
	return p.Severity
}

// routeOrDefault returns RouteDefault when payload route is unset.
func (p Payload) routeOrDefault() Route {
	if p.Route == "" {
		return RouteDefault
	}
	return p.Route
}

// HTTPPoster is the HTTP capability required by notification providers.
// Both [Runtime.NotifyClient] and [Runtime.SafeClient] satisfy this interface
// (Go's *http.Client implements both Post and Do).
// Using an interface rather than *http.Client allows test doubles and SSRF-safe wrappers.
type HTTPPoster interface {
	Post(url, contentType string, body io.Reader) (*http.Response, error)
	Do(req *http.Request) (*http.Response, error)
}

// Runtime bundles process-scoped dependencies injected into providers at
// dispatch time. Providers must never construct their own HTTP clients;
// the caller decides which client is appropriate based on trust level.
type Runtime struct {
	Version      string     // application version string, used in provider message footers
	NotifyClient HTTPPoster // standard HTTP client for trusted first-party destinations (e.g. Gotify on LAN)
	SafeClient   HTTPPoster // SSRF-protected HTTP client for untrusted user-supplied URLs (e.g. Discord webhooks)
}
