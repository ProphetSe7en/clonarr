package agents

import (
	"context"
	"strings"
	"testing"
)

// TestGotifyValidate verifies the Gotify provider's Validate logic:
// missing URL/token credentials and valid config.
func TestGotifyValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing credentials",
			agent:   Agent{Name: "Gotify", Type: "gotify"},
			wantErr: "gotify URL and token are required",
		},
		{
			name: "valid",
			agent: Agent{Name: "Gotify", Type: "gotify", Config: Config{
				GotifyURL:   "https://gotify.example.com",
				GotifyToken: "tok123",
			}},
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

// TestGotifyMaskAndPreserve verifies the credential mask/preserve round-trip:
// MaskConfigByType replaces the Gotify token with a placeholder, and
// PreserveConfigByType restores the original when that placeholder is submitted back.
func TestGotifyMaskAndPreserve(t *testing.T) {
	cfg := Config{GotifyURL: "https://gotify.example.com", GotifyToken: "tok123"}
	masked := MaskConfigByType("gotify", cfg)
	if masked.GotifyToken != maskedToken {
		t.Fatalf("gotify token not masked")
	}
	restored := PreserveConfigByType("gotify", masked, cfg)
	if restored.GotifyToken != cfg.GotifyToken {
		t.Fatalf("gotify token not preserved")
	}
}

// TestGotifyPriorityForSeverity exercises the severity-to-priority mapping:
// verifies custom priority values are returned for enabled severities, and that
// disabled severities return (0, false) to suppress delivery.
func TestGotifyPriorityForSeverity(t *testing.T) {
	p := gotifyProvider{}
	critical, warning, info := 8, 5, 3
	cfg := Config{
		GotifyPriorityCritical: true,
		GotifyPriorityWarning:  true,
		GotifyPriorityInfo:     true,
		GotifyCriticalValue:    &critical,
		GotifyWarningValue:     &warning,
		GotifyInfoValue:        &info,
	}

	if got, ok := p.priorityForSeverity(cfg, SeverityCritical); !ok || got != 8 {
		t.Fatalf("critical priority = %d, enabled = %t", got, ok)
	}
	if got, ok := p.priorityForSeverity(cfg, SeverityWarning); !ok || got != 5 {
		t.Fatalf("warning priority = %d, enabled = %t", got, ok)
	}
	if got, ok := p.priorityForSeverity(cfg, SeverityInfo); !ok || got != 3 {
		t.Fatalf("info priority = %d, enabled = %t", got, ok)
	}

	cfg.GotifyPriorityInfo = false
	if _, ok := p.priorityForSeverity(cfg, SeverityInfo); ok {
		t.Fatalf("info severity should be disabled")
	}
}

// TestNormalizeGotifyMarkdown verifies that markdown normalization doubles
// newlines before bold headers and list items for clean Gotify rendering.
func TestNormalizeGotifyMarkdown(t *testing.T) {
	input := "line\n**header**\n- item"
	out := normalizeGotifyMarkdown(input)
	if !strings.Contains(out, "\n\n**header**") {
		t.Fatalf("expected double newline before header, got %q", out)
	}
	if !strings.Contains(out, "\n\n- item") {
		t.Fatalf("expected double newline before list item, got %q", out)
	}
}

// TestGotifyTestHappyPath verifies that Test sends a probe message and
// returns an OK result when the HTTP client returns 200.
func TestGotifyTestHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:   "https://gotify.example.com",
		GotifyToken: "tok123",
	}}

	results, err := gotifyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != statusOK {
		t.Fatalf("expected OK, got status=%q error=%q", results[0].Status, results[0].Error)
	}
	if !strings.Contains(mock.lastURL, "gotify.example.com/message") {
		t.Fatalf("expected Gotify message URL, got %q", mock.lastURL)
	}
}

// TestGotifyTestHTTPError verifies that Test captures errors from the HTTP client.
func TestGotifyTestHTTPError(t *testing.T) {
	mock := newErrorPoster("no such host")
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:   "https://gotify.example.com",
		GotifyToken: "tok123",
	}}

	results, err := gotifyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() should not return top-level error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("expected error status, got %q", results[0].Status)
	}
}

