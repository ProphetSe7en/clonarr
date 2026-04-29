package agents

import (
	"context"
	"strings"
	"testing"
)

// TestPushoverValidate verifies the Pushover provider's Validate logic:
// missing user key / app token and valid config.
func TestPushoverValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing credentials",
			agent:   Agent{Name: "Pushover", Type: "pushover"},
			wantErr: "pushover user key and app token are required",
		},
		{
			name: "valid",
			agent: Agent{Name: "Pushover", Type: "pushover", Config: Config{
				PushoverUserKey:  "user",
				PushoverAppToken: "app",
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

// TestPushoverMaskAndPreserve verifies the credential mask/preserve round-trip:
// MaskConfigByType replaces both Pushover credentials with placeholders, and
// PreserveConfigByType restores the originals when those placeholders are submitted back.
func TestPushoverMaskAndPreserve(t *testing.T) {
	cfg := Config{PushoverUserKey: "user", PushoverAppToken: "app"}
	masked := MaskConfigByType("pushover", cfg)
	if masked.PushoverUserKey != maskedToken || masked.PushoverAppToken != maskedToken {
		t.Fatalf("pushover credentials not masked")
	}
	restored := PreserveConfigByType("pushover", masked, cfg)
	if restored.PushoverUserKey != cfg.PushoverUserKey || restored.PushoverAppToken != cfg.PushoverAppToken {
		t.Fatalf("pushover credentials not preserved")
	}
}

// TestPushoverTestHappyPath verifies that Test sends a probe message and
// returns an OK result when the HTTP client returns 200.
func TestPushoverTestHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	results, err := pushoverProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != statusOK {
		t.Fatalf("expected OK, got status=%q error=%q", results[0].Status, results[0].Error)
	}
	if mock.lastURL != pushoverAPIURL {
		t.Fatalf("expected Pushover API URL, got %q", mock.lastURL)
	}
}

// TestPushoverTestHTTPError verifies that Test captures errors from the HTTP client.
func TestPushoverTestHTTPError(t *testing.T) {
	mock := newErrorPoster("connection refused")
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	results, err := pushoverProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() should not return top-level error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("expected error status, got %q", results[0].Status)
	}
	if !strings.Contains(results[0].Error, "connection refused") {
		t.Fatalf("error should contain 'connection refused': %q", results[0].Error)
	}
}

// TestPushoverTestHTTPStatus verifies that Test reports HTTP error status codes.
func TestPushoverTestHTTPStatus(t *testing.T) {
	mock := newStatusPoster(401)
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "bad-token",
	}}

	results, err := pushoverProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("expected error for 401, got %q", results[0].Status)
	}
}

// TestPushoverTestNilClient verifies that Test returns an error when
// SafeClient is not configured.
func TestPushoverTestNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	_, err := pushoverProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when SafeClient is nil")
	}
}

// TestPushoverTestMissingCredentials verifies Test returns the same
// missing-credentials message as Validate.
func TestPushoverTestMissingCredentials(t *testing.T) {
	rt := testRuntime(nil, newOKPoster())
	agent := Agent{Name: "Pushover", Type: "pushover", Config: Config{}}

	_, err := pushoverProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when credentials are missing")
	}
	if !strings.Contains(err.Error(), "pushover user key and app token are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPushoverNotifyHappyPath verifies that Notify sends to the Pushover API
// and returns nil on success.
func TestPushoverNotifyHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Enabled: true, Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	err := pushoverProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title:    "Test",
		Message:  "body",
		Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if mock.lastURL != pushoverAPIURL {
		t.Fatalf("expected Pushover API URL, got %q", mock.lastURL)
	}
}

// TestPushoverNotifyHTTPError verifies that Notify returns errors on HTTP failure.
func TestPushoverNotifyHTTPError(t *testing.T) {
	mock := newStatusPoster(500)
	rt := testRuntime(nil, mock)
	agent := Agent{Name: "Pushover", Type: "pushover", Enabled: true, Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	err := pushoverProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

// TestPushoverNotifyNilClient verifies Notify returns an error when SafeClient is nil.
func TestPushoverNotifyNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Pushover", Type: "pushover", Enabled: true, Config: Config{
		PushoverUserKey:  "user",
		PushoverAppToken: "app",
	}}

	err := pushoverProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err == nil {
		t.Fatal("expected error when SafeClient is nil")
	}
}

// TestPushoverNotifyMissingCredentials verifies Notify silently skips when
// credentials are empty.
func TestPushoverNotifyMissingCredentials(t *testing.T) {
	rt := testRuntime(nil, newOKPoster())
	agent := Agent{Name: "Pushover", Type: "pushover", Enabled: true, Config: Config{}}

	err := pushoverProvider{}.Notify(context.Background(), rt, agent, Payload{Severity: SeverityInfo})
	if err != nil {
		t.Fatalf("expected nil for empty credentials, got: %v", err)
	}
}

// TestPushoverPriority verifies severity-to-priority mapping:
// critical → 1 (high), warning → 0 (normal), info → 0 (normal).
func TestPushoverPriority(t *testing.T) {
	tests := []struct {
		severity Severity
		want     int
	}{
		{SeverityCritical, 1},
		{SeverityWarning, 0},
		{SeverityInfo, 0},
		{"", 0},        // unset severity
		{"unknown", 0}, // unrecognized severity
	}
	for _, tc := range tests {
		got := pushoverPriority(tc.severity)
		if got != tc.want {
			t.Errorf("pushoverPriority(%q) = %d, want %d", tc.severity, got, tc.want)
		}
	}
}
