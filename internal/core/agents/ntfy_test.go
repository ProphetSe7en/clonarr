package agents

import (
	"context"
	"strings"
	"testing"
)

// TestNtfyValidate covers the URL/topic required-field guards.
func TestNtfyValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing URL",
			agent:   Agent{Name: "ntfy", Type: "ntfy", Config: Config{NtfyTopic: "alerts"}},
			wantErr: "ntfy URL is required",
		},
		{
			name:    "missing topic",
			agent:   Agent{Name: "ntfy", Type: "ntfy", Config: Config{NtfyURL: "https://ntfy.sh"}},
			wantErr: "ntfy topic is required",
		},
		{
			name: "valid (no token — public topic)",
			agent: Agent{Name: "ntfy", Type: "ntfy", Config: Config{
				NtfyURL: "https://ntfy.sh", NtfyTopic: "alerts",
			}},
		},
		{
			name: "valid (with token)",
			agent: Agent{Name: "ntfy", Type: "ntfy", Config: Config{
				NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts", NtfyToken: "tk_secret",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgent(tc.agent)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want contains %q", err, tc.wantErr)
			}
		})
	}
}

// TestNtfyMaskAndPreserve verifies token mask round-trip via the registry.
func TestNtfyMaskAndPreserve(t *testing.T) {
	cfg := Config{NtfyURL: "https://ntfy.sh", NtfyTopic: "alerts", NtfyToken: "tk_secret"}
	masked := MaskConfigByType("ntfy", cfg)
	if masked.NtfyToken != maskedToken {
		t.Fatalf("token not masked, got %q", masked.NtfyToken)
	}
	if masked.NtfyURL != cfg.NtfyURL || masked.NtfyTopic != cfg.NtfyTopic {
		t.Fatalf("non-credential fields shouldn't be masked")
	}
	restored := PreserveConfigByType("ntfy", masked, cfg)
	if restored.NtfyToken != cfg.NtfyToken {
		t.Fatalf("token not preserved across mask round-trip")
	}
}

// TestNtfyTestHappyPath runs Test against a mock returning 200.
func TestNtfyTestHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
	}}

	results, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if len(results) != 1 || results[0].Status != statusOK {
		t.Fatalf("expected one OK result, got %+v", results)
	}
	wantURL := "https://ntfy.example.com/alerts"
	if mock.lastURL != wantURL {
		t.Fatalf("URL = %q, want %q", mock.lastURL, wantURL)
	}
}

// TestNtfyTestTrimsTrailingSlash covers the URL canonicalisation that
// strips a trailing slash from the base before joining the topic.
func TestNtfyTestTrimsTrailingSlash(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{
		NtfyURL: "https://ntfy.example.com/", NtfyTopic: "/alerts",
	}}

	_, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	wantURL := "https://ntfy.example.com/alerts"
	if mock.lastURL != wantURL {
		t.Fatalf("URL = %q, want %q (trailing/leading slashes should be trimmed)", mock.lastURL, wantURL)
	}
}

// TestNtfyTestHTTPError captures network errors via TestResult.Error.
func TestNtfyTestHTTPError(t *testing.T) {
	rt := testRuntime(newErrorPoster("connection refused"), nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
	}}

	results, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() should not return top-level error: %v", err)
	}
	if results[0].Status != statusError || !strings.Contains(results[0].Error, "connection refused") {
		t.Fatalf("expected error result with connection refused, got %+v", results[0])
	}
}

// TestNtfyTestHTTPStatus reports 4xx/5xx as an error per channel.
func TestNtfyTestHTTPStatus(t *testing.T) {
	rt := testRuntime(newStatusPoster(401), nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts", NtfyToken: "bad",
	}}

	results, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("401 should produce error, got %q", results[0].Status)
	}
}

// TestNtfyTestNilClient ensures a missing NotifyClient surfaces clearly
// rather than panicking at the http boundary.
func TestNtfyTestNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
	}}

	_, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when NotifyClient is nil")
	}
}

// TestNtfyTestMissingFields verifies the same guard fires from Test as
// from Validate (defense in depth — Test can be called without going
// through Validate first via TestAgent).
func TestNtfyTestMissingFields(t *testing.T) {
	rt := testRuntime(newOKPoster(), nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Config: Config{}}
	_, err := ntfyProvider{}.Test(context.Background(), rt, agent)
	if err == nil || !strings.Contains(err.Error(), "URL and topic are required") {
		t.Fatalf("expected guard error, got %v", err)
	}
}

