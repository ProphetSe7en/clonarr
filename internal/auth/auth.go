// Package auth implements authentication for Clonarr, matching the
// Radarr/Sonarr Security-panel model:
//
//   Authentication           = forms | basic | none
//   Authentication Required  = enabled | disabled_for_local_addresses
//
// Credentials (username, bcrypt password hash, API key) live in
// /config/auth.json, separate from the main bash .conf file.
//
// Auth is enforced via HTTP middleware that runs before any handler.
// Every request is classified as AuthPublic (no auth ever) or AuthRequired
// (default — needs session cookie, valid Basic header, API key, or
// local-address bypass).
//
// Known limitation: Basic auth mode performs a bcrypt verify on every
// request (~250ms). This is a CPU-amplification DoS vector without rate
// limiting. Rate limiting on auth endpoints lands in Phase 2 of the
// security hardening plan. Users should prefer Forms auth or front the
// app with a reverse proxy that handles auth.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"clonarr/internal/netsec"
)

// safeGoAuth runs fn in a new goroutine with panic recovery. Kept local
// to the auth package so this self-contained security core stays import-
// compatible across containers (Constat / vpn-gateway) that
// live in their own modules. Duplicate of internal/utils/SafeGo by design.
func safeGoAuth(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("auth: panic in %s: %v\n%s", name, r, debug.Stack())
			}
		}()
		fn()
	}()
}

// AuthMode controls HOW a user authenticates.
type AuthMode string

const (
	ModeForms AuthMode = "forms" // login page + session cookie (default)
	ModeBasic AuthMode = "basic" // HTTP Basic Auth
	ModeNone  AuthMode = "none"  // disabled, requires env var confirmation
)

// Requirement controls WHO must authenticate.
type Requirement string

const (
	RequireAll      Requirement = "enabled"                       // every request needs auth
	RequireExtLocal Requirement = "disabled_for_local_addresses" // default — LAN skips
)

// Config carries the subset of app config that auth cares about.
type Config struct {
	Mode             AuthMode
	Requirement      Requirement
	TrustedProxies   []net.IP
	// TrustedNetworks is the user-curated list of IPs/CIDRs that bypass auth
	// when Requirement == RequireExtLocal. Empty means: fall back to the
	// hardcoded default (loopback + RFC1918 + link-local + ULA — Radarr
	// parity). When non-empty, ONLY these networks bypass (plus loopback,
	// always included implicitly so healthchecks work).
	TrustedNetworks  []*net.IPNet
	// TrustedNetworksLocked / TrustedProxiesLocked are true when the value
	// came from an environment variable (TRUSTED_NETWORKS / TRUSTED_PROXIES
	// at process start). Env-locked fields cannot be changed via the UI —
	// save attempts are rejected with a clear error. Rationale: a UI-takeover
	// attacker (session-hijack, local-bypass peer) cannot expand the trust
	// boundary without host-level access. Set the env vars in the Unraid
	// template (or docker run --env) for ops-grade lockdown.
	TrustedNetworksLocked bool
	TrustedProxiesLocked  bool
	// TrustedNetworksRaw / TrustedProxiesRaw hold the original comma-separated
	// string from the env var (populated only when *Locked is true). Used by
	// the UI to display the effective value in the disabled input so users
	// can see what's actually enforced without inspecting the Docker template.
	TrustedNetworksRaw string
	TrustedProxiesRaw  string
	SessionTTL       time.Duration
	AuthFilePath     string // default /config/auth.json
	SessionsFilePath string // default /config/sessions.json — survives container restart
	MaxSessions      int    // cap on concurrent sessions; 0 → 10000
}

// DefaultConfig returns the secure defaults applied when no explicit config
// is present. These match Radarr's out-of-the-box behavior.
func DefaultConfig() Config {
	return Config{
		Mode:             ModeForms,
		Requirement:      RequireExtLocal,
		SessionTTL:       30 * 24 * time.Hour,
		AuthFilePath:     "/config/auth.json",
		SessionsFilePath: "/config/sessions.json",
		MaxSessions:      10000,
	}
}

// creds is the on-disk shape of /config/auth.json.
type creds struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	APIKey       string    `json:"api_key"`
	CreatedAt    time.Time `json:"created_at"`
}

// sessionEntry records a session's expiry and creation time (for eviction).
type sessionEntry struct {
	expiry  time.Time
	created time.Time
}

