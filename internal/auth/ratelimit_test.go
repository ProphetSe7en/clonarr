package auth

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestRateLimit_AllowUpToMax verifies the token bucket lets through up to
// maxAttempts requests within the window before tripping. The fifth
// attempt (with maxAttempts=5) must succeed; the sixth must be blocked.
func TestRateLimit_AllowUpToMax(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 5, window: time.Minute})
	ip := "192.0.2.10"

	for i := 1; i <= 5; i++ {
		ok, retryAfter := rl.allow(ip)
		if !ok {
			t.Fatalf("attempt %d: expected allowed, got blocked (retryAfter=%v)", i, retryAfter)
		}
		if retryAfter != 0 {
			t.Errorf("attempt %d: retryAfter should be 0 when allowed, got %v", i, retryAfter)
		}
	}

	ok, retryAfter := rl.allow(ip)
	if ok {
		t.Fatal("attempt 6: expected blocked, got allowed")
	}
	if retryAfter <= 0 {
		t.Errorf("attempt 6: retryAfter should be positive, got %v", retryAfter)
	}
	if retryAfter > time.Minute {
		t.Errorf("attempt 6: retryAfter %v exceeds window %v", retryAfter, time.Minute)
	}
}

// TestRateLimit_WindowExpires confirms that once the oldest attempt
// falls outside the window, a new attempt is allowed again. Uses a
// short window so the test runs in subseconds.
func TestRateLimit_WindowExpires(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 2, window: 50 * time.Millisecond})
	ip := "192.0.2.20"

	// Fill the bucket
	rl.allow(ip)
	rl.allow(ip)
	if ok, _ := rl.allow(ip); ok {
		t.Fatal("third attempt should be blocked")
	}

	// Wait for the window to slide past the first two attempts
	time.Sleep(60 * time.Millisecond)

	if ok, _ := rl.allow(ip); !ok {
		t.Fatal("after window expires, attempt should be allowed again")
	}
}

// TestRateLimit_IndependentIPs ensures one IP exhausting its bucket
// does not affect a different IP. Critical: a single attacker must not
// be able to lock out other users.
func TestRateLimit_IndependentIPs(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 3, window: time.Minute})
	attacker := "203.0.113.99"
	victim := "203.0.113.10"

	// Attacker exhausts bucket
	for i := 0; i < 3; i++ {
		rl.allow(attacker)
	}
	if ok, _ := rl.allow(attacker); ok {
		t.Fatal("attacker bucket should be full")
	}

	// Victim's first attempt must still be allowed
	if ok, _ := rl.allow(victim); !ok {
		t.Fatal("victim must not be affected by attacker's exhausted bucket")
	}
}

// TestRateLimit_RetryAfterCountdown checks that the returned retry-after
// duration shrinks as time passes (it's the gap until the OLDEST
// in-window attempt expires, not a fixed cooldown).
func TestRateLimit_RetryAfterCountdown(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 1, window: 100 * time.Millisecond})
	ip := "192.0.2.30"

	rl.allow(ip) // bucket full at maxAttempts=1
	_, first := rl.allow(ip)
	time.Sleep(40 * time.Millisecond)
	_, second := rl.allow(ip)

	if second >= first {
		t.Errorf("retryAfter should shrink: first=%v, second (after 40ms)=%v", first, second)
	}
	if second <= 0 {
		t.Errorf("retryAfter still positive but small expected, got %v", second)
	}
}

// TestRateLimit_PrunesExpiredEntries verifies internal pruning so the
// bucket doesn't grow unbounded for a single IP that legitimately
// retries over a long period.
func TestRateLimit_PrunesExpiredEntries(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 5, window: 30 * time.Millisecond})
	ip := "192.0.2.40"

	// 3 attempts, then wait for window
	rl.allow(ip)
	rl.allow(ip)
	rl.allow(ip)
	time.Sleep(40 * time.Millisecond)

	// Next allow() should prune the 3 expired entries internally
	rl.allow(ip)

	rl.mu.Lock()
	count := len(rl.attempts[ip])
	rl.mu.Unlock()
	if count != 1 {
		t.Errorf("after window expiry + 1 new attempt, expected 1 entry, got %d", count)
	}
}

// TestRateLimit_ConcurrentAccess hammers allow() from multiple
// goroutines on the same IP to verify mutex correctness — no race,
// no count corruption, total allows == maxAttempts (rest blocked).
func TestRateLimit_ConcurrentAccess(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 10, window: time.Minute})
	ip := "192.0.2.50"

	const goroutines = 50
	allowed := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := rl.allow(ip); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed != 10 {
		t.Errorf("expected exactly 10 allowed (maxAttempts), got %d", allowed)
	}
}

