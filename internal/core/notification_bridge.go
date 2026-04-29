package core

import (
	"clonarr/internal/core/agents"
	"clonarr/internal/utils"
	"context"
)

// notification_bridge.go provides the bridge between the core application
// package and the agents sub-package. It exists for two reasons:
//
//  1. Type aliasing: core-facing code uses domain-specific names like
//     NotificationPayload and NotificationSeverity, while the agents package
//     uses shorter names (Payload, Severity). The type aliases here allow
//     the rest of core to use the domain names without importing agents directly.
//
//  2. Dependency injection: providers need HTTP clients and the app version
//     at dispatch time. The notificationRuntime method adapts App fields into
//     the agents.Runtime struct, keeping providers decoupled from App.
//
// All notification dispatch ultimately flows through:
//
//	core caller → DispatchNotificationAgent → agents.DispatchAgent → provider.Notify

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

// notificationRuntime adapts App dependencies into the agents.Runtime contract.
// Called once per dispatch/test to snapshot the current app state. The returned
// Runtime is safe for concurrent use because App.Version is immutable and the
// HTTP clients are stateless.
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
// The context allows callers (API handlers) to set request deadlines.
func TestNotificationAgent(ctx context.Context, app *App, agent NotificationAgent) ([]NotificationTestResult, error) {
	return agents.TestAgent(ctx, app.notificationRuntime(), agent)
}

// DispatchNotificationAgent sends a notification payload through one configured agent.
// Async-capable providers are dispatched via utils.SafeGo to isolate panics from
// provider code and prevent slow external APIs from blocking the caller.
// Uses context.Background because autosync dispatch has no parent context.
func (app *App) DispatchNotificationAgent(agent NotificationAgent, payload NotificationPayload) {
	agents.DispatchAgent(context.Background(), app.notificationRuntime(), agent, payload, func(name string, fn func()) {
		utils.SafeGo(name, fn)
	})
}