// Store holds auth state for the running process.
type Store struct {
	cfg Config

	mu       sync.RWMutex
	loaded   bool
	creds    *creds
	sessions map[string]sessionEntry // session_id -> entry

	// persistMu serializes session-file writes. Every CreateSession /
	// DeleteSession / CleanupExpiredSessions spawns a goroutine that writes
	// to sessions.json.tmp; without a mutex two concurrent writes could
	// truncate each other or race on the rename. Held only during the
	// WriteFile + Rename in writeSessionsSnapshot — not while any in-memory
	// state is locked, so this doesn't cross the main mu critical path.
	persistMu sync.Mutex
}

// NewStore creates a Store with the given config. Credentials are loaded
// lazily from disk — call Load() explicitly if you want an early error.
func NewStore(cfg Config) *Store {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 10000
	}
	return &Store{
		cfg:      cfg,
		sessions: make(map[string]sessionEntry),
	}
}

// Load reads /config/auth.json if present. Returns (exists, error).
// A missing file is not an error — it's the signal that first-run setup
// is required. A malformed file IS an error — we refuse to silently
// treat it as "unconfigured" to prevent an attacker with file-write
// access from triggering a setup-wizard reset.
//
// Also loads /config/sessions.json if present so existing session cookies
// survive container restarts within their TTL. Sessions file errors are
// logged but non-fatal — worst case users re-login.
func (s *Store) Load() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.cfg.AuthFilePath)
	if os.IsNotExist(err) {
		s.loaded = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read auth file: %w", err)
	}
	var c creds
	if err := json.Unmarshal(data, &c); err != nil {
		return false, fmt.Errorf("parse auth file (refuse to auto-reset): %w", err)
	}
	if c.Username == "" || c.PasswordHash == "" || c.APIKey == "" {
		return false, fmt.Errorf("auth file is structurally valid but missing required fields")
	}
	s.creds = &c
	s.loaded = true

	// Best-effort sessions restore. Don't fail Load on errors here — the
	// sessions file is purely a UX convenience; worst case users re-login.
	s.loadSessionsLocked()

	return true, nil
}

// sessionFileEntry is the on-disk shape of a single session entry.
type sessionFileEntry struct {
	ID      string    `json:"id"`
	Expiry  time.Time `json:"expiry"`
	Created time.Time `json:"created"`
}

// loadSessionsLocked reads /config/sessions.json into s.sessions, dropping
// entries whose expiry has already passed. Caller must hold s.mu.
func (s *Store) loadSessionsLocked() {
	if s.cfg.SessionsFilePath == "" {
		return
	}
	data, err := os.ReadFile(s.cfg.SessionsFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("auth: read sessions file: %v (continuing)", err)
		}
		return
	}
	var entries []sessionFileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("auth: parse sessions file: %v (continuing with empty session map)", err)
		return
	}
	now := time.Now()
	kept := 0
	for _, e := range entries {
		if now.Before(e.Expiry) {
			s.sessions[e.ID] = sessionEntry{expiry: e.Expiry, created: e.Created}
			kept++
		}
	}
	if kept > 0 {
		log.Printf("auth: restored %d session(s) from disk", kept)
	}
}

// persistSessionsLocked snapshots the session map under the write lock
// the caller already holds, then fires a goroutine that writes to disk
// WITHOUT holding the lock. The caller MUST hold s.mu.
//
// Prior implementation blocked the exclusive mutex through fsync-less
// WriteFile + rename; on slow storage that stalled every auth check
// (which takes RLock) behind the session-write. Attackers spamming
// /logout (no auth required by design) could amplify this. Moving disk
// I/O out from under the lock removes the DoS vector.
func (s *Store) persistSessionsLocked() {
	if s.cfg.SessionsFilePath == "" {
		return
	}
	// Snapshot under the lock we already hold.
	entries := make([]sessionFileEntry, 0, len(s.sessions))
	for id, e := range s.sessions {
		entries = append(entries, sessionFileEntry{ID: id, Expiry: e.expiry, Created: e.created})
	}
	path := s.cfg.SessionsFilePath
	// Write off the critical path. Don't wait for it to finish — eventual
	// consistency is fine; worst case a restart drops the last few seconds
	// of sessions (user re-logs in). Wrapped in safeGoAuth so a panic in
	// MarshalIndent / os.WriteFile can't crash the container.
	safeGoAuth("writeSessionsSnapshot", func() {
		s.writeSessionsSnapshot(path, entries)
	})
}

