package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"clonarr/auth"
	"clonarr/netsec"
)

var Version = "dev" // overridden at build time via ldflags

//go:embed static
var staticFiles embed.FS

// App holds shared application state.
// CleanupEvent records a stale rule/history removal for frontend notification.
type CleanupEvent struct {
	ProfileName string `json:"profileName"`
	InstanceName string `json:"instanceName"`
	ArrProfileID int    `json:"arrProfileId"`
	Timestamp    string `json:"timestamp"`
}

// AutoSyncEvent records an auto-sync result for frontend toast notification.
type AutoSyncEvent struct {
	InstanceName   string   `json:"instanceName"`
	ProfileName    string   `json:"profileName"`
	ArrProfileName string   `json:"arrProfileName,omitempty"`
	CFsCreated     int      `json:"cfsCreated"`
	CFsUpdated     int      `json:"cfsUpdated"`
	ScoresUpdated  int      `json:"scoresUpdated"`
	QualityUpdated bool     `json:"qualityUpdated"`
	SettingsCount  int      `json:"settingsCount"`
	Details        []string `json:"details,omitempty"` // e.g. "Repack/Proper: 5 → 6"
	Error          string   `json:"error,omitempty"`
	Timestamp      string   `json:"timestamp"`
}

type App struct {
	config         *configStore
	trash          *trashStore
	profiles       *profileStore
	customCFs      *customCFStore
	debugLog       *debugLogger
	httpClient     *http.Client // shared HTTP client for Arr/Prowlarr API calls (LAN targets legit)
	notifyClient   *http.Client // Gotify only — LAN targets legit (self-hosted Gotify)
	safeClient     *http.Client // Discord/Pushover — always external, SSRF-blocklisted
	authStore      *auth.Store  // exposed so handlers can live-reload auth settings
	pullUpdateCh   chan string  // send new interval string to reschedule pull
	cleanupEvents  []CleanupEvent
	cleanupMu      sync.Mutex
	autoSyncEvents []AutoSyncEvent
	autoSyncMu     sync.Mutex
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "6060"
	}

	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		configDir = "/config"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(configDir, "data")
	}

	// Initialize stores
	config := newConfigStore(configDir)
	if err := config.Load(); err != nil {
		log.Printf("Warning: could not load config: %v", err)
	}

	trash := newTrashStore(dataDir)
	profiles := newProfileStore(filepath.Join(configDir, "profiles"))
	customCFs := newCustomCFStore(filepath.Join(configDir, "custom", "json"))
	customCFs.migrateFromFlatDir(filepath.Join(configDir, "custom-cfs"))
	customCFs.migrateFilenames()

	// Migrate any imported profiles from old config to per-file storage
	migrateImportedProfiles(config, profiles)

	debugLog := newDebugLogger(configDir)
	debugLog.SetEnabled(config.Get().DebugLogging)

	app := &App{
		config:       config,
		trash:        trash,
		profiles:     profiles,
		customCFs:    customCFs,
		debugLog:     debugLog,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		notifyClient: &http.Client{Timeout: 10 * time.Second},
		safeClient:   netsec.NewSafeHTTPClient(10*time.Second, nil),
		pullUpdateCh: make(chan string, 1),
	}

	// Wire up changelog notification callback
	trash.onNewChangelog = func(section ChangelogSection) {
		app.notifyChangelog(section)
	}

	// Startup: reset auto-sync commit hashes so all rules re-evaluate on next pull.
	// This ensures quality item changes and other updates are picked up after version upgrades.
	// Also clean up broken rules with arrProfileId=0 (create mode bug from older versions).
	config.Update(func(cfg *Config) {
		cleaned := make([]AutoSyncRule, 0, len(cfg.AutoSync.Rules))
		for i := range cfg.AutoSync.Rules {
			cfg.AutoSync.Rules[i].LastSyncCommit = ""
			if cfg.AutoSync.Rules[i].ArrProfileID == 0 {
				log.Printf("Removing broken auto-sync rule %s (arrProfileId=0)", cfg.AutoSync.Rules[i].ID)
				continue
			}
			cleaned = append(cleaned, cfg.AutoSync.Rules[i])
		}
		cfg.AutoSync.Rules = cleaned
	})

	// Context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Background: clone/pull TRaSH repo on startup
	safeGo("startup-trash-pull", func() {
		cfg := config.Get()
		if err := trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("Startup TRaSH clone/pull failed: %v", err)
		} else {
			app.autoSyncQualitySizes()
			app.autoSyncAfterPull()
		}
	})

	// Scheduled TRaSH pull (reads interval from config, supports live rescheduling)
	safeGo("trash-pull-scheduler", func() {
		cfg := config.Get()
		interval := parsePullInterval(cfg.PullInterval)
		var ticker *time.Ticker
		var tickCh <-chan time.Time

		setTicker := func(d time.Duration) {
			if ticker != nil {
				ticker.Stop()
			}
			if d > 0 {
				ticker = time.NewTicker(d)
				tickCh = ticker.C
				log.Printf("Scheduled TRaSH pull every %s", d)
			} else {
				ticker = nil
				tickCh = nil
				log.Printf("Scheduled TRaSH pull disabled")
			}
		}
		setTicker(interval)

		for {
			select {
			case <-tickCh:
				cfg := config.Get()
				prevCommit := trash.CurrentCommit()
				log.Printf("Scheduled TRaSH pull starting...")
				if err := trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
					log.Printf("Scheduled TRaSH pull failed: %v", err)
				} else {
					newCommit := trash.CurrentCommit()
					if prevCommit != "" && newCommit != prevCommit {
						log.Printf("TRaSH repo updated: %s → %s", prevCommit, newCommit)
						app.notifyRepoUpdate(prevCommit, newCommit)
					} else {
						log.Printf("Scheduled TRaSH pull completed (no changes)")
					}
					app.autoSyncQualitySizes()
					app.autoSyncAfterPull()
				}
			case newInterval := <-app.pullUpdateCh:
				setTicker(parsePullInterval(newInterval))
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
				return
			}
		}
	})

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Config
	mux.HandleFunc("GET /api/config", app.handleGetConfig)
	mux.HandleFunc("PUT /api/config", app.handleUpdateConfig)

	// Instances
	mux.HandleFunc("GET /api/instances", app.handleListInstances)
	mux.HandleFunc("POST /api/instances", app.handleCreateInstance)
	mux.HandleFunc("PUT /api/instances/{id}", app.handleUpdateInstance)
	mux.HandleFunc("DELETE /api/instances/{id}", app.handleDeleteInstance)
	mux.HandleFunc("POST /api/instances/{id}/test", app.handleTestInstance)
	mux.HandleFunc("POST /api/test-connection", app.handleTestConnection)
	mux.HandleFunc("GET /api/instances/{id}/profiles", app.handleInstanceProfiles)
	mux.HandleFunc("PUT /api/instances/{id}/profiles/{profileId}/rename", app.handleRenameProfile)
	mux.HandleFunc("GET /api/instances/{id}/languages", app.handleInstanceLanguages)
	mux.HandleFunc("GET /api/instances/{id}/cfs", app.handleInstanceCFs)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes", app.handleInstanceQualitySizes)
	mux.HandleFunc("POST /api/instances/{id}/quality-sizes/sync", app.handleSyncQualitySizes)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes/overrides", app.handleGetQSOverrides)
	mux.HandleFunc("PUT /api/instances/{id}/quality-sizes/overrides", app.handleSaveQSOverrides)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes/auto-sync", app.handleGetQSAutoSync)
	mux.HandleFunc("PUT /api/instances/{id}/quality-sizes/auto-sync", app.handleSaveQSAutoSync)
	mux.HandleFunc("GET /api/instances/{id}/quality-definitions", app.handleQualityDefinitions)
	mux.HandleFunc("GET /api/instances/{id}/profile-export/{profileId}", app.handleInstanceProfileExport)
	mux.HandleFunc("POST /api/instances/{id}/backup", app.handleInstanceBackup)
	mux.HandleFunc("POST /api/instances/{id}/restore", app.handleInstanceRestore)
	mux.HandleFunc("GET /api/instances/{id}/naming", app.handleGetInstanceNaming)
	mux.HandleFunc("PUT /api/instances/{id}/naming", app.handleApplyNaming)
	mux.HandleFunc("GET /api/instances/{id}/compare", app.handleCompareProfile)
	mux.HandleFunc("POST /api/instances/{id}/profile-cfs/remove", app.handleRemoveProfileCFs)
	mux.HandleFunc("POST /api/instances/{id}/profile-cfs/sync-one", app.handleSyncSingleCF)

	// TRaSH
	mux.HandleFunc("GET /api/trash/status", app.handleTrashStatus)
	mux.HandleFunc("POST /api/trash/pull", app.handleTrashPull)
	mux.HandleFunc("GET /api/trash/{app}/cfs", app.handleTrashCFs)
	mux.HandleFunc("GET /api/trash/{app}/score-contexts", app.handleTrashScoreContexts)
	mux.HandleFunc("GET /api/trash/{app}/cf-groups", app.handleTrashCFGroups)
	mux.HandleFunc("GET /api/trash/{app}/profiles", app.handleTrashProfiles)
	mux.HandleFunc("GET /api/trash/{app}/profiles/{id}", app.handleTrashProfileDetail)
	mux.HandleFunc("GET /api/trash/{app}/quality-sizes", app.handleTrashQualitySizes)
	mux.HandleFunc("GET /api/trash/{app}/naming", app.handleTrashNaming)
	mux.HandleFunc("GET /api/trash/{app}/conflicts", app.handleTrashConflicts)

	// Import
	mux.HandleFunc("POST /api/import/profile", app.handleImportProfile)
	mux.HandleFunc("GET /api/import/{app}/profiles", app.handleGetImportedProfiles)
	mux.HandleFunc("GET /api/import/profiles/{id}/detail", app.handleImportedProfileDetail)
	mux.HandleFunc("PUT /api/import/profiles/{id}", app.handleUpdateImportedProfile)
	mux.HandleFunc("DELETE /api/import/profiles/{id}", app.handleDeleteImportedProfile)

	// Custom Profiles
	mux.HandleFunc("GET /api/trash/{app}/quality-presets", app.handleQualityPresets)
	mux.HandleFunc("GET /api/trash/{app}/all-cfs", app.handleAllCFsCategorized)
	mux.HandleFunc("POST /api/custom-profiles", app.handleCreateCustomProfile)
	mux.HandleFunc("PUT /api/custom-profiles/{id}", app.handleUpdateCustomProfile)

	// Custom CFs
	mux.HandleFunc("GET /api/custom-cfs/{app}", app.handleListCustomCFs)
	mux.HandleFunc("POST /api/custom-cfs", app.handleCreateCustomCFs)
	mux.HandleFunc("DELETE /api/custom-cfs/{id}", app.handleDeleteCustomCF)
	mux.HandleFunc("PUT /api/custom-cfs/{id}", app.handleUpdateCustomCF)
	mux.HandleFunc("POST /api/custom-cfs/import-from-instance", app.handleImportCFsFromInstance)
	mux.HandleFunc("GET /api/customformat/schema/{app}", app.handleCFSchema)

	// Sync
	mux.HandleFunc("POST /api/sync/dry-run", app.handleDryRun)
	mux.HandleFunc("POST /api/sync/apply", app.handleApply)

	// Sync History
	mux.HandleFunc("GET /api/instances/{id}/sync-history", app.handleSyncHistory)
	mux.HandleFunc("GET /api/instances/{id}/sync-history/{arrProfileId}/changes", app.handleProfileChangeHistory)
	mux.HandleFunc("DELETE /api/instances/{id}/sync-history/{arrProfileId}", app.handleDeleteSyncHistory)

	// Auto-Sync
	mux.HandleFunc("GET /api/auto-sync/settings", app.handleGetAutoSyncSettings)
	mux.HandleFunc("PUT /api/auto-sync/settings", app.handleSaveAutoSyncSettings)
	// Notification agents
	mux.HandleFunc("GET /api/auto-sync/notification-agents", app.handleListNotificationAgents)
	mux.HandleFunc("POST /api/auto-sync/notification-agents", app.handleCreateNotificationAgent)
	mux.HandleFunc("PUT /api/auto-sync/notification-agents/{id}", app.handleUpdateNotificationAgent)
	mux.HandleFunc("DELETE /api/auto-sync/notification-agents/{id}", app.handleDeleteNotificationAgent)
	mux.HandleFunc("POST /api/auto-sync/notification-agents/test", app.handleTestNotificationAgentInline)
	mux.HandleFunc("POST /api/auto-sync/notification-agents/{id}/test", app.handleTestNotificationAgent)
	mux.HandleFunc("GET /api/auto-sync/rules", app.handleListAutoSyncRules)
	mux.HandleFunc("POST /api/auto-sync/rules", app.handleCreateAutoSyncRule)
	mux.HandleFunc("PUT /api/auto-sync/rules/{id}", app.handleUpdateAutoSyncRule)
	mux.HandleFunc("DELETE /api/auto-sync/rules/{id}", app.handleDeleteAutoSyncRule)

	// Cleanup events
	mux.HandleFunc("GET /api/cleanup-events", app.handleCleanupEvents)
	mux.HandleFunc("GET /api/auto-sync/events", app.handleAutoSyncEvents)

	// Debug logging
	mux.HandleFunc("POST /api/debug/log", app.handleDebugLog)
	mux.HandleFunc("GET /api/debug/log/download", app.handleDebugDownload)

	// Cleanup
	mux.HandleFunc("POST /api/instances/{id}/cleanup/scan", app.handleCleanupScan)
	mux.HandleFunc("POST /api/instances/{id}/cleanup/apply", app.handleCleanupApply)
	mux.HandleFunc("GET /api/instances/{id}/cleanup/keep", app.handleGetCleanupKeep)
	mux.HandleFunc("PUT /api/instances/{id}/cleanup/keep", app.handleSaveCleanupKeep)

	// Scoring Sandbox
	mux.HandleFunc("POST /api/prowlarr/test", app.handleTestProwlarr)
	mux.HandleFunc("GET /api/scoring/prowlarr/indexers", app.handleScoringProwlarrIndexers)
	mux.HandleFunc("POST /api/scoring/prowlarr/search", app.handleScoringProwlarrSearch)
	mux.HandleFunc("POST /api/scoring/parse", app.handleScoringParse)
	mux.HandleFunc("POST /api/scoring/parse/batch", app.handleScoringParseBatch)
	mux.HandleFunc("GET /api/scoring/profile-scores", app.handleScoringProfileScores)

	// ==== Authentication =====================================================
	authStore, authHandlers := initAuth(ctx, config)
	app.authStore = authStore

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

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// Background: reap expired sessions every 5 min
	safeGo("session-cleanup", func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				authStore.CleanupExpiredSessions()
			}
		}
	})

	// Middleware chain — outermost first:
	//   SecurityHeaders → CSRF → Auth → mux
	var handler http.Handler = authStore.Middleware(mux)
	handler = authStore.CSRFMiddleware(handler)
	handler = auth.SecurityHeadersMiddleware(handler)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		log.Println("Shutting down Clonarr...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Clonarr starting on port %s", port)
	fmt.Printf("[%s] Web UI available at http://localhost:%s\n", time.Now().Format("2006-01-02 15:04:05"), port)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// initAuth loads auth settings from the main config store (JSON), validates,
