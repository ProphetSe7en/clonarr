package agents

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
)

// Provider encapsulates provider-specific behavior.
// New providers are added by implementing this interface and registering once.
type Provider interface {
	Type() string
	Validate(agent Agent) error
	MaskConfig(config Config) Config
	PreserveConfig(incoming, existing Config) Config
	Test(runtime Runtime, agent Agent) ([]TestResult, error)
	Notify(runtime Runtime, agent Agent, payload Payload) error
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
	providersMu sync.RWMutex
	providers   = make(map[string]Provider)
)

func registerProvider(provider Provider) {
	if err := RegisterProvider(provider); err != nil {
		panic(err)
	}
}

// RegisterProvider registers a provider implementation by type.
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

// GetProvider returns a provider by configured type.
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
		return cfg
	}
	return provider.MaskConfig(cfg)
}

// PreserveConfigByType preserves credential fields if the UI sends back placeholders.
func PreserveConfigByType(agentType string, incoming, existing Config) Config {
	provider, ok := GetProvider(agentType)
	if !ok {
		return incoming
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
func TestAgent(runtime Runtime, agent Agent) ([]TestResult, error) {
	provider, ok := GetProvider(agent.Type)
	if !ok {
		return nil, unknownTypeError(agent.Type)
	}
	return provider.Test(runtime, agent)
}

// DispatchAgent sends a notification payload through one configured agent.
// asyncRun is optional; when provided and provider.Async()==true, it is used
// to run notifications asynchronously.
func DispatchAgent(runtime Runtime, agent Agent, payload Payload, asyncRun func(name string, fn func())) {
	if !agent.Enabled {
		return
	}

	provider, ok := GetProvider(agent.Type)
	if !ok {
		log.Printf("Notification %q skipped: unknown agent type %q", agent.Name, agent.Type)
		return
	}

	agentPayload := payload
	agentPayload.Message = payload.messageFor(agent.Type)
	agentPayload.Severity = payload.severityOrDefault()
	agentPayload.Route = payload.routeOrDefault()

	send := func() {
		if err := provider.Notify(runtime, agent, agentPayload); err != nil {
			log.Printf("Notification %q (%s) send failed: %v", agent.Name, provider.Type(), err)
		}
	}

	if provider.Async() && asyncRun != nil {
		asyncRun("notify-"+provider.Type(), send)
		return
	}

	send()
}