// writeSessionsSnapshot is a method (not a free function) so it can take
// persistMu. Two concurrent logins/logouts would otherwise spawn two
// goroutines both writing sessions.json.tmp — the second truncates the
// first's partial write, or their renames race and one overwrites the
// other's data.
func (s *Store) writeSessionsSnapshot(path string, entries []sessionFileEntry) {
	data, err := json.Marshal(entries)
	if err != nil {
		log.Printf("auth: marshal sessions: %v", err)
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	tmp := path + ".tmp"
	// O_TRUNC acceptable for sessions file — no secret material, atomic-ish
	// write via tmp+rename is sufficient, no need for O_EXCL here.
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		log.Printf("auth: write sessions tmp: %v", err)
		return
	}
	_ = os.Chown(tmp, 99, 100)
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("auth: rename sessions file: %v", err)
		return
	}
}

// IsConfigured reports whether credentials exist on disk.
func (s *Store) IsConfigured() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.creds != nil
}

// Setup creates the first admin user. Fails if credentials already exist
// (prevents accidental overwrite).
func (s *Store) Setup(username, password string) error {
	if err := validateUsername(username); err != nil {
		return err
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds != nil {
		return errors.New("auth already configured")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	apiKey, err := randomHex(16) // 32 hex chars
	if err != nil {
		return err
	}

	c := &creds{
		Username:     username,
		PasswordHash: string(hash),
		APIKey:       apiKey,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.writeCredsLocked(c); err != nil {
		return err
	}
	s.creds = c
	return nil
}

// VerifyPassword checks username+password. Uses a real dummy bcrypt hash
// on the unknown-user path so timing is indistinguishable from the
// known-user-wrong-password path (anti-enumeration).
func (s *Store) VerifyPassword(username, password string) bool {
	s.mu.RLock()
	c := s.creds
	s.mu.RUnlock()
	if c == nil || username != c.Username {
		// Equalize timing with the bcrypt path.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)) == nil
}

// CreateSession returns a new session ID. If the session cap is reached,
// the oldest-created session is evicted to make room.
func (s *Store) CreateSession() (string, error) {
	id, err := randomHex(32) // 256 bits entropy
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict expired first (free storage without shrinking cap).
	for k, v := range s.sessions {
		if now.After(v.expiry) {
			delete(s.sessions, k)
		}
	}
	// If still at cap, evict the oldest-created.
	if len(s.sessions) >= s.cfg.MaxSessions {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range s.sessions {
			if oldestKey == "" || v.created.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.created
			}
		}
		if oldestKey != "" {
			delete(s.sessions, oldestKey)
		}
	}

	s.sessions[id] = sessionEntry{
		expiry:  now.Add(s.cfg.SessionTTL),
		created: now,
	}
	s.persistSessionsLocked()
	return id, nil
}

// ValidateSession returns true if the session ID is known and unexpired.
func (s *Store) ValidateSession(id string) bool {
	if id == "" {
		return false
	}
	s.mu.RLock()
	entry, ok := s.sessions[id]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(entry.expiry)
}

// DeleteSession removes the session (used on logout).
func (s *Store) DeleteSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	s.persistSessionsLocked()
}

// VerifyAPIKey checks the supplied key against the stored key in constant time.
func (s *Store) VerifyAPIKey(key string) bool {
	if key == "" {
		return false
	}
	s.mu.RLock()
	c := s.creds
	s.mu.RUnlock()
	if c == nil {
		return false
	}
	return netsec.SecureEqual(key, c.APIKey)
}

// Username returns the configured admin username.
func (s *Store) Username() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return ""
	}
	return s.creds.Username
}

// APIKey returns the raw API key (for display to authenticated admin only).
func (s *Store) APIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return ""
	}
	return s.creds.APIKey
}

// ChangePassword verifies the old password and, if correct, replaces the
// stored bcrypt hash with one derived from the new password. Invalidates
// all existing sessions so other logged-in clients are forced to re-login.
// Returns error on: bad old password, weak new password, I/O failure.
func (s *Store) ChangePassword(oldPw, newPw string) error {
	if err := validatePassword(newPw); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds == nil {
		return errors.New("auth not configured")
	}
	// Verify old password BEFORE we do any work. Not equalizing timing here —
	// the user is already authenticated to reach this endpoint (middleware
	// requires session/API key/local bypass), so timing-based enumeration
	// is not the concern. What we're preventing is session-hijack escalation
	// to persistent admin via password replacement without knowing old pw.
	if err := bcrypt.CompareHashAndPassword([]byte(s.creds.PasswordHash), []byte(oldPw)); err != nil {
		return errors.New("current password is incorrect")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPw), 12)
	if err != nil {
		return err
	}
	newCreds := *s.creds
	newCreds.PasswordHash = string(hash)
	if err := s.writeCredsLocked(&newCreds); err != nil {
		return err
	}
	s.creds = &newCreds
	// Invalidate every session — user must re-login, plus any stolen cookies
	// become dead. The caller (HTTP handler) creates a fresh session for the
	// current request so the user isn't logged out on success.
	s.sessions = make(map[string]sessionEntry)
	s.persistSessionsLocked()
	return nil
}

