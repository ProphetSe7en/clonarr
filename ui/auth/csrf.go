package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// CSRF protection via the double-submit cookie pattern.
//
// - Server sets a random CSRF token in a non-HttpOnly cookie on the first
//   GET request that doesn't already carry one.
// - Client (our own JS or our own HTML forms) reads the cookie value and
//   submits it back as either:
//     - X-CSRF-Token header (for AJAX/JSON fetches), OR
//     - csrf_token form field (for HTML form POSTs to /login, /setup)
// - Server verifies the submitted token matches the cookie on every
//   mutating request (POST/PUT/DELETE/PATCH).
//
// Why this works: an attacker-controlled site (evil.com) can cause the
// user's browser to send requests to our origin with the session cookie
// attached (via form submission, image src, fetch mode:no-cors, etc.) —
// but JavaScript on evil.com cannot read cookies set by our origin (same-
// origin policy), so it cannot include a matching header or form field.
//
// Requests authenticated via a VALID API key (X-Api-Key header or
// ?apikey= query) bypass CSRF — those are programmatic, not browser-
// driven, so the CSRF attack model doesn't apply. Note: only a valid
// key bypasses. A request that merely SETS an X-Api-Key header with an
// arbitrary value does not — that would have been an attacker's trick
// to bypass CSRF without actually holding a valid key.
//
// NOTE: these are methods on Store (not package-level functions) so they
// can call VerifyAPIKey (for valid-key check above) and
// IsRequestFromTrustedProxy (for correct Secure-cookie behaviour behind
// reverse-proxy-terminated TLS).

const (
	CSRFCookieName = "clonarr_csrf"
	CSRFHeaderName = "X-CSRF-Token"
	CSRFFormField  = "csrf_token"
	csrfTokenBytes = 32 // → 64 hex chars in the cookie value
)

// EnsureCSRFCookie returns the CSRF token for this browser session,
// setting a fresh cookie if none exists. Call on any GET/HEAD/OPTIONS
// that reaches a handler — the token is then available to JS and HTML
// templates for inclusion in subsequent mutating requests.
//
// Secure flag: set when the request arrived over TLS directly OR via a
// configured trusted proxy that forwarded X-Forwarded-Proto=https.
// Matches the session-cookie logic in setSessionCookie so cookie-
// security properties are consistent between the two.
func (s *Store) EnsureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(CSRFCookieName); err == nil && len(c.Value) == csrfTokenBytes*2 {
		return c.Value
	}
	b := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "" // caller will render without a token; mutations will fail (correct)
	}
	token := hex.EncodeToString(b)
	secure := r.TLS != nil
	if !secure && s.IsRequestFromTrustedProxy(r) {
		secure = strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JavaScript MUST read this cookie to echo it back
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return token
}

// CSRFMiddleware enforces the double-submit cookie check on mutating
// requests. Wire it EARLY in the middleware chain (before auth) so the
// cookie is set on any GET that reaches any handler. That way both
// public endpoints (/login GET, /setup GET) and authenticated ones can
// receive the token.
func (s *Store) CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			s.EnsureCSRFCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}

		// VALID API-key requests bypass CSRF. Key must actually verify —
		// an attacker setting a bogus X-Api-Key header to bypass the
		// CSRF check doesn't work: they don't hold the real key.
		if apiKey := extractAPIKey(r); apiKey != "" && s.VerifyAPIKey(apiKey) {
			next.ServeHTTP(w, r)
			return
		}

		// Double-submit verification.
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil || cookie.Value == "" {
			http.Error(w, "CSRF cookie missing — refresh the page and try again", http.StatusForbidden)
			return
		}
		var submitted string
		if h := r.Header.Get(CSRFHeaderName); h != "" {
			submitted = h
		} else if strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			_ = r.ParseForm()
			submitted = r.FormValue(CSRFFormField)
		}
		if submitted == "" {
			http.Error(w, "CSRF token missing", http.StatusForbidden)
			return
		}
		if subtle.ConstantTimeCompare([]byte(submitted), []byte(cookie.Value)) != 1 {
			http.Error(w, "CSRF token invalid", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SecurityHeadersMiddleware sets basic security headers on every response.
// Each header targets a specific well-understood attack class; none of
// them break legitimate browser behavior for our UI. Stateless — not
// a Store method.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent our pages from being iframe'd — blocks clickjacking.
		w.Header().Set("X-Frame-Options", "DENY")
		// Prevent MIME-type confusion attacks (browser sniffing JS out of
		// a file that's served as text/plain, etc.).
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Don't leak full URLs in Referer headers to cross-origin
		// destinations. Same-origin navigation keeps full Referer.
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
