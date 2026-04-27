package auth

import (
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	return Config{
		Mode:         ModeForms,
		Requirement:  RequireExtLocal,
		SessionTTL:   time.Hour,
		AuthFilePath: filepath.Join(dir, "auth.json"),
		MaxSessions:  100,
	}
}

func TestSetupAndLoad(t *testing.T) {
	s := NewStore(testConfig(t))

	exists, err := s.Load()
	if err != nil {
		t.Fatalf("Load fresh: %v", err)
	}
	if exists {
		t.Fatal("Load fresh: should return exists=false")
	}
	if s.IsConfigured() {
		t.Fatal("fresh store should not be configured")
	}

	if err := s.Setup("admin", "SecretPassw0rd"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !s.IsConfigured() {
		t.Fatal("after Setup, should be configured")
	}

	s2 := NewStore(s.cfg)
	exists, err = s2.Load()
	if err != nil {
		t.Fatalf("Load persisted: %v", err)
	}
	if !exists {
		t.Fatal("persisted store: exists should be true")
	}
	if !s2.VerifyPassword("admin", "SecretPassw0rd") {
		t.Error("VerifyPassword: should accept correct pw")
	}
	if s2.VerifyPassword("admin", "wrong") {
		t.Error("VerifyPassword: should reject wrong pw")
	}
	if s2.VerifyPassword("notexist", "anything") {
		t.Error("VerifyPassword: should reject unknown username")
	}
}

func TestSetupRejectsWeakPassword(t *testing.T) {
	s := NewStore(testConfig(t))
	cases := []string{
		"short",
		"abcdefghij",
		"ABCDEFGHIJ",
		"1234567890",
	}
	for _, pw := range cases {
		err := s.Setup("admin", pw)
		if err == nil {
			t.Errorf("Setup with weak pw %q: expected error", pw)
		}
	}
}

func TestSetupAcceptsStrongPassword(t *testing.T) {
	cases := []string{
		"Password12",
		"MyS3cretPass!",
		"abcdefghij1",
	}
	for _, pw := range cases {
		s := NewStore(testConfig(t))
		if err := s.Setup("admin", pw); err != nil {
			t.Errorf("Setup with valid pw %q: %v", pw, err)
		}
	}
}

func TestSetupUsernameValidation(t *testing.T) {
	bads := map[string]string{
		"empty":          "",
		"whitespace":     "   ",
		"leading_space":  " admin",
		"trailing_space": "admin ",
		"control_char":   "admin\x00",
		"newline":        "admin\nname",
		"too_long":       strings.Repeat("a", 65),
	}
	for name, u := range bads {
		t.Run(name, func(t *testing.T) {
			s := NewStore(testConfig(t))
			if err := s.Setup(u, "Password12"); err == nil {
				t.Errorf("Setup(%q,...) should fail", u)
			}
		})
	}

	// Accept a 64-char username (exact boundary).
	s := NewStore(testConfig(t))
	if err := s.Setup(strings.Repeat("a", 64), "Password12"); err != nil {
		t.Errorf("64-char username should be accepted: %v", err)
	}
}

func TestSetupTwiceFails(t *testing.T) {
	s := NewStore(testConfig(t))
	if err := s.Setup("admin", "SecretPassw0rd"); err != nil {
		t.Fatal(err)
	}
	if err := s.Setup("admin2", "AnotherPass1"); err == nil {
		t.Error("second Setup should fail")
	}
}

func TestLoad_MalformedFileFailsLoud(t *testing.T) {
	cfg := testConfig(t)
	// Write garbage to auth.json — attacker-corrupted state.
	if err := os.WriteFile(cfg.AuthFilePath, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(cfg)
	_, err := s.Load()
	if err == nil {
		t.Error("Load on malformed file should return error (fail loud, no silent reset)")
	}
}

func TestLoad_MissingFieldsFailsLoud(t *testing.T) {
	cfg := testConfig(t)
	// Structurally valid JSON but missing username field.
	if err := os.WriteFile(cfg.AuthFilePath, []byte(`{"password_hash":"x","api_key":"y"}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(cfg)
	_, err := s.Load()
	if err == nil {
		t.Error("Load on partial file should return error")
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")

	id, err := s.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	if !s.ValidateSession(id) {
		t.Error("new session should validate")
	}
	if len(id) != 64 {
		t.Errorf("session ID hex length = %d, want 64 (256 bits)", len(id))
	}
	s.DeleteSession(id)
	if s.ValidateSession(id) {
		t.Error("deleted session should not validate")
	}
}

func TestSessionExpiry(t *testing.T) {
	cfg := testConfig(t)
	cfg.SessionTTL = 10 * time.Millisecond
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")

	id, _ := s.CreateSession()
	if !s.ValidateSession(id) {
		t.Error("fresh session should validate")
	}
	time.Sleep(20 * time.Millisecond)
	if s.ValidateSession(id) {
		t.Error("expired session should not validate")
	}
	s.CleanupExpiredSessions()
	s.mu.RLock()
	_, stillThere := s.sessions[id]
	s.mu.RUnlock()
	if stillThere {
		t.Error("CleanupExpiredSessions should drop expired entries")
	}
}

func TestSessionCapEviction(t *testing.T) {
	cfg := testConfig(t)
	cfg.MaxSessions = 3
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")

	var ids []string
	for i := 0; i < 5; i++ {
		id, err := s.CreateSession()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		time.Sleep(2 * time.Millisecond) // ensure created times differ
	}
	s.mu.RLock()
	total := len(s.sessions)
	s.mu.RUnlock()
	if total > cfg.MaxSessions {
		t.Errorf("session count = %d, want <= %d", total, cfg.MaxSessions)
	}
	// Oldest two should have been evicted.
	if s.ValidateSession(ids[0]) {
		t.Error("oldest session should have been evicted")
	}
	if s.ValidateSession(ids[1]) {
		t.Error("second-oldest session should have been evicted")
	}
	if !s.ValidateSession(ids[4]) {
		t.Error("newest session should still be valid")
	}
}

func TestSession_ConcurrentCreate(t *testing.T) {
	// Race-check: concurrent CreateSession should not deadlock or panic.
	// Run with `go test -race` to actually catch races.
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				id, err := s.CreateSession()
				if err != nil {
					t.Errorf("CreateSession: %v", err)
					return
				}
				s.ValidateSession(id)
			}
		}()
	}
	wg.Wait()
}

func TestAPIKey(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")

	k1 := s.APIKey()
	if len(k1) != 32 {
		t.Errorf("APIKey length = %d, want 32", len(k1))
	}
	if !s.VerifyAPIKey(k1) {
		t.Error("original key should verify")
	}
	if s.VerifyAPIKey("wrongkey") {
		t.Error("wrong key should not verify")
	}

	k2, err := s.RegenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Error("regenerated key should differ")
	}
	if s.VerifyAPIKey(k1) {
		t.Error("old key should no longer verify after regenerate")
	}
	if !s.VerifyAPIKey(k2) {
		t.Error("new key should verify")
	}
}

func TestIsPublic(t *testing.T) {
	cases := []struct {
		path     string
		expected bool
		reason   string
	}{
		// Exact matches
		{"/login", true, "exact login"},
		{"/logout", true, "exact logout"},
		{"/setup", true, "exact setup"},
		{"/api/auth/status", true, "exact auth status"},
		// Sub-paths under documented prefixes
		{"/static/main.js", true, "static file"},
		{"/css/main.css", true, "css file"},
		{"/js/api.js", true, "js file"},
		{"/icons/icon.png", true, "icon file"},
		{"/setup/step2", true, "setup substep"},
		// Must NOT be public — catches #1 bypass
		{"/loginzzz", false, "loginzzz must NOT match /login prefix"},
		{"/login-hack", false, "login-hack must NOT match"},
		{"/logout-backdoor", false, "logout-backdoor must NOT match"},
		{"/setup2", false, "setup2 must NOT match"},
		{"/setupbackdoor", false, "setupbackdoor must NOT match"},
		{"/api/auth/status-extra", false, "status-extra must NOT match"},
		{"/static", false, "bare /static (no slash) must NOT match"},
		// Normal private paths
		{"/", false, "root"},
		{"/api/containers", false, "api"},
		{"/api/config", false, "api config"},
	}
	for _, c := range cases {
		t.Run(c.reason, func(t *testing.T) {
			if IsPublic(c.path) != c.expected {
				t.Errorf("IsPublic(%q) = %v, want %v", c.path, !c.expected, c.expected)
			}
		})
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
}

func TestMiddleware_PublicPasses(t *testing.T) {
	s := NewStore(testConfig(t))
	h := s.Middleware(okHandler())
	for _, path := range []string{"/login", "/setup", "/static/main.js"} {
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "8.8.8.8:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("public %s: code %d, want 200", path, w.Code)
		}
	}
}

func TestMiddleware_UnconfiguredRedirectsToSetup(t *testing.T) {
	s := NewStore(testConfig(t))
	h := s.Middleware(okHandler())
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.86.10:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 302 {
		t.Errorf("unconfigured: code %d, want 302", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/setup" {
		t.Errorf("unconfigured redirect: %q, want /setup", loc)
	}
}

func TestMiddleware_LocalAddressBypass(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "192.168.86.10:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("LAN bypass: code %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 302 {
		t.Errorf("public IP: code %d, want 302 (redirect)", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("public IP API: code %d, want 401", w.Code)
	}
}

func TestMiddleware_CGNNotLocal(t *testing.T) {
	// Carrier-grade NAT (100.64/10, Tailscale range) — explicitly NOT auth-bypassed.
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "100.64.0.5:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("CGN (Tailscale) IP: code %d, want 401 (not bypassed)", w.Code)
	}
}

func TestMiddleware_RequireAllDisallowsLocal(t *testing.T) {
	cfg := testConfig(t)
	cfg.Requirement = RequireAll
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "192.168.86.10:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("RequireAll LAN: code %d, want 401", w.Code)
	}
}

func TestMiddleware_APIKeyBypass(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")
	key := s.APIKey()
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Api-Key", key)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("API key header: code %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/containers?apikey="+key, nil)
	req.RemoteAddr = "8.8.8.8:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("API key query: code %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-Api-Key", "wrong")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("wrong API key: code %d, want 401", w.Code)
	}
}

func TestMiddleware_SessionAuth(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	id, _ := s.CreateSession()

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: id})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("valid session: code %d, want 200", w.Code)
	}
}

func TestMiddleware_ExpiredCookieCleared(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "expired-or-bogus"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	setCookieHeader := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookieHeader, SessionCookieName+"=;") {
		t.Errorf("expected Set-Cookie clearing session, got: %q", setCookieHeader)
	}
	if !strings.Contains(setCookieHeader, "Max-Age=0") {
		t.Errorf("expected Max-Age=0, got: %q", setCookieHeader)
	}
}

func TestMiddleware_BasicAuth(t *testing.T) {
	cfg := testConfig(t)
	cfg.Mode = ModeBasic
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.SetBasicAuth("admin", "Password12")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("Basic good: code %d, want 200", w.Code)
	}

	req = httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.SetBasicAuth("admin", "wrong")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("Basic bad: code %d, want 401", w.Code)
	}
	if w.Header().Get("WWW-Authenticate") == "" {
		t.Error("Basic bad: missing WWW-Authenticate header")
	}
}

func TestMiddleware_NoneMode(t *testing.T) {
	cfg := testConfig(t)
	cfg.Mode = ModeNone
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("ModeNone: code %d, want 200", w.Code)
	}
}

func TestValidateConfig_NoneAccepted(t *testing.T) {
	// The env-var gate was removed — Radarr-parity UX puts the guardrails
	// in the UI (type-to-confirm modal + persistent red banner + 60s log
	// warning). ValidateConfig should accept ModeNone unconditionally.
	cfg := DefaultConfig()
	cfg.Mode = ModeNone
	os.Unsetenv("I_UNDERSTAND_AUTH_IS_DISABLED")
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("ModeNone should now be accepted: %v", err)
	}
}

func TestValidateConfig_OtherModesAlwaysValid(t *testing.T) {
	for _, m := range []AuthMode{ModeForms, ModeBasic} {
		cfg := DefaultConfig()
		cfg.Mode = m
		if err := ValidateConfig(cfg); err != nil {
			t.Errorf("mode %q: %v", m, err)
		}
	}
}

func TestValidateConfig_RejectsUnknownMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Mode = "forMs" // typo — wrong case
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of unknown mode (typo)")
	}
	cfg.Mode = "garbage"
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of unknown mode")
	}
}

func TestValidateConfig_RejectsUnknownRequirement(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Requirement = "yes" // not a valid enum value
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of unknown requirement")
	}
	cfg.Requirement = "disabled" // the old Radarr name, not our enum
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of non-enum requirement")
	}
}

func TestValidateConfig_RejectsExtremeTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SessionTTL = 0
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of zero TTL")
	}
	cfg.SessionTTL = 400 * 24 * time.Hour
	if err := ValidateConfig(cfg); err == nil {
		t.Error("expected rejection of TTL > 365 days (overflow risk)")
	}
}

func TestIsRequestFromTrustedProxy(t *testing.T) {
	cfg := testConfig(t)
	cfg.TrustedProxies = []net.IP{net.ParseIP("172.17.0.1")}
	s := NewStore(cfg)

	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:5555"
	if !s.IsRequestFromTrustedProxy(r) {
		t.Error("expected true for trusted-proxy peer")
	}

	r.RemoteAddr = "8.8.8.8:5555"
	if s.IsRequestFromTrustedProxy(r) {
		t.Error("expected false for untrusted peer")
	}

	// Empty trusted list → never trust
	s2 := NewStore(testConfig(t))
	if s2.IsRequestFromTrustedProxy(r) {
		t.Error("expected false when trusted list empty")
	}
}

func TestTrustedProxy_SpoofedLeftmost(t *testing.T) {
	// Classic XFF spoof: client sets XFF=127.0.0.1 in their outgoing request,
	// trusted proxy APPENDS their real IP (8.8.8.8). A leftmost-parser would
	// read 127.0.0.1 → auth bypass. Rightmost reads 8.8.8.8 → blocked.
	cfg := testConfig(t)
	cfg.TrustedProxies = []net.IP{net.ParseIP("172.17.0.1")}
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "172.17.0.1:54321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 8.8.8.8")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Errorf("XFF leftmost spoof: code %d, want 401 (not bypassed)", w.Code)
	}
}

func TestTrustedProxy_LegitimateLocal(t *testing.T) {
	// Legitimate: LAN user → trusted proxy → app. XFF is just "192.168.86.10".
	cfg := testConfig(t)
	cfg.TrustedProxies = []net.IP{net.ParseIP("172.17.0.1")}
	s := NewStore(cfg)
	_ = s.Setup("admin", "Password12")
	h := s.Middleware(okHandler())

	req := httptest.NewRequest("GET", "/api/containers", nil)
	req.RemoteAddr = "172.17.0.1:54321"
	req.Header.Set("X-Forwarded-For", "192.168.86.10")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("legitimate LAN via trusted proxy: code %d, want 200", w.Code)
	}
}

// TestVerifyPassword_TimingEqualization confirms that the unknown-user path
// and the known-user-wrong-password path both take similar time (both go
// through bcrypt). Precise timing is flaky in CI, but we verify both
// paths take at least 100ms (real bcrypt, not an instant format-error
// return) which is the signal that the dummy hash is valid bcrypt format.
func TestVerifyPassword_TimingEqualization(t *testing.T) {
	s := NewStore(testConfig(t))
	_ = s.Setup("admin", "Password12")

	// Unknown user path
	t0 := time.Now()
	_ = s.VerifyPassword("nobody-exists", "whatever")
	unknownUserDuration := time.Since(t0)

	// Known user, wrong password
	t0 = time.Now()
	_ = s.VerifyPassword("admin", "wrong-password-here")
	knownUserBadPwDuration := time.Since(t0)

	// Both must take real bcrypt time (> 50ms at cost 12 — generous bound).
	if unknownUserDuration < 50*time.Millisecond {
		t.Errorf("unknown user verify took %v, expected > 50ms (dummy hash must be real bcrypt)", unknownUserDuration)
	}
	if knownUserBadPwDuration < 50*time.Millisecond {
		t.Errorf("wrong pw verify took %v, expected > 50ms", knownUserBadPwDuration)
	}

	// Timing must be within a factor of 10 of each other (generous — CI noise).
	ratio := float64(unknownUserDuration) / float64(knownUserBadPwDuration)
	if ratio > 10 || ratio < 0.1 {
		t.Errorf("timing ratio unknown/known = %.2f (want within 10x); unknown=%v known=%v",
			math.Abs(ratio), unknownUserDuration, knownUserBadPwDuration)
	}
}
