package api

// HTTP handlers for the auth surface: setup wizard, login page, logout,
// status endpoint, and API-key regeneration.
//
// These handlers are intentionally minimal — server-rendered HTML with
// inline CSS. The goal is to work reliably from a fresh install with
// zero frontend build step. Styling mirrors the existing dark theme of
// the main Clonarr UI for visual consistency.

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"clonarr/internal/auth"
	"clonarr/internal/core"
	"clonarr/internal/netsec"
	"clonarr/internal/utils"
)

// AuthHandlers bundles the dependencies needed by each handler.
type AuthHandlers struct {
	Store   *auth.Store
	Version string
}

// ---------- Setup wizard ----------

var setupTmpl = template.Must(template.New("setup").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Clonarr — First-run setup</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body { background:#0f1115; color:#e6e6e6; font-family:-apple-system,Segoe UI,Roboto,sans-serif; margin:0; display:flex; min-height:100vh; align-items:center; justify-content:center; }
  .card { background:#1a1d24; border:1px solid #2a2e38; border-radius:10px; padding:32px 36px; width:min(420px, 92vw); box-shadow:0 8px 24px rgba(0,0,0,.4); }
  h1 { margin:0 0 6px; font-size:22px; }
  .sub { color:#a0a4ab; margin-bottom:22px; font-size:13px; line-height:1.5; }
  label { display:block; margin:14px 0 6px; font-size:13px; color:#c3c7cc; }
  input { width:100%; box-sizing:border-box; padding:10px 12px; background:#0f1115; color:#e6e6e6; border:1px solid #2a2e38; border-radius:6px; font-size:14px; }
  input:focus { outline:none; border-color:#4a90e2; }
  button { margin-top:22px; width:100%; padding:11px; background:#4a90e2; color:#fff; border:none; border-radius:6px; font-size:14px; font-weight:500; cursor:pointer; }
  button:hover { background:#5aa0f2; }
  .err { background:#3a1a1a; color:#ff9a9a; border:1px solid #5a2a2a; border-radius:6px; padding:10px 12px; margin-bottom:14px; font-size:13px; }
  .hint { color:#888; font-size:12px; margin-top:4px; line-height:1.4; }
  footer { margin-top:18px; font-size:12px; color:#6a6e76; text-align:center; }
</style>
</head>
<body>
<div class="card">
  <h1>Welcome to Clonarr</h1>
  <p class="sub">Authentication is now required. Create the admin account to continue — you'll use this login every time you access Clonarr from outside your local network.</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <form method="POST" action="/setup">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <label>Username</label>
    <input type="text" name="username" value="{{.Username}}" required autofocus autocomplete="username">
    <label>Password</label>
    <input type="password" name="password" required autocomplete="new-password">
    <div class="hint">At least 10 characters, with at least 2 of: uppercase, lowercase, digit, symbol.</div>
    <label>Confirm password</label>
    <input type="password" name="password_confirm" required autocomplete="new-password">
    <button type="submit">Create account</button>
  </form>
  <footer>Clonarr {{.Version}} · security · <a href="https://github.com/prophetse7en/clonarr" style="color:#4a90e2;text-decoration:none">github</a></footer>
</div>
</body>
</html>`))

func (h *AuthHandlers) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	// If already configured, bounce to main app (don't let anyone hit this twice).
	if h.Store.IsConfigured() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := setupTmpl.Execute(w, map[string]any{
		"Version":   h.Version,
		"CSRFToken": h.Store.EnsureCSRFCookie(w, r),
	}); err != nil {
		log.Printf("setup page render: %v", err)
	}
}

func (h *AuthHandlers) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if h.Store.IsConfigured() {
		http.Error(w, "Setup already completed", http.StatusConflict)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		h.renderSetupError(w, r, "", "Invalid form data")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirm")

	if password != confirm {
		h.renderSetupError(w, r, username, "Passwords do not match")
		return
	}
	if err := h.Store.Setup(username, password); err != nil {
		h.renderSetupError(w, r, username, err.Error())
		return
	}

	// Auto-login: create session so user lands directly in the UI.
	sessionID, err := h.Store.CreateSession()
	if err != nil {
		log.Printf("setup: create session after setup: %v", err)
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	h.setSessionCookie(w, r, sessionID)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *AuthHandlers) renderSetupError(w http.ResponseWriter, r *http.Request, username, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusBadRequest)
	if err := setupTmpl.Execute(w, map[string]any{
		"Version":   h.Version,
		"Username":  username,
		"Error":     errMsg,
		"CSRFToken": h.Store.EnsureCSRFCookie(w, r),
	}); err != nil {
		log.Printf("setup error render: %v", err)
	}
}

// ---------- Login ----------

var loginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Clonarr — Login</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  body { background:#0f1115; color:#e6e6e6; font-family:-apple-system,Segoe UI,Roboto,sans-serif; margin:0; display:flex; min-height:100vh; align-items:center; justify-content:center; }
  .card { background:#1a1d24; border:1px solid #2a2e38; border-radius:10px; padding:32px 36px; width:min(380px, 92vw); box-shadow:0 8px 24px rgba(0,0,0,.4); }
  h1 { margin:0 0 18px; font-size:22px; text-align:center; }
  label { display:block; margin:10px 0 6px; font-size:13px; color:#c3c7cc; }
  input { width:100%; box-sizing:border-box; padding:10px 12px; background:#0f1115; color:#e6e6e6; border:1px solid #2a2e38; border-radius:6px; font-size:14px; }
  input:focus { outline:none; border-color:#4a90e2; }
  button { margin-top:18px; width:100%; padding:11px; background:#4a90e2; color:#fff; border:none; border-radius:6px; font-size:14px; font-weight:500; cursor:pointer; }
  button:hover { background:#5aa0f2; }
  .err { background:#3a1a1a; color:#ff9a9a; border:1px solid #5a2a2a; border-radius:6px; padding:10px 12px; margin-bottom:14px; font-size:13px; }
  footer { margin-top:18px; font-size:12px; color:#6a6e76; text-align:center; }
</style>
</head>
<body>
<div class="card">
  <h1>Clonarr</h1>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <form method="POST" action="/login{{if .NextEncoded}}?next={{.NextEncoded}}{{end}}">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <label>Username</label>
    <input type="text" name="username" value="{{.Username}}" required autofocus autocomplete="username">
    <label>Password</label>
    <input type="password" name="password" required autocomplete="current-password">
    <button type="submit">Log in</button>
  </form>
  <footer>Clonarr {{.Version}}</footer>
</div>
</body>
</html>`))

func (h *AuthHandlers) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// Not configured yet — force setup first.
	if !h.Store.IsConfigured() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	next := sanitizeNext(r.URL.Query().Get("next"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := loginTmpl.Execute(w, map[string]any{
		"Version":     h.Version,
		"Next":        next,
		"NextEncoded": url.QueryEscape(next),
		"CSRFToken":   h.Store.EnsureCSRFCookie(w, r),
	}); err != nil {
		log.Printf("login page render: %v", err)
	}
}

func (h *AuthHandlers) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if !h.Store.IsConfigured() {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, r, "", "Invalid form data")
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if !h.Store.VerifyPassword(username, password) {
		// Generic error — do not distinguish "wrong user" from "wrong pw".
		h.renderLoginError(w, r, username, "Invalid username or password")
		return
	}

	sessionID, err := h.Store.CreateSession()
	if err != nil {
		log.Printf("login: create session: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.setSessionCookie(w, r, sessionID)

	next := sanitizeNext(r.URL.Query().Get("next"))
	if next == "" {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func (h *AuthHandlers) renderLoginError(w http.ResponseWriter, r *http.Request, username, errMsg string) {
	next := sanitizeNext(r.URL.Query().Get("next"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	if err := loginTmpl.Execute(w, map[string]any{
		"Version":     h.Version,
		"Username":    username,
		"Error":       errMsg,
		"Next":        next,
		"NextEncoded": url.QueryEscape(next),
		"CSRFToken":   h.Store.EnsureCSRFCookie(w, r),
	}); err != nil {
		log.Printf("login error render: %v", err)
	}
}

func (h *AuthHandlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		h.Store.DeleteSession(cookie.Value)
	}
	// Clear cookie in browser.
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// ---------- Auth status (public — UI reads this to render correct state) ----------

func (h *AuthHandlers) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	authenticated := false

	if h.Store.IsConfigured() {
		if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
			if h.Store.ValidateSession(cookie.Value) {
				authenticated = true
			}
		}
	}

	// adminEquivalent covers session-authenticated callers PLUS local-bypass
	// and mode=none callers. All three reach admin-gated UI endpoints via
	// the middleware, so they must see the same view on /api/auth/status —
	// otherwise the Security panel renders with stale lock-state and Save
	// fails later.
	adminEquivalent := authenticated || h.isLocalBypassRequest(r) || h.Store.Config().Mode == auth.ModeNone

	cfg := h.Store.Config()
	resp := map[string]any{
		"configured":              h.Store.IsConfigured(),
		"authenticated":           authenticated,
		"authentication":          string(cfg.Mode),        // needed for no-auth banner in UI
		"authentication_required": string(cfg.Requirement), // needed for UI dropdown state
	}
	if adminEquivalent {
		resp["username"] = h.Store.Username()
		resp["trusted_networks_locked"] = h.Store.TrustedNetworksLocked()
		resp["trusted_proxies_locked"] = h.Store.TrustedProxiesLocked()
		if h.Store.TrustedNetworksLocked() {
			resp["trusted_networks_effective"] = h.Store.TrustedNetworksRaw()
		}
		if h.Store.TrustedProxiesLocked() {
			resp["trusted_proxies_effective"] = h.Store.TrustedProxiesRaw()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp)
}

// isLocalBypassRequest mirrors the middleware's local-bypass check for use
// in public endpoints that need to distinguish admin-equivalent callers
// (local LAN, trusted via RequireExtLocal) from genuinely pre-auth probers.
func (h *AuthHandlers) isLocalBypassRequest(r *http.Request) bool {
	cfg := h.Store.Config()
	if cfg.Requirement != auth.RequireExtLocal {
		return false
	}
	clientIP := netsec.ParseClientIP(r, cfg.TrustedProxies)
	if clientIP == nil {
		return false
	}
	return h.Store.IsLocalAddress(clientIP)
}

// ---------- API key — view + regenerate ----------

func (h *AuthHandlers) handleGetAPIKey(w http.ResponseWriter, r *http.Request) {
	// AuthRequired endpoint — reaching here means caller is already an
	// authenticated admin (via session, Basic, API key, or local bypass).
	key := h.Store.APIKey()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"api_key": key})
}

func (h *AuthHandlers) handleRegenAPIKey(w http.ResponseWriter, r *http.Request) {
	// Requires auth (any form: session, API key, local-bypass). Matches the
	// Radarr/Sonarr convention. A local-bypass peer rotating the key is an
	// annoyance (admin's scripts break) but not an escalation — they
	// already had admin-equivalent access, and rotation just swaps which
	// key they hold. Adding extra password friction for a lateral move
	// isn't worth it for most users.
	newKey, err := h.Store.RegenerateAPIKey()
	if err != nil {
		writeAuthJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"api_key": newKey})
}

// ---------- Change password ----------

func (h *AuthHandlers) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var req struct {
		CurrentPassword    string `json:"current_password"`
		NewPassword        string `json:"new_password"`
		NewPasswordConfirm string `json:"new_password_confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAuthJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.NewPassword != req.NewPasswordConfirm {
		writeAuthJSONError(w, http.StatusBadRequest, "new passwords do not match")
		return
	}
	if err := h.Store.ChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
		writeAuthJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Old sessions (including the one used to make THIS request) have been
	// invalidated. Create a fresh session so the user stays logged in on
	// this browser and set the cookie.
	newSID, err := h.Store.CreateSession()
	if err != nil {
		log.Printf("change-password: create session after rotation: %v", err)
		// Not fatal — user can log in again with the new password.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "reauth_required": true})
		return
	}
	h.setSessionCookie(w, r, newSID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "reauth_required": false})
}

func writeAuthJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// ---------- Helpers ----------

// setSessionCookie writes the session cookie with secure flags appropriate
// for the current request.
//
// The Secure flag is set when either:
//   - The request arrived via a direct TLS connection (r.TLS != nil), or
//   - The request arrived via a TRUSTED proxy and that proxy declared HTTPS
//     via X-Forwarded-Proto.
//
// Un-trusted peers could forge X-Forwarded-Proto: https on a plain-HTTP
// request to downgrade our cookie's Secure flag, so we ignore the header
// unless the direct connection came from a configured trusted proxy.
func (h *AuthHandlers) setSessionCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	secure := r.TLS != nil
	if !secure && h.Store.IsRequestFromTrustedProxy(r) {
		secure = strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	ttl := h.Store.Config().SessionTTL
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(ttl / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// sanitizeNext returns the query-param "next" only if it's a safe same-origin
// relative path. Prevents open-redirect where ?next=https://evil.com.
//
// Accepts ONLY paths that pass all of:
//   - starts with "/"
//   - does NOT start with "//" (scheme-relative) or "/\" (backslash-normalized)
//   - parses as URL with empty Scheme and empty Host
//   - contains no control bytes (< 0x20, == 0x7f) or Unicode line separators
//     (U+2028, U+2029) that could inject headers or poison logs
//   - does not contain ":" before the first "/" path segment (guards against
//     "javascript:" and similar if the browser's URL parsing is lenient)
func sanitizeNext(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return ""
	}
	if strings.ContainsAny(raw, "\\") {
		return ""
	}
	// Reject raw control bytes and Unicode line separators.
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return ""
		}
		if r == '\u2028' || r == '\u2029' {
			return ""
		}
	}
	// Structural validation: parse must succeed, scheme/host must be empty.
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return raw
}

// requireAuthForAPI writes a 401 JSON response when the middleware caught
// a request we need to reject. Unused by package — exported utility in case
// individual handlers need it.
func requireAuthForAPI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprint(w, `{"error":"unauthorized"}`)
}

// InitAuth loads auth settings from the main config store (JSON), validates,
// loads existing credentials from /config/auth.json, and returns the store
// + handlers ready to wire into the mux.
//
// Refuses to start (log.Fatal) on any unsafe combination (unknown enum
// values) or on malformed auth.json.
func InitAuth(ctx context.Context, configStore *core.ConfigStore, version string, mux *http.ServeMux) *auth.Store {
	cfg := auth.DefaultConfig()

	appCfg := configStore.Get()
	if appCfg.Authentication != "" {
		cfg.Mode = auth.AuthMode(appCfg.Authentication)
	}
	if appCfg.AuthenticationRequired != "" {
		cfg.Requirement = auth.Requirement(appCfg.AuthenticationRequired)
	}
	if appCfg.SessionTTLDays > 0 {
		cfg.SessionTTL = time.Duration(appCfg.SessionTTLDays) * 24 * time.Hour
	}
	// Env-var override for trust-boundary config.
	if envNets := strings.TrimSpace(os.Getenv("TRUSTED_NETWORKS")); envNets != "" {
		nets, err := netsec.ParseTrustedNetworks(envNets)
		if err != nil {
			log.Fatalf("auth: invalid TRUSTED_NETWORKS env var: %v", err)
		}
		cfg.TrustedNetworks = nets
		cfg.TrustedNetworksLocked = true
		cfg.TrustedNetworksRaw = envNets
		log.Printf("auth: trusted_networks locked by TRUSTED_NETWORKS env var (%d entries)", len(nets))
	} else if appCfg.TrustedNetworks != "" {
		nets, err := netsec.ParseTrustedNetworks(appCfg.TrustedNetworks)
		if err != nil {
			log.Fatalf("auth: invalid trustedNetworks config: %v", err)
		}
		cfg.TrustedNetworks = nets
	}

	if envProxies := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); envProxies != "" {
		ips, err := netsec.ParseTrustedProxies(envProxies)
		if err != nil {
			log.Fatalf("auth: invalid TRUSTED_PROXIES env var: %v", err)
		}
		cfg.TrustedProxies = ips
		cfg.TrustedProxiesLocked = true
		cfg.TrustedProxiesRaw = envProxies
		log.Printf("auth: trusted_proxies locked by TRUSTED_PROXIES env var (%d entries)", len(ips))
	} else if appCfg.TrustedProxies != "" {
		ips, err := netsec.ParseTrustedProxies(appCfg.TrustedProxies)
		if err != nil {
			log.Fatalf("auth: invalid trustedProxies config: %v", err)
		}
		cfg.TrustedProxies = ips
	}

	if err := auth.ValidateConfig(cfg); err != nil {
		log.Fatalf("auth config refuses to start: %v", err)
	}

	store := auth.NewStore(cfg)
	if _, err := store.Load(); err != nil {
		log.Fatalf("auth: load credentials: %v", err)
	}

	if store.IsConfigured() {
		log.Printf("auth: mode=%s required=%s user=%s", cfg.Mode, cfg.Requirement, store.Username())
	} else {
		log.Printf("auth: no credentials yet — first run, /setup wizard will prompt for admin user")
	}

	if cfg.Mode == auth.ModeNone {
		log.Printf("auth: WARNING — authentication is DISABLED via authentication=none. Do not expose this container to untrusted networks.")
	}

	// Periodic loud warning while in none mode. Wrapped in utils.SafeGo so
	// a panic in store.Config() (shouldn't happen, but defense in depth)
	// can't take down the container.
	utils.SafeGo("auth-none-warning", func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if store.Config().Mode == auth.ModeNone {
					log.Printf("auth: WARNING — authentication is still DISABLED. Every request is admin. Re-enable auth or restrict to 127.0.0.1.")
				}
			}
		}
	})

	authHandlers := &AuthHandlers{Store: store, Version: version}

	mux.HandleFunc("GET /setup", authHandlers.handleSetupPage)
	mux.HandleFunc("POST /setup", authHandlers.handleSetupSubmit)
	mux.HandleFunc("GET /login", authHandlers.handleLoginPage)
	mux.HandleFunc("POST /login", authHandlers.handleLoginSubmit)
	mux.HandleFunc("POST /logout", authHandlers.handleLogout)
	mux.HandleFunc("GET /api/auth/status", authHandlers.handleAuthStatus)
	mux.HandleFunc("GET /api/auth/api-key", authHandlers.handleGetAPIKey)
	mux.HandleFunc("POST /api/auth/regenerate-api-key", authHandlers.handleRegenAPIKey)
	mux.HandleFunc("POST /api/auth/change-password", authHandlers.handleChangePassword)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})

	return store
}
