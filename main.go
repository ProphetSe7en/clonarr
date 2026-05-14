package main

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"clonarr/internal/api"
	"clonarr/internal/auth"
	"clonarr/internal/core"
	"clonarr/internal/netsec"
	"clonarr/internal/utils"
	"clonarr/ui"
)

var Version = "dev" // overridden at build time via ldflags

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

	basePath, err := auth.NormalizeBasePath(os.Getenv("URL_BASE"))
	if err != nil {
		log.Fatalf("URL_BASE invalid: %v", err)
	}
	if basePath != "" {
		log.Printf("URL base: %s (serving from this prefix)", basePath)
		// URL_BASE almost always means "behind a reverse proxy"; without
		// TRUSTED_PROXIES set, X-Forwarded-Proto won't be honored, Secure
		// cookies won't be set on HTTPS, and X-Forwarded-For from the proxy
		// will be ignored. Warn at startup so misconfigurations are visible
		// in the container log instead of silently breaking session security.
		if os.Getenv("TRUSTED_PROXIES") == "" {
			log.Printf("WARNING: URL_BASE is set but TRUSTED_PROXIES is empty — Clonarr won't trust X-Forwarded-Proto from your reverse proxy. Set TRUSTED_PROXIES to your proxy's IP so HTTPS Secure cookies and client-IP resolution work correctly.")
		}
	}

	// Initialize stores
	cfgStore := core.NewConfigStore(configDir)
	if err := cfgStore.Load(); err != nil {
		log.Printf("Warning: could not load config: %v", err)
	}

	trashStore := core.NewTrashStore(dataDir)
	profilesStore := core.NewProfileStore(filepath.Join(configDir, "profiles"))
	// Migrate profile filenames at startup so the appType suffix added in
	// PR #28's sanitizeFilename change is applied to existing files. Without
	// this, profiles created before the fix keep their old names and
	// same-name-Radarr-vs-Sonarr collisions stay unresolved on disk.
	if n := profilesStore.MigrateFilenames(); n > 0 {
		log.Printf("profile: migrated %d filenames to name-based", n)
	}
	customCFsStore := core.NewCustomCFStore(filepath.Join(configDir, "custom", "json"))
	customCFsStore.MigrateFromFlatDir(filepath.Join(configDir, "custom-cfs"))
	customCFsStore.MigrateFilenames()
	// One-shot normalization: pre-fix imports stored Radarr's full UI-hint
	// metadata (label/type/advanced/selectOptions) per spec field, which
	// bloated Language-spec CFs to ~50 KB each. Reduce to condensed
	// `{name: value}` object form. Idempotent — already-condensed fields
	// are left alone.
	customCFsStore.NormalizeStoredFields()
	cfGroupsStore := core.NewCFGroupStore(filepath.Join(configDir, "custom", "json"))
	cfGroupsStore.MigrateFilenames()

	// Migrate any imported profiles from old config to per-file storage
	core.MigrateImportedProfiles(cfgStore, profilesStore)

	debugLogStore := core.NewDebugLogger(configDir)
	debugLogStore.SetEnabled(cfgStore.Get().DebugLogging)
	activityLogStore := core.NewActivityLogger(configDir)
	activityLogStore.SetEnabled(cfgStore.Get().DebugLogging)

	// CLONARR_DEV_FEATURES gates contributor-only UI (TRaSH schema fields, Recyclarr
	// import/export). Read once at startup; restart required to change. Not exposed
	// in the Unraid template — must be added manually via Extra Parameters.
	devFeatures := os.Getenv("CLONARR_DEV_FEATURES") == "true"
	if devFeatures {
		log.Printf("CLONARR_DEV_FEATURES=true — contributor features enabled")
	}

	app := &core.App{
		Config:       cfgStore,
		Trash:        trashStore,
		Profiles:     profilesStore,
		CustomCFs:    customCFsStore,
		CFGroups:     cfGroupsStore,
		DebugLog:     debugLogStore,
		ActivityLog:  activityLogStore,
		Version:      Version,
		DevFeatures:  devFeatures,
		HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		NotifyClient: &http.Client{Timeout: 10 * time.Second},
		SafeClient:   netsec.NewSafeHTTPClient(10*time.Second, nil),
		PullUpdateCh: make(chan string, 1),
		SyncUpdateCh: make(chan struct{}, 1),
	}

	// Wire up changelog notification callback
	trashStore.SetOnNewChangelog(func(section core.ChangelogSection) {
		app.NotifyChangelog(section)
	})

	// Startup: clean up broken rules (arrProfileId=0). Historical builds
	// also reset LastSyncCommit here to force every rule to re-evaluate at
	// next pull, but that conflicts with the v2.5.8 intent of skipping
	// auto-sync at restart — the next pull tick would otherwise full-sync
	// every rule even when TRaSH has no new commits, defeating the point.
	// Now: rules keep their LastSyncCommit across restarts. Sync triggers
	// only when (a) TRaSH commit advances since last sync of that rule, or
	// (b) the user enables SyncSchedule for periodic force-resync, or
	// (c) the user runs a manual sync.
	cfgStore.Update(func(cfg *core.Config) {
		cleaned := make([]core.AutoSyncRule, 0, len(cfg.AutoSync.Rules))
		for i := range cfg.AutoSync.Rules {
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
	app.ShutdownCh = ctx.Done()

	// Set up HTTP routes
	mux := http.NewServeMux()
	server := &api.Server{Core: app}
	server.RegisterRoutes(mux)

	// Background: clone/pull TRaSH repo on startup.
	//
	// Respect PullInterval=Disabled when the repo is already cloned: users who
	// explicitly disable pulls don't expect a pull on every container restart.
	// On first run (no .git) we still clone — the app has no CF/profile data
	// otherwise — and we still load the existing on-disk data into memory.
	utils.SafeGo("startup-trash-pull", func() {
		cfg := cfgStore.Get()
		repoCloned := false
		if _, err := os.Stat(filepath.Join(trashStore.DataDir(), ".git")); err == nil {
			repoCloned = true
		}

		// Auto-sync at startup is skipped intentionally. It used to fire here
		// (via app.AutoSyncAfterPull(SourceAutoPullStartup)) but caused
		// 4-AM-Sunday-style collisions when Sonarr/Radarr restarted at the same
		// time. Auto-sync now triggers ONLY on:
		//   - Pull tick that finds new TRaSH commits (interval or scheduled mode)
		//   - Manual user actions (Pull now, Sync All, Sync now, Save & Sync)
		// CleanupStaleRules and MigratePriorAvailableGroups still run at startup
		// since they're maintenance (read-only Arr probe + git history read);
		// neither writes to Arr.
		runStartupMaintenance := func() {
			server.AutoSyncQualitySizes()
			app.CleanupStaleRules()
			app.MigratePriorAvailableGroups()
			app.MigratePriorSyncedCFs()
			app.MigrateExcludedCFs()
		}

		if (cfg.PullInterval == "0" || cfg.PullInterval == "specific") && repoCloned {
			log.Printf("Startup TRaSH pull skipped (interval=%s) — loading existing repo", cfg.PullInterval)
			if err := trashStore.LoadFromDisk(); err != nil {
				log.Printf("Startup TRaSH load failed: %v", err)
				return
			}
			runStartupMaintenance()
			return
		}

		if err := trashStore.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("Startup TRaSH clone/pull failed: %v", err)
		} else {
			runStartupMaintenance()
		}
	})

	// Scheduled TRaSH pull. One goroutine owns either a fixed-interval ticker
	// or a one-shot wall-clock timer; config changes wake it to rebuild from
	// the saved config.
	utils.SafeGo("trash-pull-scheduler", func() {
		var ticker *time.Ticker
		var tickCh <-chan time.Time
		var timer *time.Timer
		var timerCh <-chan time.Time
		var currentInterval time.Duration

		stopSchedule := func() {
			if ticker != nil {
				ticker.Stop()
				ticker = nil
			}
			tickCh = nil
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer = nil
			}
			timerCh = nil
			currentInterval = 0
			app.SetNextPullAt(time.Time{})
		}

		runScheduledPull := func() {
			cfg := cfgStore.Get()
			prevCommit := trashStore.CurrentCommit()
			log.Printf("Scheduled TRaSH pull starting...")
			if err := trashStore.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
				log.Printf("Scheduled TRaSH pull failed: %v", err)
				return
			}

			newCommit := trashStore.CurrentCommit()
			if prevCommit != "" && newCommit != prevCommit {
				log.Printf("TRaSH repo updated: %s → %s", prevCommit, newCommit)
				app.NotifyRepoUpdate(prevCommit, newCommit)
			} else {
				log.Printf("Scheduled TRaSH pull completed (no changes)")
			}
			server.AutoSyncQualitySizes()
			app.AutoSyncAfterPull(core.SourceAutoPullInterval)
		}

		// Track whether we've already warned about the TZ-unset case, so the
		// log doesn't spam on every config-change rearm.
		tzUnsetWarned := false

		armFromConfig := func() {
			stopSchedule()

			cfg := cfgStore.Get()
			if cfg.PullInterval == "specific" {
				if cfg.PullSchedule == nil {
					log.Printf("Scheduled TRaSH pull disabled (specific schedule missing)")
					return
				}
				// One-shot heads-up: if the user picks a wall-clock schedule
				// but TZ is unset, Go falls back to UTC silently and "Daily
				// 03:00" fires at 03:00 UTC. Most container platforms set TZ
				// (Unraid templates ship it by default), but bare-Docker
				// users running without --env TZ=... can be surprised.
				if !tzUnsetWarned && os.Getenv("TZ") == "" {
					log.Printf("warning: pullSchedule uses 'specific' but TZ env var is unset — schedule will fire in UTC. Set TZ in your container (e.g. America/New_York) for local-time scheduling.")
					tzUnsetWarned = true
				}
				next := core.NextPullTime(*cfg.PullSchedule)
				if next.IsZero() {
					log.Printf("Scheduled TRaSH pull disabled (invalid specific schedule)")
					return
				}
				delay := time.Until(next)
				if delay <= 0 {
					// The scheduled time has already passed (typically because
					// the container was down across it). Catch-up: fire on the
					// next loop iteration so the missed pull still runs once.
					// Documented here so the immediate-fire isn't surprising.
					log.Printf("Scheduled TRaSH pull: next time %s is in the past (container was likely down across it) — running catch-up immediately", next.Format(time.RFC3339))
					delay = time.Millisecond
				}
				timer = time.NewTimer(delay)
				timerCh = timer.C
				app.SetNextPullAt(next)
				log.Printf("Scheduled TRaSH pull at %s", next.Format(time.RFC3339))
				return
			}

			interval := core.ParsePullInterval(cfg.PullInterval)
			if interval > 0 {
				ticker = time.NewTicker(interval)
				tickCh = ticker.C
				currentInterval = interval
				next := time.Now().Add(interval)
				app.SetNextPullAt(next)
				log.Printf("Scheduled TRaSH pull every %s (next at %s)", interval, next.Format(time.RFC3339))
				return
			}

			log.Printf("Scheduled TRaSH pull disabled")
		}
		armFromConfig()

		for {
			select {
			case tickAt := <-tickCh:
				runScheduledPull()
				if currentInterval > 0 {
					// Ticker values are scheduled times, so this avoids drifting by the pull duration.
					app.SetNextPullAt(tickAt.Add(currentInterval))
				}
			case <-timerCh:
				runScheduledPull()
				armFromConfig()
			case <-app.PullUpdateCh:
				armFromConfig()
			case <-ctx.Done():
				stopSchedule()
				return
			}
		}
	})

	// ==== Authentication =====================================================
	authStore := api.InitAuth(ctx, cfgStore, Version, basePath, configDir, mux)
	server.AuthStore = authStore

	// Static files
	staticFS, err := fs.Sub(ui.StaticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
	// Render index.html as a template so BasePath can be injected at serve
	// time. "GET /{$}" is an exact-match in Go 1.22+ ServeMux and takes
	// priority over the catch-all "/" for GET / requests. The root template
	// composes the larger UI from partials so feature markup can live in
	// smaller files without changing the rendered page.
	indexTmpl, err := template.New("index.html").ParseFS(
		staticFS,
		"index.html",
		"partials/layout/*.html",
		"partials/sections/*.html",
		"partials/overlays/*.html",
		"partials/modals/*.html",
	)
	if err != nil {
		log.Fatalf("Failed to parse index template: %v", err)
	}
	mux.Handle("GET /{$}", &api.IndexHandler{Tmpl: indexTmpl, BasePath: basePath})
	mux.HandleFunc("/partials/", http.NotFound)
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	// Auto-sync schedule. When the user enables SyncSchedule (separate from
	// PullSchedule — pull = "fetch new TRaSH data", sync = "push my saved
	// settings to Arr"), this goroutine arms a wall-clock timer and fires
	// ForceSyncAllRules at the scheduled instant. Force-sync bypasses the
	// LastSyncCommit short-circuit so it catches Arr-side drift (manual
	// edits, third-party tools) without needing a passive drift detector.
	// Wakes on SyncUpdateCh whenever the config changes.
	utils.SafeGo("auto-sync-scheduler", func() {
		var timer *time.Timer
		var timerCh <-chan time.Time

		stopTimer := func() {
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer = nil
				timerCh = nil
			}
		}

		armFromConfig := func() {
			stopTimer()
			cfg := cfgStore.Get()
			if cfg.SyncSchedule == nil || !cfg.SyncSchedule.Enabled {
				app.SetNextSyncAt(time.Time{})
				log.Printf("Auto-sync schedule disabled")
				return
			}
			next := core.NextSyncTime(*cfg.SyncSchedule)
			if next.IsZero() {
				app.SetNextSyncAt(time.Time{})
				log.Printf("Auto-sync schedule disabled (invalid configuration)")
				return
			}
			delay := time.Until(next)
			if delay <= 0 {
				log.Printf("Auto-sync schedule: next time %s is in the past — running catch-up immediately", next.Format(time.RFC3339))
				delay = time.Millisecond
			}
			timer = time.NewTimer(delay)
			timerCh = timer.C
			app.SetNextSyncAt(next)
			log.Printf("Auto-sync schedule armed for %s", next.Format(time.RFC3339))
		}

		armFromConfig()

		for {
			select {
			case <-timerCh:
				log.Printf("Auto-sync schedule firing — force-syncing all enabled rules")
				app.ForceSyncAllRules()
				armFromConfig() // re-arm for next occurrence
			case <-app.SyncUpdateCh:
				armFromConfig()
			case <-ctx.Done():
				stopTimer()
				return
			}
		}
	})

	// Background: reap expired sessions every 5 min
	utils.SafeGo("session-cleanup", func() {
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
	//   [BasePath] → SecurityHeaders → CSRF → Auth → mux
	// withBasePath wraps the chain only when URL_BASE is set: it 301-redirects
	// the bare base path (no trailing slash) to base/, 404s anything outside
	// the base, and strips the prefix before the inner handlers see the path.
	// Inner handlers keep using root-relative paths (/api/..., /login, etc.).
	var handler http.Handler = authStore.Middleware(mux)
	handler = authStore.CSRFMiddleware(handler)
	handler = auth.SecurityHeadersMiddleware(handler)
	if basePath != "" {
		handler = withBasePath(basePath, handler)
	}

	serverHTTP := &http.Server{
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
		serverHTTP.Shutdown(shutdownCtx)
	}()

	log.Printf("Clonarr starting on port %s", port)
	fmt.Printf("[%s] Web UI available at http://localhost:%s\n", time.Now().Format("2006-01-02 15:04:05"), port)

	if err := serverHTTP.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// withBasePath wraps a handler so it is only reachable under base (e.g.
// "/clonarr"). It:
//   - 301-redirects the bare base path (no trailing slash) → base/
//   - 404s anything whose path does not start with base/
//   - strips the prefix before passing to the inner handler, so inner code
//     continues to work with root-relative paths (/api/..., /login, etc.)
func withBasePath(base string, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == base {
			target := base + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
			return
		}
		if !strings.HasPrefix(r.URL.Path, base+"/") {
			http.NotFound(w, r)
			return
		}
		http.StripPrefix(base, inner).ServeHTTP(w, r)
	})
}
