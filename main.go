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
	}

	// Wire up changelog notification callback
	trashStore.SetOnNewChangelog(func(section core.ChangelogSection) {
		app.NotifyChangelog(section)
	})

	// Startup: reset auto-sync commit hashes so all rules re-evaluate on next pull.
	cfgStore.Update(func(cfg *core.Config) {
		cleaned := make([]core.AutoSyncRule, 0, len(cfg.AutoSync.Rules))
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

		if cfg.PullInterval == "0" && repoCloned {
			log.Printf("Startup TRaSH pull skipped (interval disabled) — loading existing repo")
			if err := trashStore.LoadFromDisk(); err != nil {
				log.Printf("Startup TRaSH load failed: %v", err)
				return
			}
			server.AutoSyncQualitySizes()
			app.AutoSyncAfterPull(core.SourceAutoPullStartup)
			return
		}

		if err := trashStore.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("Startup TRaSH clone/pull failed: %v", err)
		} else {
			server.AutoSyncQualitySizes()
			app.AutoSyncAfterPull(core.SourceAutoPullStartup)
		}
	})

	// Scheduled TRaSH pull
	utils.SafeGo("trash-pull-scheduler", func() {
		cfg := cfgStore.Get()
		interval := core.ParsePullInterval(cfg.PullInterval)
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
				cfg := cfgStore.Get()
				prevCommit := trashStore.CurrentCommit()
				log.Printf("Scheduled TRaSH pull starting...")
				if err := trashStore.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
					log.Printf("Scheduled TRaSH pull failed: %v", err)
				} else {
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
			case newInterval := <-app.PullUpdateCh:
				setTicker(core.ParsePullInterval(newInterval))
			case <-ctx.Done():
				if ticker != nil {
					ticker.Stop()
				}
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
