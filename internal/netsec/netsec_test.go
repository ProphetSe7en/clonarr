package netsec

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
		reason  string
	}{
		// Loopback
		{"127.0.0.1", true, "IPv4 loopback"},
		{"127.0.0.5", true, "IPv4 loopback /8"},
		{"::1", true, "IPv6 loopback"},
		{"::ffff:127.0.0.1", true, "IPv4-mapped IPv6 loopback"},
		// RFC1918 private
		{"10.0.0.1", true, "10/8"},
		{"172.16.0.1", true, "172.16/12"},
		{"172.31.255.254", true, "172.31 edge"},
		{"192.168.86.22", true, "192.168/16"},
		{"::ffff:192.168.1.1", true, "IPv4-mapped private"},
		// IPv6 ULA
		{"fd00::1", true, "IPv6 ULA fc00::/7"},
		// Link-local
		{"169.254.169.254", true, "AWS metadata"},
		{"169.254.1.1", true, "link-local /16"},
		{"fe80::1", true, "IPv6 link-local"},
		// Unspecified
		{"0.0.0.0", true, "IPv4 unspecified"},
		{"::", true, "IPv6 unspecified"},
		// 0.0.0.0/8 (this host on this network)
		{"0.1.2.3", true, "0.0.0.0/8 range"},
		// Multicast
		{"224.0.0.1", true, "IPv4 multicast"},
		{"ff02::1", true, "IPv6 multicast"},
		// Broadcast
		{"255.255.255.255", true, "limited broadcast"},
		// Documentation / reserved
		{"192.0.2.1", true, "TEST-NET-1"},
		{"198.51.100.1", true, "TEST-NET-2"},
		{"203.0.113.1", true, "TEST-NET-3"},
		{"240.0.0.1", true, "Class E reserved"},
		{"100.64.0.1", true, "Carrier-grade NAT"},
		// NAT64
		{"64:ff9b::1", true, "NAT64 well-known"},
		// Should NOT be blocked
		{"8.8.8.8", false, "Google DNS (public)"},
		{"1.1.1.1", false, "Cloudflare DNS (public)"},
		{"140.82.112.4", false, "github.com public IP"},
		{"2001:4860:4860::8888", false, "Google DNS IPv6 (public)"},
	}
	for _, c := range cases {
		t.Run(c.ip+"_"+strings.ReplaceAll(c.reason, " ", "_"), func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("invalid test IP %q", c.ip)
			}
			got := IsBlockedIP(ip)
			if got != c.blocked {
				t.Errorf("IsBlockedIP(%s) = %v, want %v (%s)", c.ip, got, c.blocked, c.reason)
			}
		})
	}
}

func TestIsBlockedIPNil(t *testing.T) {
	if !IsBlockedIP(nil) {
		t.Error("IsBlockedIP(nil) should be true (conservative)")
	}
}

func TestIsLocalAddress(t *testing.T) {
	cases := []struct {
		ip    string
		local bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"::ffff:127.0.0.1", true},
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.20.0.1", true},
		{"169.254.1.1", true},
		{"fd00::1", true},
		// CGN explicitly NOT local (Tailscale etc.) — SSRF blocks but auth doesn't bypass
		{"100.64.0.1", false},
		// NAT64 not local either
		{"64:ff9b::1", false},
		// Public
		{"8.8.8.8", false},
		{"2001:4860:4860::8888", false},
		// Broadcast/multicast not local
		{"255.255.255.255", false},
		{"224.0.0.1", false},
	}
	for _, c := range cases {
		t.Run(c.ip, func(t *testing.T) {
			got := IsLocalAddress(net.ParseIP(c.ip))
			if got != c.local {
				t.Errorf("IsLocalAddress(%s) = %v, want %v", c.ip, got, c.local)
			}
		})
	}
}

func TestIsLocalAddressNil(t *testing.T) {
	// Conservative: nil is NOT local (can't bypass auth if we don't know).
	if IsLocalAddress(nil) {
		t.Error("IsLocalAddress(nil) should be false")
	}
}

func TestParseClientIP_NoProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.86.10:54321"
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	ip := ParseClientIP(r, nil)
	if ip.String() != "192.168.86.10" {
		t.Errorf("no trusted proxies: got %s, want 192.168.86.10 (ignore XFF)", ip)
	}
}

