package agents

import (
	"io"
	"net/http"
	"strings"
)

// Agent is a configured notification provider instance.
type Agent struct {
	ID      string `json:"id"`
	Name    string `json:"name"` // user-defined label, e.g. "Discord #alerts"
	Type    string `json:"type"` // registered provider type, e.g. "discord" | "gotify" | "pushover"
	Enabled bool   `json:"enabled"`
	Events  Events `json:"events"`
	Config  Config `json:"config"`
}

// Events controls which auto-sync events trigger this agent.
type Events struct {
	OnSyncSuccess bool `json:"onSyncSuccess"`
	OnSyncFailure bool `json:"onSyncFailure"`
	OnCleanup     bool `json:"onCleanup"`
	OnRepoUpdate  bool `json:"onRepoUpdate"`
	OnChangelog   bool `json:"onChangelog"`
}

// Config holds provider-specific credentials and settings.
// Fields are omitempty so unused providers add no JSON bloat.
// Adding a new provider = append fields here + register a Provider.
type Config struct {
	// Discord
	DiscordWebhook        string `json:"discordWebhook,omitempty"`
	DiscordWebhookUpdates string `json:"discordWebhookUpdates,omitempty"`
	// Gotify
	GotifyURL              string `json:"gotifyUrl,omitempty"`
	GotifyToken            string `json:"gotifyToken,omitempty"`
	GotifyPriorityCritical bool   `json:"gotifyPriorityCritical,omitempty"`
	GotifyPriorityWarning  bool   `json:"gotifyPriorityWarning,omitempty"`
	GotifyPriorityInfo     bool   `json:"gotifyPriorityInfo,omitempty"`
	GotifyCriticalValue    *int   `json:"gotifyCriticalValue,omitempty"`
	GotifyWarningValue     *int   `json:"gotifyWarningValue,omitempty"`
	GotifyInfoValue        *int   `json:"gotifyInfoValue,omitempty"`
	// Pushover
	PushoverUserKey  string `json:"pushoverUserKey,omitempty"`
	PushoverAppToken string `json:"pushoverAppToken,omitempty"`
}

// TestResult captures the outcome of a single notification-channel probe.
type TestResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status == "error"
}

const (
	statusOK    = "ok"
	statusError = "error"
)

// Severity indicates the semantic severity of an outgoing notification.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Route indicates which logical channel an agent should use.
// Providers that do not support routing can ignore this.
type Route string

const (
	RouteDefault Route = "default"
	RouteUpdates Route = "updates"
)

// Payload is the provider-agnostic message contract for outbound notifications.
type Payload struct {
	Title        string            // short title, e.g. "Clonarr: Auto-Sync Applied"
	Message      string            // default provider message body
	TypeMessages map[string]string // optional provider-type override, e.g. {"gotify": "..."}
	Color        int               // embed color for providers that support it
	Severity     Severity
	Route        Route
}

func (p Payload) messageFor(agentType string) string {
	if len(p.TypeMessages) == 0 {
		return p.Message
	}
	if msg, ok := p.TypeMessages[strings.ToLower(strings.TrimSpace(agentType))]; ok && msg != "" {
		return msg
	}
	return p.Message
}

func (p Payload) severityOrDefault() Severity {
	if p.Severity == "" {
		return SeverityInfo
	}
	return p.Severity
}

func (p Payload) routeOrDefault() Route {
	if p.Route == "" {
		return RouteDefault
	}
	return p.Route
}

// HTTPPoster is the minimal capability providers need from HTTP clients.
type HTTPPoster interface {
	Post(url, contentType string, body io.Reader) (*http.Response, error)
}

// Runtime holds process dependencies required by providers at runtime.
type Runtime struct {
	Version      string
	NotifyClient HTTPPoster
	SafeClient   HTTPPoster
}