// TestGotifyTestHTTPStatus verifies that Test reports HTTP error status codes.
func TestGotifyTestHTTPStatus(t *testing.T) {
	mock := newStatusPoster(401)
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:   "https://gotify.example.com",
		GotifyToken: "bad-token",
	}}

	results, err := gotifyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("expected error status for 401, got %q", results[0].Status)
	}
	if !strings.Contains(results[0].Error, "401") {
		t.Fatalf("error should mention 401: %q", results[0].Error)
	}
}

// TestGotifyTestNilClient verifies that Test returns an error when
// NotifyClient is not configured.
func TestGotifyTestNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:   "https://gotify.example.com",
		GotifyToken: "tok123",
	}}

	_, err := gotifyProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when NotifyClient is nil")
	}
}

// TestGotifyTestMissingCredentials verifies Test returns the same
// missing-credentials message as Validate.
func TestGotifyTestMissingCredentials(t *testing.T) {
	rt := testRuntime(newOKPoster(), nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{}}

	_, err := gotifyProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when credentials are missing")
	}
	if !strings.Contains(err.Error(), "gotify URL and token are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGotifyNotifyHappyPath verifies Notify sends to the correct URL with
// severity-based priority.
func TestGotifyNotifyHappyPath(t *testing.T) {
	critical := 8
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:              "https://gotify.example.com",
		GotifyToken:            "tok123",
		GotifyPriorityCritical: true,
		GotifyCriticalValue:    &critical,
	}}

	err := gotifyProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title:    "Test",
		Message:  "body",
		Severity: SeverityCritical,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if !strings.Contains(mock.lastURL, "gotify.example.com/message") {
		t.Fatalf("expected Gotify URL, got %q", mock.lastURL)
	}
}

// TestGotifyNotifyDisabledSeverity verifies that Notify silently skips when
// the payload's severity is disabled in the agent config.
func TestGotifyNotifyDisabledSeverity(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:          "https://gotify.example.com",
		GotifyToken:        "tok123",
		GotifyPriorityInfo: false, // info disabled
	}}

	err := gotifyProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title:    "Test",
		Message:  "body",
		Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("expected nil when severity disabled, got: %v", err)
	}
	if mock.lastURL != "" {
		t.Fatalf("expected no HTTP call when severity disabled, got URL %q", mock.lastURL)
	}
}

// TestGotifyNotifyHTTPError verifies Notify returns errors on HTTP failure.
func TestGotifyNotifyHTTPError(t *testing.T) {
	mock := newStatusPoster(500)
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:          "https://gotify.example.com",
		GotifyToken:        "tok123",
		GotifyPriorityInfo: true,
	}}

	err := gotifyProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention 500: %v", err)
	}
}

// TestGotifyNotifyNilClient verifies Notify returns an error when
// NotifyClient is nil.
func TestGotifyNotifyNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{
		GotifyURL:          "https://gotify.example.com",
		GotifyToken:        "tok123",
		GotifyPriorityInfo: true,
	}}

	err := gotifyProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err == nil {
		t.Fatal("expected error when NotifyClient is nil")
	}
}

// TestGotifyNotifyMissingCredentials verifies Notify silently skips when
// credentials are empty.
func TestGotifyNotifyMissingCredentials(t *testing.T) {
	rt := testRuntime(newOKPoster(), nil)
	agent := Agent{Name: "Gotify", Type: "gotify", Config: Config{}}

	err := gotifyProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err != nil {
		t.Fatalf("expected nil for empty credentials, got: %v", err)
	}
}

// TestGotifyPriorityNilDefaults verifies that enabled severities with nil
// custom values default to priority 0.
func TestGotifyPriorityNilDefaults(t *testing.T) {
	p := gotifyProvider{}
	cfg := Config{
		GotifyPriorityCritical: true,
		GotifyPriorityWarning:  true,
		GotifyPriorityInfo:     true,
		// All *Value fields are nil
	}

	for _, sev := range []Severity{SeverityCritical, SeverityWarning, SeverityInfo} {
		got, ok := p.priorityForSeverity(cfg, sev)
		if !ok {
			t.Fatalf("severity %q should be enabled", sev)
		}
		if got != 0 {
			t.Fatalf("severity %q with nil value should default to 0, got %d", sev, got)
		}
	}
}
