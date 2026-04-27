package core

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"clonarr/internal/utils"
)

// NotificationTestResult captures the outcome of a single notification-channel probe.
type NotificationTestResult struct {
	Label  string `json:"label"`
	Status string `json:"status"`          // "ok" or "error"
	Error  string `json:"error,omitempty"` // set when status == "error"
}

const (
	notificationStatusOK    = "ok"
	notificationStatusError = "error"
)

// NotificationSeverity indicates the semantic severity of an outgoing notification.
type NotificationSeverity string

const (
	NotificationSeverityInfo     NotificationSeverity = "info"
	NotificationSeverityWarning  NotificationSeverity = "warning"
	NotificationSeverityCritical NotificationSeverity = "critical"
)

// NotificationRoute indicates which logical channel an agent should use.
// Providers that do not support routing can ignore this.
type NotificationRoute string

const (
	NotificationRouteDefault NotificationRoute = "default"
	NotificationRouteUpdates NotificationRoute = "updates"
)

// NotificationPayload is the provider-agnostic message contract for outbound notifications.
type NotificationPayload struct {
	Title        string            // short title, e.g. "Clonarr: Auto-Sync Applied"
	Message      string            // default provider message body
	TypeMessages map[string]string // optional provider-type override, e.g. {"gotify": "..."}
	Color        int               // embed color for providers that support it
	Severity     NotificationSeverity
	Route        NotificationRoute
}

func (p NotificationPayload) messageFor(agentType string) string {
	if len(p.TypeMessages) == 0 {
		return p.Message
	}
	if msg, ok := p.TypeMessages[strings.ToLower(strings.TrimSpace(agentType))]; ok && msg != "" {
		return msg
	}
	return p.Message
}

func (p NotificationPayload) severityOrDefault() NotificationSeverity {
	if p.Severity == "" {
		return NotificationSeverityInfo
	}
	return p.Severity
}

func (p NotificationPayload) routeOrDefault() NotificationRoute {
	if p.Route == "" {
		return NotificationRouteDefault
	}
	return p.Route
}

// NotificationAgentProvider encapsulates provider-specific behavior.
// New providers are added by implementing this interface and registering once.
type NotificationAgentProvider interface {
	Type() string
	Validate(agent NotificationAgent) error
	MaskConfig(config NotificationConfig) NotificationConfig
	PreserveConfig(incoming, existing NotificationConfig) NotificationConfig
	Test(app *App, agent NotificationAgent) ([]NotificationTestResult, error)
	Notify(app *App, agent NotificationAgent, payload NotificationPayload) error
	Async() bool
}

const (
	maskedDiscordWebhook = "https://discord.com/api/webhooks/[MASKED]/[MASKED]"
	maskedToken          = "••••••••••••••••" // 16 bullets — visually distinct from real credentials
)

func maskSecret(s, placeholder string) string {
	if s == "" {
		return ""
	}
	return placeholder
}

func preserveIfMasked(incoming, existing, placeholder string) string {
	if incoming == placeholder {
		return existing
	}
	return incoming
}

var (
	notificationProvidersMu sync.RWMutex
	notificationProviders   = make(map[string]NotificationAgentProvider)
)

func registerNotificationAgentProvider(provider NotificationAgentProvider) {
	if err := RegisterNotificationAgentProvider(provider); err != nil {
		panic(err)
	}
}

// RegisterNotificationAgentProvider registers a provider implementation by type.
func RegisterNotificationAgentProvider(provider NotificationAgentProvider) error {
	if provider == nil {
		return fmt.Errorf("notification provider is nil")
	}

	pt := strings.ToLower(strings.TrimSpace(provider.Type()))
	if pt == "" {
		return fmt.Errorf("notification provider type is required")
	}

	notificationProvidersMu.Lock()
	defer notificationProvidersMu.Unlock()

	if _, exists := notificationProviders[pt]; exists {
		return fmt.Errorf("notification provider %q already registered", pt)
	}
	notificationProviders[pt] = provider
	return nil
}

// GetNotificationAgentProvider returns a provider by configured type.
func GetNotificationAgentProvider(agentType string) (NotificationAgentProvider, bool) {
	notificationProvidersMu.RLock()
	defer notificationProvidersMu.RUnlock()
	p, ok := notificationProviders[strings.ToLower(strings.TrimSpace(agentType))]
	return p, ok
}

// SupportedNotificationAgentTypes returns all registered provider types sorted alphabetically.
func SupportedNotificationAgentTypes() []string {
	notificationProvidersMu.RLock()
	defer notificationProvidersMu.RUnlock()
	types := make([]string, 0, len(notificationProviders))
	for t := range notificationProviders {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func unknownNotificationTypeError(agentType string) error {
	types := SupportedNotificationAgentTypes()
	if len(types) == 0 {
		return fmt.Errorf("unknown agent type: %q", agentType)
	}
	return fmt.Errorf("unknown agent type: %q (expected %s)", agentType, strings.Join(types, " | "))
}

// MaskNotificationAgentConfig masks credential fields for the given agent type.
func MaskNotificationAgentConfig(agentType string, nc NotificationConfig) NotificationConfig {
	provider, ok := GetNotificationAgentProvider(agentType)
	if !ok {
		return nc
	}
	return provider.MaskConfig(nc)
}

// PreserveNotificationAgentConfig preserves credential fields if the UI sends back placeholders.
func PreserveNotificationAgentConfig(agentType string, incoming, existing NotificationConfig) NotificationConfig {
	provider, ok := GetNotificationAgentProvider(agentType)
	if !ok {
		return incoming
	}
	return provider.PreserveConfig(incoming, existing)
}

// ValidateNotificationAgent validates common and provider-specific settings.
func ValidateNotificationAgent(agent NotificationAgent) error {
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("name is required")
	}
	provider, ok := GetNotificationAgentProvider(agent.Type)
	if !ok {
		return unknownNotificationTypeError(agent.Type)
	}
	return provider.Validate(agent)
}

// TestNotificationAgent probes an inline or persisted agent configuration.
func TestNotificationAgent(app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	provider, ok := GetNotificationAgentProvider(agent.Type)
	if !ok {
		return nil, unknownNotificationTypeError(agent.Type)
	}
	return provider.Test(app, agent)
}

// DispatchNotificationAgent sends a notification payload through one configured agent.
func (app *App) DispatchNotificationAgent(agent NotificationAgent, payload NotificationPayload) {
	if !agent.Enabled {
		return
	}

	provider, ok := GetNotificationAgentProvider(agent.Type)
	if !ok {
		log.Printf("Notification %q skipped: unknown agent type %q", agent.Name, agent.Type)
		return
	}

	agentPayload := payload
	agentPayload.Message = payload.messageFor(agent.Type)
	agentPayload.Severity = payload.severityOrDefault()
	agentPayload.Route = payload.routeOrDefault()

	send := func() {
		if err := provider.Notify(app, agent, agentPayload); err != nil {
			log.Printf("Notification %q (%s) send failed: %v", agent.Name, provider.Type(), err)
		}
	}

	if provider.Async() {
		utils.SafeGo("notify-"+provider.Type(), send)
		return
	}

	send()
}
