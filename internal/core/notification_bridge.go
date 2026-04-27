package core

import (
	"clonarr/internal/core/agents"
	"clonarr/internal/utils"
)

// NotificationPayload is the provider-agnostic message contract for outbound notifications.
type NotificationPayload = agents.Payload

// NotificationTestResult captures the outcome of a single notification-channel probe.
type NotificationTestResult = agents.TestResult

// NotificationSeverity indicates the semantic severity of an outgoing notification.
type NotificationSeverity = agents.Severity

const (
	NotificationSeverityInfo     NotificationSeverity = agents.SeverityInfo
	NotificationSeverityWarning  NotificationSeverity = agents.SeverityWarning
	NotificationSeverityCritical NotificationSeverity = agents.SeverityCritical
)

// NotificationRoute indicates which logical channel an agent should use.
type NotificationRoute = agents.Route

const (
	NotificationRouteDefault NotificationRoute = agents.RouteDefault
	NotificationRouteUpdates NotificationRoute = agents.RouteUpdates
)

func (app *App) notificationRuntime() agents.Runtime {
	return agents.Runtime{
		Version:      app.Version,
		NotifyClient: app.NotifyClient,
		SafeClient:   app.SafeClient,
	}
}

// MaskNotificationAgentConfig masks credential fields for the given agent type.
func MaskNotificationAgentConfig(agentType string, cfg NotificationConfig) NotificationConfig {
	return agents.MaskConfigByType(agentType, cfg)
}

// PreserveNotificationAgentConfig preserves credential fields if the UI sends back placeholders.
func PreserveNotificationAgentConfig(agentType string, incoming, existing NotificationConfig) NotificationConfig {
	return agents.PreserveConfigByType(agentType, incoming, existing)
}

// ValidateNotificationAgent validates common and provider-specific settings.
func ValidateNotificationAgent(agent NotificationAgent) error {
	return agents.ValidateAgent(agent)
}

// TestNotificationAgent probes an inline or persisted agent configuration.
func TestNotificationAgent(app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	return agents.TestAgent(app.notificationRuntime(), agent)
}

// DispatchNotificationAgent sends a notification payload through one configured agent.
func (app *App) DispatchNotificationAgent(agent NotificationAgent, payload NotificationPayload) {
	agents.DispatchAgent(app.notificationRuntime(), agent, payload, func(name string, fn func()) {
		utils.SafeGo(name, fn)
	})
}