// RegenerateAPIKey generates a fresh key, invalidating the old one.
func (s *Store) RegenerateAPIKey() (string, error) {
	key, err := randomHex(16)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds == nil {
		return "", errors.New("auth not configured")
	}
	s.creds.APIKey = key
	if err := s.writeCredsLocked(s.creds); err != nil {
		return "", err
	}
	return key, nil
}

// Config returns the current config (for status endpoint).
func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// UpdateConfig atomically replaces the live auth config. Use after the
// user saves changes to AUTHENTICATION / AUTHENTICATION_REQUIRED /
// TRUSTED_PROXIES / SESSION_TTL_DAYS through the UI so changes take
// effect immediately — no container restart needed.
//
// The new config must pass ValidateConfig; if it fails, the old config
// is kept and an error returned (so a bad UI submission can't break
// auth on a running server).
//
// Does NOT invalidate existing sessions — users stay logged in across
// config updates. If you specifically want to kick all sessions (e.g.
// lowering SessionTTL dramatically), call this followed by a sessions
// reset separately.
func (s *Store) UpdateConfig(cfg Config) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Preserve all deployment-level / env-locked fields: they're not caller-
	// editable. A future caller that builds cfg from scratch (not from
	// Config()) would otherwise silently drop these, weakening T63's env
	// lock and breaking session persistence.
	cfg.AuthFilePath = s.cfg.AuthFilePath
	cfg.SessionsFilePath = s.cfg.SessionsFilePath
	cfg.MaxSessions = s.cfg.MaxSessions
	cfg.TrustedNetworksLocked = s.cfg.TrustedNetworksLocked
	cfg.TrustedProxiesLocked = s.cfg.TrustedProxiesLocked
	cfg.TrustedNetworksRaw = s.cfg.TrustedNetworksRaw
	cfg.TrustedProxiesRaw = s.cfg.TrustedProxiesRaw
	// If locked, also force the parsed values back to the env-derived state
	// — defense in depth against C1-style callers that forgot the lock guard.
	if s.cfg.TrustedNetworksLocked {
		cfg.TrustedNetworks = s.cfg.TrustedNetworks
	}
	if s.cfg.TrustedProxiesLocked {
		cfg.TrustedProxies = s.cfg.TrustedProxies
	}
	s.cfg = cfg
	return nil
}

// CleanupExpiredSessions removes expired sessions. The main goroutine
// should call this periodically (e.g. every 5 minutes) to keep memory
// bounded under normal load. Persists to disk iff anything was removed.
func (s *Store) CleanupExpiredSessions() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, entry := range s.sessions {
		if now.After(entry.expiry) {
			delete(s.sessions, id)
			removed++
		}
	}
	if removed > 0 {
		s.persistSessionsLocked()
	}
}

// writeCredsLocked persists creds to disk atomically. Caller must hold s.mu.
func (s *Store) writeCredsLocked(c *creds) error {
	dir := filepath.Dir(s.cfg.AuthFilePath)
	// Parent dir is 0700 so filename existence/mtime isn't readable to other
	// UIDs on shared-volume hosts (Unraid appdata).
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.cfg.AuthFilePath + ".tmp"
	// O_EXCL prevents symlink-following attacks (another writer can't pre-create
	// the tmp file pointing elsewhere).
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		// If the .tmp file already exists (previous crash), remove and retry once.
		if os.IsExist(err) {
			_ = os.Remove(tmp)
			f, err = os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		}
		if err != nil {
			return fmt.Errorf("open tmp auth file: %w", err)
		}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write auth tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("fsync auth tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Chown is best-effort: on non-Unraid hosts UID 99/GID 100 may not exist.
	if err := os.Chown(tmp, 99, 100); err != nil && !os.IsPermission(err) {
		log.Printf("auth: chown tmp auth file (non-fatal): %v", err)
	}
	return os.Rename(tmp, s.cfg.AuthFilePath)
}

// ===== Validation =====

// validateUsername enforces: 1..64 chars, no control chars, no leading/trailing space.
func validateUsername(u string) error {
	if u == "" {
		return errors.New("username is required")
	}
	if u != strings.TrimSpace(u) {
		return errors.New("username must not have leading/trailing whitespace")
	}
	if len(u) > 64 {
		return errors.New("username too long (max 64 chars)")
	}
	for _, r := range u {
		if unicode.IsControl(r) {
			return errors.New("username must not contain control characters")
		}
	}
	return nil
}

// validatePassword enforces: min 10 chars, at least 2 of {upper, lower, digit, symbol}.
func validatePassword(pw string) error {
	if len(pw) < 10 {
		return errors.New("password must be at least 10 characters")
	}
	var hasUpper, hasLower, hasDigit, hasSymbol bool
	for _, r := range pw {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSymbol = true
		}
	}
	count := 0
	for _, b := range []bool{hasUpper, hasLower, hasDigit, hasSymbol} {
		if b {
			count++
		}
	}
	if count < 2 {
		return errors.New("password must contain at least 2 of: uppercase, lowercase, digit, symbol")
	}
	return nil
}