func TestParseClientIP_TrustedProxy_Rightmost(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:54321" // trusted proxy
	r.Header.Set("X-Forwarded-For", "8.8.8.8")
	trusted := []net.IP{net.ParseIP("172.17.0.1")}
	ip := ParseClientIP(r, trusted)
	if ip.String() != "8.8.8.8" {
		t.Errorf("single XFF: got %s, want 8.8.8.8", ip)
	}
}

func TestParseClientIP_SpoofedLeftmost(t *testing.T) {
	// Attack: client on the internet injects XFF=127.0.0.1 in their own request.
	// The trusted proxy APPENDS their real IP (8.8.8.8) to the header.
	// A leftmost-parser would see 127.0.0.1 → bypass auth. Rightmost sees 8.8.8.8.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:54321"
	r.Header.Set("X-Forwarded-For", "127.0.0.1, 8.8.8.8")
	trusted := []net.IP{net.ParseIP("172.17.0.1")}
	ip := ParseClientIP(r, trusted)
	if ip.String() != "8.8.8.8" {
		t.Errorf("spoofed leftmost: got %s, want 8.8.8.8 (rightmost non-trusted)", ip)
	}
	if IsLocalAddress(ip) {
		t.Error("spoofed leftmost: IsLocalAddress should be false — this is the attack")
	}
}

func TestParseClientIP_ChainedProxies(t *testing.T) {
	// Client → Proxy1 → Proxy2 → App. Both proxies trusted.
	// XFF should be "client, proxy1", rightmost non-trusted = client.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.2:54321" // proxy2
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 172.17.0.1")
	trusted := []net.IP{net.ParseIP("172.17.0.1"), net.ParseIP("172.17.0.2")}
	ip := ParseClientIP(r, trusted)
	if ip.String() != "1.2.3.4" {
		t.Errorf("chained proxies: got %s, want 1.2.3.4", ip)
	}
}

func TestParseClientIP_UntrustedSource(t *testing.T) {
	// Direct client, not a trusted proxy — XFF must be ignored entirely.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "8.8.8.8:54321"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	trusted := []net.IP{net.ParseIP("172.17.0.1")}
	ip := ParseClientIP(r, trusted)
	if ip.String() != "8.8.8.8" {
		t.Errorf("untrusted source: got %s, want 8.8.8.8 (ignore XFF)", ip)
	}
}

func TestParseClientIP_AllXFFAreTrusted(t *testing.T) {
	// Weird case: every XFF entry is a trusted proxy. Fall back to RemoteAddr.
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:54321"
	r.Header.Set("X-Forwarded-For", "172.17.0.2, 172.17.0.3")
	trusted := []net.IP{
		net.ParseIP("172.17.0.1"),
		net.ParseIP("172.17.0.2"),
		net.ParseIP("172.17.0.3"),
	}
	ip := ParseClientIP(r, trusted)
	if ip.String() != "172.17.0.1" {
		t.Errorf("all trusted: got %s, want 172.17.0.1 (fallback to RemoteAddr)", ip)
	}
}

func TestParseTrustedProxies_Valid(t *testing.T) {
	got, err := ParseTrustedProxies("172.17.0.1, 10.0.0.5,  192.168.1.1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d IPs, want 3", len(got))
	}
	want := []string{"172.17.0.1", "10.0.0.5", "192.168.1.1"}
	for i, w := range want {
		if got[i].String() != w {
			t.Errorf("idx %d: got %s, want %s", i, got[i], w)
		}
	}
}

func TestParseTrustedProxies_InvalidFailsLoud(t *testing.T) {
	// Genuinely malformed entry — has whitespace inside, can't be a
	// hostname (issue #40 added hostname support so "not-an-ip" alone
	// now passes parse-time validation as a syntactically-valid name
	// that just happens to fail DNS lookup).
	_, err := ParseTrustedProxies("172.17.0.1, bad host, 10.0.0.5")
	if err == nil {
		t.Error("expected error on invalid entry (fail loud)")
	}
}

