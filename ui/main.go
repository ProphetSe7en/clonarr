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

type App struct {
	config        *configStore
	trash         *trashStore
	profiles      *profileStore
	customCFs     *customCFStore
	debugLog      *debugLogger
	pullUpdateCh  chan string // send new interval string to reschedule pull
	cleanupEvents []CleanupEvent
	cleanupMu     sync.Mutex
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
	customCFs := newCustomCFStore(filepath.Join(configDir, "custom-cfs"))

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
	go func() {
		cfg := config.Get()
		if err := trash.CloneOrPull(cfg.TrashRepo.URL, cfg.TrashRepo.Branch); err != nil {
			log.Printf("Startup TRaSH clone/pull failed: %v", err)
		} else {
			app.autoSyncQualitySizes()
			app.autoSyncAfterPull()
		}
	}()

	// Scheduled TRaSH pull (reads interval from config, supports live rescheduling)
	go func() {
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
	}()

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
	mux.HandleFunc("GET /api/trash/{app}/cf-groups", app.handleTrashCFGroups)
	mux.HandleFunc("GET /api/trash/{app}/profiles", app.handleTrashProfiles)
	mux.HandleFunc("GET /api/trash/{app}/profiles/{id}", app.handleTrashProfileDetail)
	mux.HandleFunc("GET /api/trash/{app}/quality-sizes", app.handleTrashQualitySizes)
	mux.HandleFunc("GET /api/trash/{app}/naming", app.handleTrashNaming)

	// Import
	mux.HandleFunc("POST /api/import/profile", app.handleImportProfile)
	mux.HandleFunc("GET /api/import/{app}/profiles", app.handleGetImportedProfiles)
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
	mux.HandleFunc("DELETE /api/instances/{id}/sync-history/{arrProfileId}", app.handleDeleteSyncHistory)

	// Auto-Sync
	mux.HandleFunc("GET /api/auto-sync/settings", app.handleGetAutoSyncSettings)
	mux.HandleFunc("PUT /api/auto-sync/settings", app.handleSaveAutoSyncSettings)
	mux.HandleFunc("GET /api/auto-sync/rules", app.handleListAutoSyncRules)
	mux.HandleFunc("POST /api/auto-sync/rules", app.handleCreateAutoSyncRule)
	mux.HandleFunc("PUT /api/auto-sync/rules/{id}", app.handleUpdateAutoSyncRule)
	mux.HandleFunc("DELETE /api/auto-sync/rules/{id}", app.handleDeleteAutoSyncRule)

	// Cleanup events
	mux.HandleFunc("GET /api/cleanup-events", app.handleCleanupEvents)

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

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create static file system: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFS)))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
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
