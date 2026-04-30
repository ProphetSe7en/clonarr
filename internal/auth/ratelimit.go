package auth

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"clonarr/internal/netsec"
)

// ratelimit.go — in-memory per-IP token bucket for auth-hot endpoints
// (login, setup, password change, auth-mode transition). Stops brute-
// force on credentials without an external Redis dependency.
//
// Threat model: attacker with a list of usernames/passwords pounds
// /login or /api/auth/change-password from a single IP (or a small set
// of rotating IPs). Token bucket caps the per-IP rate so a single
// source can't try unbounded combinations. Doesn't address distributed
// brute-force from a botnet — the homelab threat model assumes that's
// out of scope.
//
// Design choices:
//   - Per-IP bucket (not per-username). Avoids username-enumeration via
//     differential rate-limit response timing.
//   - In-memory, no persistence. Container restart resets buckets;
//     attacker would have to wait between restarts to retry, so this
//     isn't a vulnerability — restart is a costly action for them.
//   - Trusted-proxy aware via X-Forwarded-For when configured. Falls
//     back to RemoteAddr when no proxy configured.
//   - Conservative defaults: 5 attempts per minute per IP. Failed
//     attempt costs 1 token; successful attempt also costs 1 (so a
//     password-typo flood doesn't open a window for brute-force after
//     the user gets in).

// rateLimitConfig pins the per-window cap + window duration. v1 uses
// the same shape for every protected endpoint; could split later if
// we want different limits per endpoint.
type rateLimitConfig struct {
	maxAttempts int
	window      time.Duration
}

// authRateLimit — 5 attempts per minute per IP. Generous enough for
// fat-finger password retries; tight enough that a brute-force script
// can't try a wordlist in any practical time.
var authRateLimit = rateLimitConfig{maxAttempts: 5, window: time.Minute}

// rateLimiter is the in-memory state. attempts tracks each IP's recent
// timestamps within the window; older entries are pruned on each
// check. A background sweep goroutine drops stale buckets entirely so
// the map doesn't grow unbounded under sustained scan traffic — see
// startSweep() for the GC strategy.
type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	cfg      rateLimitConfig
}

func newRateLimiter(cfg rateLimitConfig) *rateLimiter {
	rl := &rateLimiter{
		attempts: make(map[string][]time.Time),
		cfg:      cfg,
	}
	rl.startSweep()
	return rl
}

// sweepInterval determines how often the GC goroutine runs. Tied to
// the configured window — sweeping more often than once per window
// is wasteful (no entries can have aged out yet between sweeps).
// Bounded below at 1 minute so a degenerate cfg.window (test-only)
// doesn't spin a goroutine in a tight loop.
func (rl *rateLimiter) sweepInterval() time.Duration {
	if rl.cfg.window < time.Minute {
		return time.Minute
	}
	return rl.cfg.window
}

// startSweep launches a background goroutine that periodically drops
// IPs whose newest attempt is older than 10 windows. Bounds map growth
// under one-shot scanner traffic where each IP connects once and never
// returns. Goroutine has no shutdown hook — it runs for process
// lifetime, which is correct since the rateLimiter itself does too.
func (rl *rateLimiter) startSweep() {
	go func() {
		ticker := time.NewTicker(rl.sweepInterval())
		defer ticker.Stop()
		for range ticker.C {
			// Recover INSIDE the loop body so a single panic in
			// rl.sweep() doesn't kill the goroutine permanently —
			// otherwise GC stops and the map grows unbounded under
			// scanner traffic for the rest of process lifetime.
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("ratelimit sweep panic recovered: %v", r)
					}
				}()
				rl.sweep()
			}()
		}
	}()
}

// sweep drops bucket entries for IPs whose newest entry is older than
// 10 * window. Holds the lock for the full sweep — the lock window is
// short because the scan is over the map, not nested loops over per-IP
// slices. For maps with millions of entries this could be an issue;
// homelab-scale (thousands max) doesn't care.
func (rl *rateLimiter) sweep() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-10 * rl.cfg.window)
	for ip, ts := range rl.attempts {
		if len(ts) == 0 {
			delete(rl.attempts, ip)
			continue
		}
		// ts is in insertion order; the newest is at the end.
		if ts[len(ts)-1].Before(cutoff) {
			delete(rl.attempts, ip)
		}
	}
}

// allow checks whether the given IP can make another attempt right now.
// Records the attempt internally on the way through, so callers don't
// double-count. Returns (true, 0) when the attempt is permitted;
// (false, retryAfter) when the bucket is full — retryAfter is how long
// until the oldest in-window attempt expires.
func (rl *rateLimiter) allow(ip string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.cfg.window)
	// Prune expired attempts.
	kept := rl.attempts[ip][:0]
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.cfg.maxAttempts {
		// Bucket full — oldest attempt sets the retry-after window.
		// Defensive against degenerate config (maxAttempts=0): when kept
		// is empty, we'd panic on kept[0]. Return a sane retryAfter and
		// don't crash the goroutine.
		if len(kept) == 0 {
			rl.attempts[ip] = kept
			return false, rl.cfg.window
		}
		retryAfter := kept[0].Add(rl.cfg.window).Sub(now)
		rl.attempts[ip] = kept
		return false, retryAfter
	}
	kept = append(kept, now)
	rl.attempts[ip] = kept
	return true, 0
}

// AuthRateLimitMiddleware wraps an http.Handler with per-IP rate limiting
// using the authRateLimit config. Apply to login / setup / change-password
// / change-auth-mode endpoints. Returns 429 Too Many Requests with a
// Retry-After header on bucket exhaustion.
//
// IP resolution: uses Trusted Proxies when configured (the X-Forwarded-For
// chain's last untrusted hop); falls back to RemoteAddr. Prevents
// reverse-proxy deployments from rate-limiting all clients to one
// pool keyed on the proxy's own IP.
func AuthRateLimitMiddleware(store *Store) func(http.Handler) http.Handler {
	rl := newRateLimiter(authRateLimit)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIPForRateLimit(r, store)
			if ok, retryAfter := rl.allow(ip); !ok {
				secs := int(retryAfter.Seconds())
				if secs < 1 {
					secs = 1
				}
				w.Header().Set("Retry-After", itoa(secs))
				http.Error(w, "rate limit exceeded — try again later", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIPForRateLimit returns the client IP suitable for rate-limiting.
// Honors the trusted-proxy chain when configured (via netsec.ParseClientIP
// which walks X-Forwarded-For with the configured trusted-proxies allowlist);
// otherwise uses RemoteAddr host-only (strips port). Defensive: if
// X-Forwarded-For is malformed or contains an untrusted last hop, falls
// through to RemoteAddr rather than rate-limiting based on a controllable
// header.
func clientIPForRateLimit(r *http.Request, store *Store) string {
	if store != nil {
		cfg := store.Config()
		if ip := netsec.ParseClientIP(r, cfg.TrustedProxies); ip != nil {
			return ip.String()
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// itoa is a tiny strconv-free integer formatter used only by the
// Retry-After header. Avoids pulling strconv into auth/ for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	pos := len(b)
	for n > 0 {
		pos--
		b[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return strings.Clone(string(b[pos:]))
}
