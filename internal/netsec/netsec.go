// Package netsec provides network-security helpers: IP classification for
// SSRF protection and local-address auth bypass, plus a safe HTTP client
// that re-validates destination IPs before connecting (DNS rebinding defense).
package netsec

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IsBlockedIP returns true for IP addresses that must not be reachable as
// outbound destinations: loopback, private ranges, link-local, IPv6 ULA,
// unspecified, multicast, documentation ranges, carrier-grade NAT, and
// cloud metadata.
//
// Callers that need to allow specific LAN targets (e.g. Arr instances on the
// user's home network) should maintain an allowlist and check against it
// BEFORE calling this function.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalise IPv4-mapped IPv6 (::ffff:a.b.c.d) to IPv4 so Is* checks work.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsUnspecified() {
		return true
	}
	if ip.IsMulticast() {
		return true
	}
	// IsPrivate covers RFC1918 (10/8, 172.16/12, 192.168/16) and IPv6 ULA (fc00::/7).
	if ip.IsPrivate() {
		return true
	}
	for _, cidr := range blockedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// IsLocalAddress returns true for addresses considered "local" for auth
// bypass purposes ("Disabled for Local Addresses" mode — matches
// Radarr/Sonarr semantics).
//
// This is deliberately slightly NARROWER than IsBlockedIP: it excludes
// carrier-grade NAT (100.64.0.0/10). CGN is used by Tailscale and some
// ISPs, and treating it as "local" would auto-bypass auth for anyone
// on the tailnet, which is usually not the intent. SSRF blocking still
// applies to CGN.
func IsLocalAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// RFC1918 + IPv6 ULA.
	if ip.IsPrivate() {
		return true
	}
	return false
}

var blockedCIDRs = mustParseCIDRs(
	"0.0.0.0/8",       // "This host on this network" — some stacks route to loopback
	"192.0.2.0/24",    // TEST-NET-1
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"240.0.0.0/4",     // Reserved (Class E)
	"255.255.255.255/32", // Limited broadcast
	"100.64.0.0/10",   // Carrier-grade NAT (Tailscale, ISP CGN)
	"64:ff9b::/96",    // NAT64 well-known prefix
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("netsec: invalid CIDR %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}

// ParseClientIP extracts the client IP from an http.Request using the
// RIGHTMOST-non-trusted algorithm. If trustedProxies is non-empty AND the
// direct peer is one of them, X-Forwarded-For is parsed right-to-left,
// trusted-proxy entries are skipped, and the first non-trusted IP is
// returned. This is the only safe way to honor XFF, because proxies APPEND
// to the header — the rightmost entry added by OUR trusted proxy is
// authoritative, while leftmost entries can be spoofed by the end client.
//
// If the direct peer is NOT a trusted proxy, XFF is ignored entirely and
// r.RemoteAddr is used. This means a client speaking directly to us cannot
// influence client-IP detection by setting XFF themselves.
//
// NOTE: operators should also ensure their reverse proxy is configured to
// either strip incoming XFF or to overwrite it with a single authoritative
// value (e.g. SWAG / nginx `proxy_set_header X-Forwarded-For $remote_addr`).
// The rightmost algorithm is a defense against misconfigured proxies, not
// a substitute for proxy hygiene.
func ParseClientIP(r *http.Request, trustedProxies []net.IP) net.IP {
	remoteIP := remoteAddrIP(r.RemoteAddr)
	if remoteIP == nil {
		return nil
	}
	if len(trustedProxies) == 0 || !containsIP(trustedProxies, remoteIP) {
		return remoteIP
	}
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteIP
	}
	// Walk right-to-left; first non-trusted IP is the real client.
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		piece := strings.TrimSpace(parts[i])
		ip := net.ParseIP(piece)
		if ip == nil {
			continue
		}
		if containsIP(trustedProxies, ip) {
			continue
		}
		return ip
	}
	// All entries were trusted proxies — fall back to remote addr.
	return remoteIP
}

func remoteAddrIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, p := range list {
		if p.Equal(ip) {
			return true
		}
	}
	return false
}

// ParseTrustedNetworks parses a comma-separated list of IP addresses
// and/or CIDR ranges into a slice of *net.IPNet. Single IPs are promoted
// to /32 (IPv4) or /128 (IPv6).
//
// Returns an error on any unparseable entry — config mistakes must be
// loud, not silently leave the admin with a different trust set than
// they thought they had.
func ParseTrustedNetworks(csv string) ([]*net.IPNet, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []*net.IPNet
	for _, raw := range strings.Split(csv, ",") {
		piece := strings.TrimSpace(raw)
		if piece == "" {
			continue
		}
		if _, ipnet, err := net.ParseCIDR(piece); err == nil {
			// Reject catastrophically-broad masks: "0.0.0.0/0" would trust
			// every IP on the internet in one typo. Enforce a sensible
			// minimum prefix length: /8 for IPv4 (at most one legacy A-class
			// block), /16 for IPv6. Anyone who actually needs a wider
			// bypass should pick Authentication Required = Enabled + API
			// keys for automation.
			ones, bits := ipnet.Mask.Size()
			minOnes := 8
			if bits == 128 { // IPv6
				minOnes = 16
			}
			if ones < minOnes {
				return nil, fmt.Errorf("trusted_networks entry %q is too broad (/%d); smallest allowed prefix is /%d (v4) or /16 (v6)", piece, ones, minOnes)
			}
			out = append(out, ipnet)
			continue
		}
		if ip := net.ParseIP(piece); ip != nil {
			var mask net.IPMask
			if v4 := ip.To4(); v4 != nil {
				ip = v4
				mask = net.CIDRMask(32, 32)
			} else {
				mask = net.CIDRMask(128, 128)
			}
			out = append(out, &net.IPNet{IP: ip, Mask: mask})
			continue
		}
		return nil, fmt.Errorf("invalid entry in trusted_networks: %q (expected IP or CIDR like 192.168.0.0/24)", piece)
	}
	return out, nil
}

