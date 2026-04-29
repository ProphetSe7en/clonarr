package agents

import (
	"context"
	"strings"
	"testing"
)

// TestAppriseValidate covers required-field guards.
func TestAppriseValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing API URL",
			agent:   Agent{Name: "Apprise", Type: "apprise", Config: Config{AppriseURLs: []string{"discord://x/y"}}},
			wantErr: "apprise URL is required",
		},
		{
			name:    "no notification URLs",
			agent:   Agent{Name: "Apprise", Type: "apprise", Config: Config{AppriseURL: "https://apprise.example.com"}},
			wantErr: "at least one Apprise notification URL is required",
		},
		{
			name:    "only blank notification URLs",
			agent:   Agent{Name: "Apprise", Type: "apprise", Config: Config{AppriseURL: "https://apprise.example.com", AppriseURLs: []string{"", "  "}}},
			wantErr: "at least one Apprise notification URL is required",
		},
		{
			name: "valid",
			agent: Agent{Name: "Apprise", Type: "apprise", Config: Config{
				AppriseURL:  "https://apprise.example.com",
				AppriseURLs: []string{"discord://x/y"},
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

// TestAppriseMaskAndPreserve verifies token + URL list mask round-trip.
// Apprise URLs embed credentials (e.g. discord webhook id+token) so the
// whole URL is treated as secret.
func TestAppriseMaskAndPreserve(t *testing.T) {
	cfg := Config{
		AppriseURL:   "https://apprise.example.com",
		AppriseToken: "tk_secret",
		AppriseURLs:  []string{"discord://abc/def", "mailto://user:pass@host"},
	}

	masked := MaskConfigByType("apprise", cfg)
	if masked.AppriseToken != maskedToken {
		t.Fatalf("token not masked, got %q", masked.AppriseToken)
	}
	if len(masked.AppriseURLs) != 2 {
		t.Fatalf("URL list length lost during mask")
	}
	for i, u := range masked.AppriseURLs {
		if u != maskedToken {
			t.Errorf("URLs[%d] not masked: %q", i, u)
		}
	}
	// Public field — server URL — must remain visible (not a secret).
	if masked.AppriseURL != cfg.AppriseURL {
		t.Errorf("API URL should not be masked, got %q", masked.AppriseURL)
	}

	restored := PreserveConfigByType("apprise", masked, cfg)
	if restored.AppriseToken != cfg.AppriseToken {
		t.Errorf("token not preserved, got %q", restored.AppriseToken)
	}
	if len(restored.AppriseURLs) != len(cfg.AppriseURLs) {
		t.Fatalf("URL list length changed during preserve")
	}
	for i := range restored.AppriseURLs {
		if restored.AppriseURLs[i] != cfg.AppriseURLs[i] {
			t.Errorf("URLs[%d] not preserved: got %q want %q", i, restored.AppriseURLs[i], cfg.AppriseURLs[i])
		}
	}
}

// TestAppriseMaskedPreserveWithEdit verifies that when ONE incoming URL is
// edited (not masked) and others are masked, the user's edit is accepted
// AS-IS — we only do all-or-nothing preserve when every URL still holds
// the placeholder. This avoids silently inheriting old URLs across an edit.
func TestAppriseMaskedPreserveWithEdit(t *testing.T) {
	existing := Config{AppriseURLs: []string{"discord://old1/x", "mailto://old2@x"}}
	incoming := Config{AppriseURLs: []string{maskedToken, "mailto://NEW@x"}}

	restored := PreserveConfigByType("apprise", incoming, existing)
	// Length matches but not all masked → keep incoming verbatim
	if restored.AppriseURLs[0] != maskedToken {
		t.Errorf("expected masked placeholder kept when other entry was edited, got %q", restored.AppriseURLs[0])
	}
	if restored.AppriseURLs[1] != "mailto://NEW@x" {
		t.Errorf("expected user's new URL kept verbatim, got %q", restored.AppriseURLs[1])
	}
}

// TestAppriseTestHappyPath runs Test against a mock returning 200.
func TestAppriseTestHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Config: Config{
		AppriseURL:  "https://apprise.example.com",
		AppriseURLs: []string{"discord://x/y"},
	}}

	results, err := appriseProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if len(results) != 1 || results[0].Status != statusOK {
		t.Fatalf("expected one OK result, got %+v", results)
	}
	if mock.lastURL != "https://apprise.example.com/notify" {
		t.Fatalf("URL = %q", mock.lastURL)
	}
}

// TestAppriseTestHTTPError captures network errors via TestResult.Error.
func TestAppriseTestHTTPError(t *testing.T) {
	rt := testRuntime(newErrorPoster("connection refused"), nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Config: Config{
		AppriseURL:  "https://apprise.example.com",
		AppriseURLs: []string{"discord://x/y"},
	}}

	results, err := appriseProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() should not return top-level error: %v", err)
	}
	if results[0].Status != statusError || !strings.Contains(results[0].Error, "connection refused") {
		t.Fatalf("expected connection error in result, got %+v", results[0])
	}
}

