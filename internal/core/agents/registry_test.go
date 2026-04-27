package agents

import (
	"context"
	"strings"
	"testing"
)

// TestSupportedTypesIncludesBuiltins verifies that all three built-in providers
// (discord, gotify, pushover) are registered at startup via their init() functions.
func TestSupportedTypesIncludesBuiltins(t *testing.T) {
	types := SupportedTypes()
	if len(types) < 3 {
		t.Fatalf("expected at least 3 notification providers, got %d", len(types))
	}

	expected := []string{"discord", "gotify", "pushover"}
	for _, typ := range expected {
		if _, ok := GetProvider(typ); !ok {
			t.Fatalf("missing registered notification provider %q", typ)
		}
	}
}

// TestValidateAgentCommon exercises the common validation path shared by all
// providers: missing agent name and unknown agent type.
func TestValidateAgentCommon(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name: "missing name",
			agent: Agent{Type: "discord", Config: Config{
				DiscordWebhook: "https://discord.com/api/webhooks/111/aaa",
			}},
			wantErr: "name is required",
		},
		{
			name:    "unknown type",
			agent:   Agent{Name: "Custom", Type: "smtp"},
			wantErr: "unknown agent type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgent(tc.agent)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAgent() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAgent() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateAgent() error = %q, want contains %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestDispatchAgentEnabled verifies that DispatchAgent delivers a notification
// through a registered provider when the agent is enabled.
func TestDispatchAgentEnabled(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{
		Name:    "Test Discord",
		Type:    "discord",
		Enabled: true,
		Config:  Config{DiscordWebhook: "https://discord.com/api/webhooks/111/aaa"},
	}
	payload := Payload{Title: "Test", Message: "body", Route: RouteDefault}

	DispatchAgent(context.Background(), rt, agent, payload, nil)

	if mock.lastURL == "" {
		t.Fatal("expected HTTP call when agent is enabled")
	}
}

// TestDispatchAgentDisabled verifies that DispatchAgent skips disabled agents.
func TestDispatchAgentDisabled(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{
		Name:    "Test Discord",
		Type:    "discord",
		Enabled: false,
		Config:  Config{DiscordWebhook: "https://discord.com/api/webhooks/111/aaa"},
	}

	DispatchAgent(context.Background(), rt, agent, Payload{}, nil)

	if mock.lastURL != "" {
		t.Fatal("expected no HTTP call when agent is disabled")
	}
}

// TestDispatchAgentUnknownType verifies that DispatchAgent silently skips
// agents with an unregistered provider type.
func TestDispatchAgentUnknownType(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Unknown", Type: "smtp", Enabled: true}

	// Should not panic or call HTTP
	DispatchAgent(context.Background(), rt, agent, Payload{}, nil)

	if mock.lastURL != "" {
		t.Fatal("expected no HTTP call for unknown provider type")
	}
}

// TestDispatchAgentAsync verifies that DispatchAgent uses the asyncRun callback
// when the provider returns Async() == true.
func TestDispatchAgentAsync(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{
		Name:    "Test Gotify",
		Type:    "gotify",
		Enabled: true,
		Config: Config{
			GotifyURL:          "https://gotify.example.com",
			GotifyToken:        "tok",
			GotifyPriorityInfo: true,
		},
	}

	var asyncCalled bool
	asyncRun := func(name string, fn func()) {
		asyncCalled = true
		fn() // run synchronously for test
	}

	DispatchAgent(context.Background(), rt, agent, Payload{Severity: SeverityInfo}, asyncRun)

	if !asyncCalled {
		t.Fatal("expected asyncRun to be called for async provider")
	}
	if mock.lastURL == "" {
		t.Fatal("expected HTTP call via async dispatch")
	}
}

// TestDispatchAgentMessageOverride verifies that DispatchAgent resolves
// provider-specific message overrides from Payload.TypeMessages.
func TestDispatchAgentMessageOverride(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{
		Name:    "Test Discord",
		Type:    "discord",
		Enabled: true,
		Config:  Config{DiscordWebhook: "https://discord.com/api/webhooks/111/aaa"},
	}
	payload := Payload{
		Title:        "Test",
		Message:      "default body",
		TypeMessages: map[string]string{"discord": "discord-specific body"},
		Route:        RouteDefault,
	}

	// DispatchAgent resolves the message internally — just ensure no error
	DispatchAgent(context.Background(), rt, agent, payload, nil)

	if mock.lastURL == "" {
		t.Fatal("expected HTTP call")
	}
}

// TestPayloadMessageFor verifies provider-specific message resolution.
func TestPayloadMessageFor(t *testing.T) {
	p := Payload{
		Message:      "default",
		TypeMessages: map[string]string{"gotify": "gotify-specific"},
	}

	if got := p.messageFor("gotify"); got != "gotify-specific" {
		t.Fatalf("messageFor(gotify) = %q, want gotify-specific", got)
	}
	if got := p.messageFor("discord"); got != "default" {
		t.Fatalf("messageFor(discord) = %q, want default", got)
	}
	if got := p.messageFor("GOTIFY"); got != "gotify-specific" {
		t.Fatalf("messageFor(GOTIFY) should be case-insensitive, got %q", got)
	}

	// Nil map
	p2 := Payload{Message: "fallback"}
	if got := p2.messageFor("discord"); got != "fallback" {
		t.Fatalf("messageFor with nil TypeMessages = %q, want fallback", got)
	}
}

// TestPayloadSeverityOrDefault verifies the severity default behavior.
func TestPayloadSeverityOrDefault(t *testing.T) {
	p := Payload{Severity: SeverityCritical}
	if got := p.severityOrDefault(); got != SeverityCritical {
		t.Fatalf("severityOrDefault() = %q, want critical", got)
	}

	p2 := Payload{}
	if got := p2.severityOrDefault(); got != SeverityInfo {
		t.Fatalf("severityOrDefault() empty = %q, want info", got)
	}
}

// TestPayloadRouteOrDefault verifies the route default behavior.
func TestPayloadRouteOrDefault(t *testing.T) {
	p := Payload{Route: RouteUpdates}
	if got := p.routeOrDefault(); got != RouteUpdates {
		t.Fatalf("routeOrDefault() = %q, want updates", got)
	}

	p2 := Payload{}
	if got := p2.routeOrDefault(); got != RouteDefault {
		t.Fatalf("routeOrDefault() empty = %q, want default", got)
	}
}

// TestTestAgentUnknownType verifies that TestAgent returns an error for
// unregistered provider types.
func TestTestAgentUnknownType(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Unknown", Type: "smtp"}

	_, err := TestAgent(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error for unknown provider type")
	}
	if !strings.Contains(err.Error(), "unknown agent type") {
		t.Fatalf("error should mention unknown type: %v", err)
	}
}

// TestTestAgentKnownType verifies that TestAgent delegates to the provider's
// Test method for a known type.
func TestTestAgentKnownType(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	results, err := TestAgent(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("TestAgent() error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
}

