package core

import (
	"strings"
	"testing"
)

func TestRedactSecrets_HexKey(t *testing.T) {
	// Radarr/Sonarr API keys are 32 lowercase hex characters.
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare key", "abc123def456789012345678901234ab", "[REDACTED]"},
		{"key in URL", "GET /api/v3/system/status?apikey=abc123def456789012345678901234ab", "GET /api/v3/system/status?apikey=[REDACTED]"},
		{"key in error", "Auth failed: invalid key abc123def456789012345678901234ab provided", "Auth failed: invalid key [REDACTED] provided"},
		{"31 char (under threshold)", "abc123def456789012345678901234a", "abc123def456789012345678901234a"},
		{"33 char (still hex blob)", "abc123def456789012345678901234abc", "[REDACTED]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSecrets(tc.in)
			if got != tc.want {
				t.Errorf("redactSecrets(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactSecrets_BearerToken(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Authorization: Bearer eyJabc123def456ghijk", "Authorization: Bearer [REDACTED]"},
		{"Authorization: Token abcdef1234567890XYZ", "Authorization: Token [REDACTED]"},
		{"authorization: bearer mytokenvalue1234567890", "authorization: bearer [REDACTED]"},
		{"Header dump: Authorization: Bearer abc, X-API-Key: xyz789", "Header dump: Authorization: Bearer [REDACTED], X-API-Key: [REDACTED]"},
	}
	for _, tc := range cases {
		got := redactSecrets(tc.in)
		if got != tc.want {
			t.Errorf("redactSecrets(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactSecrets_JWT(t *testing.T) {
	in := "Body: {\"token\":\"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\"}"
	got := redactSecrets(in)
	if !strings.Contains(got, "[REDACTED]") || strings.Contains(got, "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9") {
		t.Errorf("expected JWT replaced with [REDACTED], got %q", got)
	}
}

func TestRedactSecrets_QueryParam(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"GET /foo?apikey=secret123&page=2", "GET /foo?apikey=[REDACTED]&page=2"},
		{"POST body: api_key=mysecret&name=foo", "POST body: api_key=[REDACTED]&name=foo"},
		{"access_token=ya29.A0ARrdaM-token", "access_token=[REDACTED]"},
		{"secret=mypassword!", "secret=[REDACTED]"},
	}
	for _, tc := range cases {
		got := redactSecrets(tc.in)
		if got != tc.want {
			t.Errorf("redactSecrets(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFlattenForLog(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single line unchanged", "single line", "single line"},
		{"newlines collapsed", "line one\nline two", "line one line two"},
		{"crlf collapsed", "line one\r\nline two", "line one line two"},
		{"tab collapsed", "col1\tcol2", "col1 col2"},
		{"runs of spaces collapsed", "a   b      c", "a b c"},
		{"arr 400 response", "HTTP 400: [\n  {\n    \"propertyName\": \"\",\n    \"errorMessage\": \"foo\"\n  }\n]", "HTTP 400: [ { \"propertyName\": \"\", \"errorMessage\": \"foo\" } ]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flattenForLog(tc.in)
			if got != tc.want {
				t.Errorf("flattenForLog(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRedactSecrets_LeavesNonSecretsAlone(t *testing.T) {
	// Strings without secret patterns must pass through unchanged.
	cases := []string{
		"Plain log line about syncing PCOK",
		"Profile [SQP] SQP-1 WEB (1080p) → HD on Radarr-main",
		"Created CF in Radarr (id=42)",
		"abcdef (only 6 hex chars, not a key)",
	}
	for _, in := range cases {
		got := redactSecrets(in)
		if got != in {
			t.Errorf("redactSecrets(%q) modified to %q — should be unchanged", in, got)
		}
	}
}
