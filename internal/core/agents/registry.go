package agents

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

// Provider encapsulates provider-specific notification behavior.
// Each provider registers exactly once via init() and handles all operations
// for its notification backend. The interface methods form the complete
// lifecycle of a notification agent:
//
//   - [Provider.Type] — returns the unique registration key (e.g. "discord").
//   - [Provider.Validate] — checks required fields before saving an agent.
//   - [Provider.MaskConfig] — replaces secrets with placeholders for API responses.
//   - [Provider.PreserveConfig] — restores real secrets when UI submits placeholders.
//   - [Provider.Test] — sends a probe message and returns per-channel results.
//   - [Provider.Notify] — delivers one production notification.
//   - [Provider.Async] — signals whether Notify should run in a background goroutine.
//
// New providers are added by implementing this interface and calling
// registerProvider in an init() function.
type Provider interface {
	// Type returns the lowercase registration key (e.g. "discord", "gotify").
	// Must be stable — it is persisted in Agent.Type and used for registry lookup.
	Type() string

	// Validate checks that all required provider-specific fields are present
	// and well-formed. Called before creating or updating an agent.
	Validate(agent Agent) error

	// MaskConfig replaces sensitive credential fields with display-safe
	// placeholder values. The returned Config is safe to send to the UI.
	MaskConfig(config Config) Config

	// PreserveConfig compares incoming (from UI) against existing (from disk).
	// If a field still holds its masked placeholder value, the existing real
	// credential is preserved. Otherwise the user-supplied value is accepted.
	PreserveConfig(incoming, existing Config) Config

	// Test sends one or more probe messages and returns a TestResult per
	// channel. Errors are returned per-channel in TestResult.Error rather
	// than as a top-level error, unless the request itself is invalid.
	// The context allows callers to set deadlines and cancel in-flight requests.
	Test(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error)

	// Notify delivers one production notification. The payload has already
	// been resolved (message overrides, default severity/route applied).
	// The context allows callers to cancel during graceful shutdown.
	Notify(ctx context.Context, runtime Runtime, agent Agent, payload Payload) error

	// Async returns true when Notify should be dispatched in a background
	// goroutine (via the asyncRun callback in DispatchAgent). Providers
	// targeting external APIs with potentially high latency return true.
	Async() bool
}

const (
	// maskedDiscordWebhook is returned to the UI instead of raw webhook credentials.
	maskedDiscordWebhook = "https://discord.com/api/webhooks/[MASKED]/[MASKED]"
	// maskedToken is returned to the UI instead of bearer credentials.
	maskedToken = "••••••••••••••••"

	// testTitle is the embed/notification title used by all provider Test methods.
	testTitle = "Clonarr Test"
	// testColor is the Discord embed sidebar color used for test messages (blue).
	testColor = 0x58a6ff
)

// testMessage returns the standard test body for a given provider name.
func testMessage(providerName string) string {
	return "If you see this, " + providerName + " is configured correctly!"
}

// maskSecret returns placeholder when a secret exists, otherwise empty.
func maskSecret(s, placeholder string) string {
	if s == "" {
		return ""
	}
	return placeholder
}

// preserveIfMasked keeps existing when UI submits an unchanged placeholder value.
func preserveIfMasked(incoming, existing, placeholder string) string {
	if incoming == placeholder {
		return existing
	}
	return incoming
}

func maskConfigFallback(cfg Config) Config {
	cfg.DiscordWebhook = maskSecret(cfg.DiscordWebhook, maskedDiscordWebhook)
	cfg.DiscordWebhookUpdates = maskSecret(cfg.DiscordWebhookUpdates, maskedDiscordWebhook)
	cfg.GotifyToken = maskSecret(cfg.GotifyToken, maskedToken)
	cfg.PushoverUserKey = maskSecret(cfg.PushoverUserKey, maskedToken)
	cfg.PushoverAppToken = maskSecret(cfg.PushoverAppToken, maskedToken)
	return cfg
}

func preserveConfigFallback(incoming, existing Config) Config {
	incoming.DiscordWebhook = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhook), existing.DiscordWebhook, maskedDiscordWebhook)
	incoming.DiscordWebhookUpdates = preserveIfMasked(strings.TrimSpace(incoming.DiscordWebhookUpdates), existing.DiscordWebhookUpdates, maskedDiscordWebhook)
	incoming.GotifyToken = preserveIfMasked(strings.TrimSpace(incoming.GotifyToken), existing.GotifyToken, maskedToken)
	incoming.PushoverUserKey = preserveIfMasked(strings.TrimSpace(incoming.PushoverUserKey), existing.PushoverUserKey, maskedToken)
	incoming.PushoverAppToken = preserveIfMasked(strings.TrimSpace(incoming.PushoverAppToken), existing.PushoverAppToken, maskedToken)
	return incoming
}

var (
	providersMu sync.RWMutex
	providers   = make(map[string]Provider)
)