// ParseTrustedProxies parses a comma-separated list of IP addresses.
// Returns an error on any invalid entry — misconfiguration should be loud,
// not silently produce an empty list (which would disable XFF parsing and
// break reverse-proxy deployments).
func ParseTrustedProxies(csv string) ([]net.IP, error) {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil, nil
	}
	var out []net.IP
	for _, piece := range strings.Split(csv, ",") {
		piece = strings.TrimSpace(piece)
		if piece == "" {
			continue
		}
		ip := net.ParseIP(piece)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP in trusted_proxies: %q", piece)
		}
		out = append(out, ip)
	}
	return out, nil
}

// ErrBlockedDestination is returned by SafeHTTPClient when the resolved
// destination IP is in the blocked set.
var ErrBlockedDestination = errors.New("destination IP is blocked (SSRF protection)")

// ErrDNSResolutionFailed is returned by ValidateURL when DNS lookup fails
// and the caller requested strict validation.
var ErrDNSResolutionFailed = errors.New("DNS resolution failed")

// NewSafeHTTPClient returns an http.Client that resolves the destination
// hostname AND checks every resolved IP against IsBlockedIP before dialing.
// This protects against DNS rebinding: even if the URL was validated at
// save-time, a malicious DNS server could return a different IP at request
// time. Re-validating per-request closes that gap.
//
// Non-nil allowlist entries (e.g. the user's LAN Arr instance IPs) are
// permitted even if IsBlockedIP would normally reject them.
//
// The returned client explicitly disables HTTP proxy discovery (Proxy: nil)
// so that an attacker who sets HTTP_PROXY in the environment cannot route
// our requests through an untrusted proxy that bypasses IP validation.
//
// Keep-alive is disabled to prevent IP pinning: each request re-resolves,
// closing the window for DNS-rebinding on a pooled connection.
func NewSafeHTTPClient(timeout time.Duration, allowlist []net.IP) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	transport := &http.Transport{
		Proxy: nil, // explicit: do not honor HTTP_PROXY
		// TLS ≥1.2 enforced explicitly (not relying on Go-version defaults).
		// Registry endpoints and notification APIs all support 1.2+.
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// Literal IP: skip DNS, validate directly.
			if ip := net.ParseIP(host); ip != nil {
				if containsIP(allowlist, ip) {
					return dialer.DialContext(ctx, network, addr)
				}
				if IsBlockedIP(ip) {
					return nil, fmt.Errorf("%w: %s", ErrBlockedDestination, ip)
				}
				return dialer.DialContext(ctx, network, addr)
			}
			// Hostname: resolve, then pick the first IP that passes — either
			// allowlisted or not in the block set. Dial that specific IP to
			// prevent resolver-level TOCTOU.
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if containsIP(allowlist, ip.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				}
				if !IsBlockedIP(ip.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("%w: no permitted IPs for %s", ErrBlockedDestination, host)
		},
		MaxIdleConns:        0,
		DisableKeepAlives:   true, // each request re-resolves; no IP pinning
		IdleConnTimeout:     0,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

// ValidateURL is a convenience checker for use when an app accepts a URL
// from the user at save-time (e.g. webhook URL). Returns an error if the
// URL is malformed, uses an unsupported scheme, resolves to a blocked
// destination, or fails DNS resolution. Actual HTTP calls should still
// use NewSafeHTTPClient (defense in depth against DNS rebinding).
//
// If the hostname resolves to a mix of allowed and blocked IPs, the URL
// is rejected: we can't know which IP the runtime dialer will pick and
// must fail closed. The one exception is when ANY resolved IP is in the
// allowlist — then we trust the user's explicit intent.
func ValidateURL(rawURL string, allowlist []net.IP) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("missing hostname")
	}
	if ip := net.ParseIP(host); ip != nil {
		if containsIP(allowlist, ip) {
			return nil
		}
		if IsBlockedIP(ip) {
			return fmt.Errorf("%w: %s", ErrBlockedDestination, ip)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: %s (%v)", ErrDNSResolutionFailed, host, err)
	}
	// Fast path: any resolved IP in allowlist → user explicitly trusts this host.
	for _, ip := range ips {
		if containsIP(allowlist, ip.IP) {
			return nil
		}
	}
	// Otherwise every IP must pass the block check.
	for _, ip := range ips {
		if IsBlockedIP(ip.IP) {
			return fmt.Errorf("%w: %s resolves to blocked IP %s", ErrBlockedDestination, host, ip.IP)
		}
	}
	return nil
}

// SecureEqual compares two strings in constant time. Exported for callers
// outside this package that need to compare short secrets (API keys, tokens).
func SecureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