// TestAppriseTestHTTPStatus reports 4xx/5xx as an error per channel.
func TestAppriseTestHTTPStatus(t *testing.T) {
	rt := testRuntime(newStatusPoster(401), nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Config: Config{
		AppriseURL:   "https://apprise.example.com",
		AppriseURLs:  []string{"discord://x/y"},
		AppriseToken: "bad",
	}}

	results, err := appriseProvider{}.Test(context.Background(), rt, agent)
	if err != nil {
		t.Fatalf("Test() error: %v", err)
	}
	if results[0].Status != statusError {
		t.Fatalf("401 should produce error, got %q", results[0].Status)
	}
}

// TestAppriseTestNilClient ensures missing NotifyClient surfaces clearly.
func TestAppriseTestNilClient(t *testing.T) {
	rt := testRuntime(nil, nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Config: Config{
		AppriseURL:  "https://apprise.example.com",
		AppriseURLs: []string{"discord://x/y"},
	}}

	_, err := appriseProvider{}.Test(context.Background(), rt, agent)
	if err == nil {
		t.Fatal("expected error when NotifyClient is nil")
	}
}

// TestAppriseTestMissingFields verifies the same guard fires from Test as
// from Validate.
func TestAppriseTestMissingFields(t *testing.T) {
	rt := testRuntime(newOKPoster(), nil)

	// Missing API URL
	agent := Agent{Type: "apprise", Config: Config{AppriseURLs: []string{"x"}}}
	_, err := appriseProvider{}.Test(context.Background(), rt, agent)
	if err == nil || !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("expected API URL guard, got %v", err)
	}

	// Missing notification URLs
	agent = Agent{Type: "apprise", Config: Config{AppriseURL: "https://apprise.example.com"}}
	_, err = appriseProvider{}.Test(context.Background(), rt, agent)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("expected URL list guard, got %v", err)
	}
}

// TestAppriseNotifyHappyPath verifies that Notify reaches the API URL.
func TestAppriseNotifyHappyPath(t *testing.T) {
	mock := newOKPoster()
	rt := testRuntime(mock, nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Enabled: true, Config: Config{
		AppriseURL:  "https://apprise.example.com",
		AppriseURLs: []string{"discord://x/y"},
	}}

	err := appriseProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title: "Sync", Message: "all clear", Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("Notify() error: %v", err)
	}
	if mock.lastURL != "https://apprise.example.com/notify" {
		t.Fatalf("URL = %q", mock.lastURL)
	}
}

// TestAppriseNotifyHTTPError surfaces non-2xx as an error.
func TestAppriseNotifyHTTPError(t *testing.T) {
	rt := testRuntime(newStatusPoster(500), nil)
	agent := Agent{Name: "Apprise", Type: "apprise", Enabled: true, Config: Config{
		AppriseURL:  "https://apprise.example.com",
		AppriseURLs: []string{"discord://x/y"},
	}}

	err := appriseProvider{}.Notify(context.Background(), rt, agent, Payload{
		Title: "Fail", Message: "x", Severity: SeverityCritical,
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

// TestAppriseSeverityTypeMapping covers the severity → Apprise type mapping.
func TestAppriseSeverityTypeMapping(t *testing.T) {
	cases := map[Severity]string{
		SeverityCritical: "failure",
		SeverityWarning:  "warning",
		SeverityInfo:     "info",
	}
	for sev, want := range cases {
		if got := appriseTypeForSeverity(sev); got != want {
			t.Errorf("%s: got %q, want %q", sev, got, want)
		}
	}
}

// TestFilterEmpty covers the helper that cleans Apprise URL list.
func TestFilterEmpty(t *testing.T) {
	got := filterEmpty([]string{"a", "", "  ", "b", "  c  "})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