// TestAuthRateLimitMiddleware_429OnExhaustion exercises the full HTTP
// middleware: 5 in-window POSTs from the same RemoteAddr succeed, 6th
// returns 429 with a Retry-After header.
func TestAuthRateLimitMiddleware_429OnExhaustion(t *testing.T) {
	store := &Store{cfg: Config{}} // empty TrustedProxies → falls back to RemoteAddr
	mw := AuthRateLimitMiddleware(store)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 1; i <= 5; i++ {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = "192.0.2.60:54321"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: expected 200, got %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "192.0.2.60:54322"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("attempt 6: expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Error("attempt 6: missing Retry-After header")
	}
}

// TestItoa is a minor sanity check on the strconv-free formatter used
// for Retry-After. Verifies positive, zero, and a representative
// multi-digit value.
func TestItoa(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{900, "900"},
		{60, "60"},
	}
	for _, c := range cases {
		got := itoa(c.n)
		if got != c.want {
			t.Errorf("itoa(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestRateLimit_Sweep verifies the GC goroutine drops bucket entries
// for IPs whose newest attempt is older than 10 windows. Without this,
// one-shot scanner traffic would accumulate indefinitely.
func TestRateLimit_Sweep(t *testing.T) {
	rl := newRateLimiter(rateLimitConfig{maxAttempts: 5, window: 30 * time.Millisecond})

	// Insert 3 IPs, then directly age them out via the lock + manual
	// timestamp manipulation. (The sweep ticker is bounded at 1 minute
	// so we can't wait for a real sweep cycle in a unit test — instead
	// we validate sweep() in isolation.)
	rl.allow("192.0.2.100")
	rl.allow("192.0.2.101")
	rl.allow("192.0.2.102")

	rl.mu.Lock()
	if len(rl.attempts) != 3 {
		t.Fatalf("expected 3 buckets, got %d", len(rl.attempts))
	}
	// Backdate two IPs to be sweep-eligible (>10*window old)
	old := time.Now().Add(-15 * 30 * time.Millisecond)
	rl.attempts["192.0.2.100"] = []time.Time{old}
	rl.attempts["192.0.2.101"] = []time.Time{old}
	rl.mu.Unlock()

	rl.sweep()

	rl.mu.Lock()
	defer rl.mu.Unlock()
	if len(rl.attempts) != 1 {
		t.Errorf("after sweep, expected 1 surviving bucket, got %d (keys: %v)",
			len(rl.attempts), keysOf(rl.attempts))
	}
	if _, ok := rl.attempts["192.0.2.102"]; !ok {
		t.Error("expected fresh bucket 192.0.2.102 to survive sweep")
	}
}

// TestRateLimit_DegenerateConfig — defense in depth. Production code
// hardcodes authRateLimit = {5, 1min}, but a future change might tune
// it. Verify the limiter doesn't crash or misbehave on zero values
// (would be a footgun if anyone tunes via env-var later).
func TestRateLimit_DegenerateConfig(t *testing.T) {
	t.Run("window zero allows everything", func(t *testing.T) {
		rl := newRateLimiter(rateLimitConfig{maxAttempts: 5, window: 0})
		// With window=0, cutoff == now → every prior entry is "expired"
		// → kept stays empty → every attempt allowed.
		for i := 0; i < 20; i++ {
			if ok, _ := rl.allow("192.0.2.200"); !ok {
				t.Errorf("attempt %d: window=0 should allow everything, got blocked", i)
				break
			}
		}
	})
	t.Run("maxAttempts zero blocks first attempt safely", func(t *testing.T) {
		rl := newRateLimiter(rateLimitConfig{maxAttempts: 0, window: time.Minute})
		// First attempt: kept is empty, len(kept) >= 0 is true → block
		// path. Must not panic on the kept[0] access in retryAfter calc.
		// Note: this is degenerate config; defensive behavior expected.
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("maxAttempts=0 panicked: %v (should block defensively, not crash)", r)
			}
		}()
		ok, _ := rl.allow("192.0.2.201")
		if ok {
			t.Error("maxAttempts=0 should block all attempts (defensive degenerate behavior)")
		}
	})
}

// TestClientIPForRateLimit_TrustedProxy verifies the X-Forwarded-For
// path is honored when TrustedProxies is configured. Without this,
// reverse-proxy deployments would rate-limit all real clients into one
// bucket keyed on the proxy's IP.
func TestClientIPForRateLimit_TrustedProxy(t *testing.T) {
	// Configure a single trusted proxy at 10.0.0.1
	store := &Store{cfg: Config{
		TrustedProxies: []net.IP{net.ParseIP("10.0.0.1")},
	}}

	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "10.0.0.1:54321"               // proxy is the direct hop
	req.Header.Set("X-Forwarded-For", "203.0.113.45") // real client

	got := clientIPForRateLimit(req, store)
	if got != "203.0.113.45" {
		t.Errorf("with trusted proxy + XFF, expected real-client IP 203.0.113.45, got %q", got)
	}
}

// TestClientIPForRateLimit_NoXFF — when XFF is absent and trusted proxy
// is configured, falls back to RemoteAddr. Defensive against header
// stripping or non-proxy direct connections.
func TestClientIPForRateLimit_NoXFF(t *testing.T) {
	store := &Store{cfg: Config{
		TrustedProxies: []net.IP{net.ParseIP("10.0.0.1")},
	}}

	req := httptest.NewRequest("POST", "/login", nil)
	req.RemoteAddr = "10.0.0.5:54321" // direct connection, no XFF

	got := clientIPForRateLimit(req, store)
	if got != "10.0.0.5" {
		t.Errorf("no XFF + trusted proxy config: expected RemoteAddr-derived 10.0.0.5, got %q", got)
	}
}

// keysOf is a test-only helper to format map keys for error messages.
func keysOf(m map[string][]time.Time) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