func TestResolveTrustedProxies_HostnamesAccepted(t *testing.T) {
	// Hostname syntax must pass parse-time validation. Lookup is
	// allowed to fail (lab/CI may not have DNS for "proxy"), but the
	// hostname slice should still come back so a refresh can retry.
	ips, hostnames, err := ResolveTrustedProxies("172.17.0.1, proxy")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(hostnames) != 1 || hostnames[0] != "proxy" {
		t.Errorf("hostnames: got %v, want [\"proxy\"]", hostnames)
	}
	// Must contain at least the literal IP.
	gotLiteral := false
	for _, ip := range ips {
		if ip.String() == "172.17.0.1" {
			gotLiteral = true
			break
		}
	}
	if !gotLiteral {
		t.Errorf("ips: literal 172.17.0.1 missing from %v", ips)
	}
}

func TestIsValidHostname(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"proxy", true},
		{"reverse-proxy", true},
		{"sub.domain.example", true},
		{"a", true},
		{"", false},
		{"bad host", false},                                                          // space
		{"-leading-hyphen", false},                                                   // label starts with hyphen
		{"trailing-hyphen-", false},                                                  // label ends with hyphen
		{"double..dot", false},                                                       // empty label
		{".leading-dot", false},                                                      // empty first label
		{"trailing-dot.", false},                                                     // empty last label
		{"under_score", false},                                                       // underscore not allowed
		{strings.Repeat("a", 254), false},                                            // total length > 253
		{strings.Repeat("a", 64) + ".x", false},                                      // label > 63
	}
	for _, c := range cases {
		got := isValidHostname(c.in)
		if got != c.want {
			t.Errorf("isValidHostname(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseTrustedProxies_Empty(t *testing.T) {
	got, err := ParseTrustedProxies("")
	if err != nil {
		t.Fatalf("unexpected err on empty: %v", err)
	}
	if got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
		reason  string
	}{
		{"http://127.0.0.1/admin", true, "loopback literal"},
		{"http://192.168.1.1/admin", true, "RFC1918 literal"},
		{"http://169.254.169.254/latest/meta-data", true, "AWS metadata"},
		{"http://100.64.0.1/router", true, "CGN (Tailscale) literal blocked for SSRF"},
		{"http://[::ffff:127.0.0.1]/", true, "IPv4-mapped IPv6 loopback"},
		{"ftp://example.com", true, "unsupported scheme"},
		{"not-a-url", true, "malformed"},
		{"http://", true, "empty host"},
		{"https://discord.com/api/webhooks/xxx", false, "public discord"},
	}
	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			err := ValidateURL(c.url, nil)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateURL(%q) err=%v, wantErr=%v", c.url, err, c.wantErr)
			}
		})
	}
}

func TestValidateURL_DNSFailure(t *testing.T) {
	// A hostname that reliably does not resolve.
	err := ValidateURL("http://this-host-should-not-exist.invalid/", nil)
	if err == nil {
		t.Error("expected error on DNS failure (fail closed)")
	}
}

func TestValidateURL_Allowlist(t *testing.T) {
	err := ValidateURL("http://192.168.86.22:7878", nil)
	if err == nil {
		t.Error("expected error without allowlist")
	}
	allow := []net.IP{net.ParseIP("192.168.86.22")}
	err = ValidateURL("http://192.168.86.22:7878", allow)
	if err != nil {
		t.Errorf("expected no error with allowlist, got %v", err)
	}
}

func TestSafeHTTPClient_RejectsBlockedIP(t *testing.T) {
	c := NewSafeHTTPClient(5*time.Second, nil)
	_, err := c.Get("http://127.0.0.1:6060/anything")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSafeHTTPClient_RejectsIPv4MappedIPv6(t *testing.T) {
	c := NewSafeHTTPClient(5*time.Second, nil)
	_, err := c.Get("http://[::ffff:127.0.0.1]:6060/")
	if err == nil {
		t.Fatal("expected error on IPv4-mapped loopback")
	}
}

func TestSafeHTTPClient_AllowsPublicServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	srvHost := srv.Listener.Addr().(*net.TCPAddr).IP
	allow := []net.IP{srvHost}

	c := NewSafeHTTPClient(5*time.Second, allow)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatalf("expected no error with allowlist, got %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("got status %d, want 200", resp.StatusCode)
	}
}

func TestSecureEqual(t *testing.T) {
	if !SecureEqual("abc123", "abc123") {
		t.Error("equal strings should return true")
	}
	if SecureEqual("abc", "abd") {
		t.Error("different strings should return false")
	}
	if SecureEqual("abc", "abcd") {
		t.Error("different lengths should return false")
	}
	if !SecureEqual("", "") {
		t.Error("empty/empty should return true")
	}
}
