package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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

	// Initialize stores
	cfgStore := core.NewConfigStore(configDir)
	if err := cfgStore.Load(); err != nil {
		log.Printf("Warning: could not load config: %v", err)
	}

	trashStore := core.NewTrashStore(dataDir)
	profilesStore := core.NewProfileStore(filepath.Join(configDir, "profiles"))
	customCFsStore := core.NewCustomCFStore(filepath.Join(configDir, "custom", "json"))
	customCFsStore.MigrateFromFlatDir(filepath.Join(configDir, "custom-cfs"))
	customCFsStore.MigrateFilenames()
	cfGroupsStore := core.NewCFGroupStore(filepath.Join(configDir, "custom", "json"))
	cfGroupsStore.MigrateFilenames()

	// Migrate any imported profiles from old config to per-file storage
	core.MigrateImportedProfiles(cfgStore, profilesStore)

	debugLogStore := core.NewDebugLogger(configDir)
	debugLogStore.SetEnabled(cfgStore.Get().DebugLogging)

	app := &core.App{
		Config:       cfgStore,
		Trash:        trashStore,
		Profiles:     profilesStore,
		CustomCFs:    customCFsStore,
		CFGroups:     cfGroupsStore,
		DebugLog:     debugLogStore,
		Version:      Version,
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

	// Background: clone/pull TRaSH repo on startup
	utils.SafeGo("startup-trash-pull", func() {
		cfg := cfgStore.Get()
		if err := trashStore.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("Startup TRaSH clone/pull failed: %v", err)
		} else {
			server.AutoSyncQualitySizes()
			app.AutoSyncAfterPull()
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
					app.AutoSyncAfterPull()
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
	authStore := api.InitAuth(ctx, cfgStore, Version, mux)
	server.AuthStore = authStore

	// Static files
	staticFS, err := fs.Sub(ui.StaticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
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
	//   SecurityHeaders → CSRF → Auth → mux
	var handler http.Handler = authStore.Middleware(mux)
	handler = authStore.CSRFMiddleware(handler)
	handler = auth.SecurityHeadersMiddleware(handler)

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