// loads existing credentials from /config/auth.json, and returns the store
// + handlers ready to wire into the mux.
//
// Refuses to start (log.Fatal) on any unsafe combination (unknown enum
// values) or on malformed auth.json.
func initAuth(ctx context.Context, configStore *configStore) (*auth.Store, *AuthHandlers) {
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
	// Env-var override for trust-boundary config. If the env var is set at
	// process start, that value wins over the config-file value AND the UI
	// cannot change it. Use this in Unraid templates / docker-compose to
	// lock down the trust boundary against UI-takeover attacks (session
	// hijack, local-bypass peer adding themselves to the trust list).
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

	// Periodic loud warning while in none mode. Picks up live-reload
	// transitions both ways.
	safeGo("auth-none-warning", func() {
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

	return store, &AuthHandlers{Store: store}
}

// parsePullInterval parses a pull interval string. Supports Go duration (1h, 30m, 24h).
// Returns 0 to disable. Defaults to 24h if empty.
func parsePullInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 24 * time.Hour
	}
	if s == "0" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("Invalid PULL_INTERVAL %q, using 24h default: %v", s, err)
		return 24 * time.Hour
	}
	if d < time.Minute {
		log.Printf("PULL_INTERVAL %s too short, minimum 1m", s)
		return time.Minute
	}
	return d
}