// ===== Random helpers =====

func randomHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ===== Timing-equalization dummy hash =====

// dummyHash is a real bcrypt hash (cost 12) computed once at package init.
// Used in VerifyPassword when the username is unknown, so the response
// time is indistinguishable from the known-user-wrong-password case.
// Generating a fresh hash per process is cheap (one-time ~250ms cost at
// startup) and avoids shipping a hardcoded sample that an attacker could
// pre-compute collisions against.
var dummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-equalization-not-a-real-password"), 12)
	if err != nil {
		// bcrypt with default cost on a real password should never fail.
		panic(fmt.Sprintf("auth: cannot initialize dummy hash: %v", err))
	}
	dummyHash = h
}

// ===== Endpoint classification =====

// publicExact is a set of paths that are always treated as public.
// /api/health is included so Docker / Kubernetes / Uptime-Kuma-style
// healthchecks work regardless of auth configuration. The endpoint
// returns only a liveness signal — no data, no enumeration risk.
var publicExact = map[string]struct{}{
	"/login":            {},
	"/logout":           {},
	"/setup":            {},
	"/api/auth/status":  {},
	"/api/health":       {},
}

// publicPrefixes are sub-tree prefixes treated as public. Must include the
// trailing slash; a path is public if it has exactly this prefix followed
// by more characters (so "/static" alone is NOT public, but "/static/x" is).
// This prevents "/loginhack" from matching "/login".
var publicPrefixes = []string{
	"/static/",
	"/setup/", // for any setup-wizard sub-resources (e.g. form POSTs)
	"/css/",
	"/js/",
	"/icons/",
}

// IsPublic returns true if the request path should bypass auth. Exact-match
// against publicExact, or path starts with one of publicPrefixes.
func IsPublic(path string) bool {
	if _, ok := publicExact[path]; ok {
		return true
	}
	for _, p := range publicPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// ===== Middleware =====

// SessionCookieName is the name of the session cookie.
const SessionCookieName = "clonarr_session"

// Middleware wraps an http.Handler with auth enforcement. See package docs
// for the decision flow.
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if IsPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// If not configured yet, force users to the setup wizard.
		if !s.IsConfigured() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}

		// Snapshot the config up front so concurrent UpdateConfig calls
		// don't tear slice reads mid-request (T29 / C3). Config() takes
		// the RLock and returns a value copy.
		cfg := s.Config()

		// Mode "none" — all requests pass (startup validated I_UNDERSTAND).
		if cfg.Mode == ModeNone {
			next.ServeHTTP(w, r)
			return
		}

		// API key beats everything.
		if key := extractAPIKey(r); key != "" && s.VerifyAPIKey(key) {
			next.ServeHTTP(w, r)
			return
		}

		// Local-address bypass.
		if cfg.Requirement == RequireExtLocal {
			clientIP := netsec.ParseClientIP(r, cfg.TrustedProxies)
			if clientIP != nil && s.IsLocalAddress(clientIP) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Mode-specific auth check.
		switch cfg.Mode {
		case ModeForms:
			if cookie, err := r.Cookie(SessionCookieName); err == nil {
				if s.ValidateSession(cookie.Value) {
					next.ServeHTTP(w, r)
					return
				}
				// Expired/invalid cookie — clear it so the browser doesn't keep sending.
				http.SetCookie(w, &http.Cookie{
					Name:     SessionCookieName,
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)

		case ModeBasic:
			user, pass, ok := r.BasicAuth()
			if !ok || !s.VerifyPassword(user, pass) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Clonarr", charset="UTF-8"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)

		default:
			http.Error(w, "Authentication misconfigured", http.StatusInternalServerError)
			log.Printf("auth: unknown mode %q", cfg.Mode)
		}
	})
}