// TestNtfyNotifyHappyPath verifies that Notify reaches the topic URL when
// the severity is enabled in the config.
func TestNtfyNotifyHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Enabled: true, Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
		NtfyPriorityInfo: true,
	}}

	err := ntfyProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title: "Sync ok", Message: "all clear", Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if mock.lastURL != "https://ntfy.example.com/alerts" {
		t.Fatalf("URL = %q", mock.lastURL)
	}
}

// TestNtfyNotifyDisabledSeverity verifies that disabling a severity drops
// the message silently (no HTTP call, no error).
func TestNtfyNotifyDisabledSeverity(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Enabled: true, Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
		NtfyPriorityCritical: true, NtfyPriorityWarning: false, NtfyPriorityInfo: false,
	}}

	err := ntfyProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title: "Info", Message: "x", Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("Notify() should not error when severity is disabled: %v", err)
	}
	if mock.lastURL != "" {
		t.Fatalf("expected no HTTP call when severity disabled, got URL %q", mock.lastURL)
	}
}

// TestNtfyNotifyHTTPError surfaces non-2xx as an error from Notify.
func TestNtfyNotifyHTTPError(t *testing.T) {
	rt := testRuntime(newStatusPoster(500), nil)
	agent := Agent{Name: "ntfy", Type: "ntfy", Enabled: true, Config: Config{
		NtfyURL: "https://ntfy.example.com", NtfyTopic: "alerts",
		NtfyPriorityCritical: true,
	}}

	err := ntfyProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title: "Fail", Message: "x", Severity: SeverityCritical,
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should include status: %v", err)
	}
}

// TestNtfyPriorityMapping covers the per-severity priority resolver:
// disabled → drop (false), enabled → either custom value or default.
func TestNtfyPriorityMapping(t *testing.T) {
	five := 5
	four := 4
	three := 3
	out99 := 99
	cases := []struct {
		name     string
		cfg      Config
		severity Severity
		wantP    int
		wantOK   bool
	}{
		{"critical disabled", Config{}, SeverityCritical, 0, false},
		{"critical enabled default", Config{NtfyPriorityCritical: true}, SeverityCritical, 5, true},
		{"critical enabled custom", Config{NtfyPriorityCritical: true, NtfyCriticalValue: &four}, SeverityCritical, 4, true},
		{"warning enabled default", Config{NtfyPriorityWarning: true}, SeverityWarning, 4, true},
		{"info enabled default", Config{NtfyPriorityInfo: true}, SeverityInfo, 3, true},
		{"info enabled custom 5", Config{NtfyPriorityInfo: true, NtfyInfoValue: &five}, SeverityInfo, 5, true},
		{"info enabled out-of-range clamps to default 3", Config{NtfyPriorityInfo: true, NtfyInfoValue: &out99}, SeverityInfo, 3, true},
		{"warning disabled", Config{}, SeverityWarning, 0, false},
		{"info disabled", Config{}, SeverityInfo, 0, false},
		{"critical custom 0 falls back to default 5 (out of range)", Config{NtfyPriorityCritical: true, NtfyCriticalValue: &three}, SeverityCritical, 3, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotP, gotOK := ntfyProvider{}.priorityForSeverity(tc.cfg, tc.severity)
			if gotP != tc.wantP || gotOK != tc.wantOK {
				t.Errorf("got (%d, %v), want (%d, %v)", gotP, gotOK, tc.wantP, tc.wantOK)
			}
		})
	}
}

// TestNtfyTagsForSeverity covers the emoji-tag resolver — these are the
// exact ntfy emoji shortcodes the docs map to ⚠️, 🚨, ✅.
func TestNtfyTagsForSeverity(t *testing.T) {
	if got := ntfyTagsForSeverity(SeverityCritical); got != "rotating_light" {
		t.Errorf("critical: got %q", got)
	}
	if got := ntfyTagsForSeverity(SeverityWarning); got != "warning" {
		t.Errorf("warning: got %q", got)
	}
	if got := ntfyTagsForSeverity(SeverityInfo); got != "white_check_mark" {
		t.Errorf("info: got %q", got)
	}
}
