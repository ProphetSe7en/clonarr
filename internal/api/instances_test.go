package api

import "testing"

func TestIsBlockedHost(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantBlocked bool
	}{
		{"loopback v4", "http://127.0.0.1/", true},
		{"loopback v6", "http://[::1]/", true},
		{"aws metadata", "http://169.254.169.254/", true},
		{"link-local v4", "http://169.254.1.1/", true},
		{"unspecified", "http://0.0.0.0/", true},
		{"v4-mapped loopback", "http://[::ffff:127.0.0.1]/", true},
		{"public v4", "http://1.1.1.1/", false},
		{"public v6", "http://[2606:4700:4700::1111]/", false},
		{"rfc1918", "http://10.0.0.5/", false},
		{"tailscale cgnat", "http://100.99.136.67/", false},
		{"empty", "", true},
		{"parse error", "http://%zz/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, reason := isBlockedHost(tc.url)
			if blocked != tc.wantBlocked {
				t.Errorf("isBlockedHost(%q) blocked=%v reason=%q, want blocked=%v",
					tc.url, blocked, reason, tc.wantBlocked)
			}
			if blocked && reason == "" {
				t.Errorf("isBlockedHost(%q) blocked but returned empty reason", tc.url)
			}
			if !blocked && reason != "" {
				t.Errorf("isBlockedHost(%q) not blocked but returned reason %q", tc.url, reason)
			}
		})
	}
}