// extractAPIKey returns the API key from either the X-Api-Key header or the
// ?apikey= query parameter.
//
// NOTE: the query-parameter form (?apikey=...) appears in server access
// logs, reverse-proxy logs, browser history, and Referer headers when the
// user navigates away. Callers should treat the query form as a reduced-
// security fallback and prefer the header form for scripted access. Any
// request-logging middleware in front of this package MUST scrub the
// `apikey` parameter.
func extractAPIKey(r *http.Request) string {
	if h := r.Header.Get("X-Api-Key"); h != "" {
		return h
	}
	return r.URL.Query().Get("apikey")
}

// ValidateConfig checks that the runtime config is safe to start with.
// Rejects unknown enum values and unreasonable TTLs.
//
// `none` mode is permitted without an env-var escape hatch: the UI
// requires a typed confirmation ("DISABLE") and shows a persistent red
// banner plus periodic log warnings so the insecure state is impossible
// to miss. That matches Radarr's UX and the realistic homelab threat
// model (an attacker with UI admin access has already won regardless).
func ValidateConfig(cfg Config) error {
	switch cfg.Mode {
	case ModeForms, ModeBasic, ModeNone:
		// ok
	default:
		return fmt.Errorf("authentication: unknown mode %q (expected forms | basic | none)", cfg.Mode)
	}
	switch cfg.Requirement {
	case RequireAll, RequireExtLocal:
		// ok
	default:
		return fmt.Errorf(
			"authentication_required: unknown value %q (expected enabled | disabled_for_local_addresses)",
			cfg.Requirement)
	}
	if cfg.SessionTTL <= 0 {
		return errors.New("session TTL must be positive")
	}
	if cfg.SessionTTL > 365*24*time.Hour {
		return errors.New("session TTL must be ≤ 365 days (prevents overflow on 32-bit platforms)")
	}
	return nil
}

// TrustedNetworksLocked reports whether the trusted-networks value came
// from an environment variable (and therefore cannot be edited via UI).
func (s *Store) TrustedNetworksLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TrustedNetworksLocked
}

// TrustedProxiesLocked reports whether the trusted-proxies value came
// from an environment variable (and therefore cannot be edited via UI).
func (s *Store) TrustedProxiesLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TrustedProxiesLocked
}

// TrustedNetworksRaw returns the raw env-var string when the field is
// env-locked, empty otherwise.
func (s *Store) TrustedNetworksRaw() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TrustedNetworksRaw
}

// TrustedProxiesRaw returns the raw env-var string when the field is
// env-locked, empty otherwise.
func (s *Store) TrustedProxiesRaw() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.TrustedProxiesRaw
}

// IsLocalAddress returns true if the given IP should bypass auth under
// "Disabled for Local Addresses" mode. If TrustedNetworks is configured,
// uses only that list (plus loopback, always implicit). Otherwise falls
// back to the hardcoded Radarr-parity default (loopback + RFC1918 +
// link-local + ULA, via netsec.IsLocalAddress).
func (s *Store) IsLocalAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	// Normalize IPv4-mapped IPv6 before range checks.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	// Loopback always bypasses — Docker healthchecks, localhost admin
	// tooling etc. never need to authenticate against us.
	if ip.IsLoopback() {
		return true
	}
	s.mu.RLock()
	nets := s.cfg.TrustedNetworks
	s.mu.RUnlock()
	if len(nets) == 0 {
		// No user list → Radarr-parity default.
		return netsec.IsLocalAddress(ip)
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsRequestFromTrustedProxy returns true iff the HTTP request's direct
// peer (r.RemoteAddr) is listed in TrustedProxies. Used to decide whether
// X-Forwarded-* headers on THIS request can be trusted.
func (s *Store) IsRequestFromTrustedProxy(r *http.Request) bool {
	s.mu.RLock()
	tps := s.cfg.TrustedProxies
	s.mu.RUnlock()
	if len(tps) == 0 {
		return false
	}
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	peer := net.ParseIP(host)
	if peer == nil {
		return false
	}
	for _, tp := range tps {
		if tp.Equal(peer) {
			return true
		}
	}
	return false
}