// ResetProviders clears all registered providers. This is intended for test
// isolation — it allows tests to exercise RegisterProvider error paths
// (e.g. duplicate registration) without polluting global state across test runs.
func ResetProviders() {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers = make(map[string]Provider)
}

// registerProvider is called by provider init() functions to add themselves
// to the global registry. Panics on duplicate or invalid registration because
// that indicates a programming error (provider type collision or nil provider).
func registerProvider(provider Provider) {
	if err := RegisterProvider(provider); err != nil {
		panic(err)
	}
}

// RegisterProvider registers a provider implementation by its Type() key.
// Returns an error if the provider is nil, has an empty type, or if a provider
// with the same type is already registered. Thread-safe.
func RegisterProvider(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("notification provider is nil")
	}

	pt := strings.ToLower(strings.TrimSpace(provider.Type()))
	if pt == "" {
		return fmt.Errorf("notification provider type is required")
	}

	providersMu.Lock()
	defer providersMu.Unlock()

	if _, exists := providers[pt]; exists {
		return fmt.Errorf("notification provider %q already registered", pt)
	}
	providers[pt] = provider
	return nil
}

// GetProvider returns the registered provider for the given agent type string.
// The lookup is case-insensitive and whitespace-trimmed. Thread-safe.
func GetProvider(agentType string) (Provider, bool) {
	providersMu.RLock()
	defer providersMu.RUnlock()
	p, ok := providers[strings.ToLower(strings.TrimSpace(agentType))]
	return p, ok
}

// SupportedTypes returns all registered provider types sorted alphabetically.
func SupportedTypes() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	types := make([]string, 0, len(providers))
	for t := range providers {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// unknownTypeError formats a user-facing error that lists all registered
// provider types so the caller can see what values are valid.
func unknownTypeError(agentType string) error {
	types := SupportedTypes()
	if len(types) == 0 {
		return fmt.Errorf("unknown agent type: %q", agentType)
	}
	return fmt.Errorf("unknown agent type: %q (expected %s)", agentType, strings.Join(types, " | "))
}

// MaskConfigByType masks credential fields for the given agent type.
func MaskConfigByType(agentType string, cfg Config) Config {
	provider, ok := GetProvider(agentType)
	if !ok {
		return maskConfigFallback(cfg)
	}
	return provider.MaskConfig(cfg)
}

// PreserveConfigByType preserves credential fields if the UI sends back placeholders.
func PreserveConfigByType(agentType string, incoming, existing Config) Config {
	provider, ok := GetProvider(agentType)
	if !ok {
		return preserveConfigFallback(incoming, existing)
	}
	return provider.PreserveConfig(incoming, existing)
}

// ValidateAgent validates common and provider-specific settings.
func ValidateAgent(agent Agent) error {
	if strings.TrimSpace(agent.Name) == "" {
		return fmt.Errorf("name is required")
	}
	provider, ok := GetProvider(agent.Type)
	if !ok {
		return unknownTypeError(agent.Type)
	}
	return provider.Validate(agent)
}

// TestAgent probes an inline or persisted agent configuration.
// The context is forwarded to the provider's Test method for cancellation/deadline support.
func TestAgent(ctx context.Context, runtime Runtime, agent Agent) ([]TestResult, error) {
	provider, ok := GetProvider(agent.Type)
	if !ok {
		return nil, unknownTypeError(agent.Type)
	}
	return provider.Test(ctx, runtime, agent)
}

// DispatchAgent sends a notification payload through one configured agent.
// The function is the main entry point for outbound notifications:
//
//  1. Skips disabled agents silently.
//  2. Looks up the registered provider by Agent.Type.
//  3. Resolves payload defaults (message override, severity, route).
//  4. Calls provider.Notify synchronously, or via asyncRun when the provider
//     opts into async delivery (provider.Async() == true) and asyncRun is non-nil.
//
// The context is forwarded to provider.Notify for cancellation/deadline support.
// asyncRun is typically utils.SafeGo, which isolates panics from provider code.
func DispatchAgent(ctx context.Context, runtime Runtime, agent Agent, payload Payload, asyncRun func(name string, fn func())) {
	if !agent.Enabled {
		return
	}

	provider, ok := GetProvider(agent.Type)
	if !ok {
		log.Printf("Notification %q skipped: unknown agent type %q", agent.Name, agent.Type)
		return
	}

	// Resolve defaults and provider-specific message overrides once per dispatch.
	agentPayload := payload
	agentPayload.Message = payload.messageFor(agent.Type)
	agentPayload.Severity = payload.severityOrDefault()
	agentPayload.Route = payload.routeOrDefault()

	send := func() {
		if err := provider.Notify(ctx, runtime, agent, agentPayload); err != nil {
			log.Printf("Notification %q (%s) send failed: %v", agent.Name, provider.Type(), err)
		}
	}

	if provider.Async() && asyncRun != nil {
		asyncRun("notify-"+provider.Type(), send)
		return
	}

	send()
}
