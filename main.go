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
	"clonarr/internal/core/calculator"
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
	// Local Source mode: read guide data from a local folder instead of the git
	// clone. Default path is <data>/local-source (sibling to trash-guides).
	// Disabled by default, so sourceDir() returns the clone dir and guide mode is
	// unchanged. The Settings handler re-applies this when the toggle changes.
	{
		ls := cfgStore.Get().LocalSource
		lsPath := ls.Path
		if lsPath == "" {
			lsPath = filepath.Join(dataDir, "local-source")
		}
		trashStore.SetLocalSource(ls.Enabled, lsPath)
	}
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

	sandboxStore := core.NewSandboxStore(configDir)

	// Scoring Generator sessions (back-solve CF scores from a ranked release
	// list). Gated in the UI by Advanced Mode, same as the Scoring Sandbox.
	calcStore := calculator.NewStore(filepath.Join(configDir, "calculator-sessions"))

	app := &core.App{
		Config:            cfgStore,
		Trash:             trashStore,
		Profiles:          profilesStore,
		CustomCFs:         customCFsStore,
		CFGroups:          cfGroupsStore,
		Sandbox:           sandboxStore,
		CalcSessions:      calcStore,
		DebugLog:          debugLogStore,
		ActivityLog:       activityLogStore,
		Version:           Version,
		DevFeatures:       devFeatures,
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
		NotifyClient:  &http.Client{Timeout: 10 * time.Second},
		SafeClient:    netsec.NewSafeHTTPClient(10*time.Second, nil),
		PullUpdateCh:  make(chan string, 1),
		ApplyUpdateCh: make(chan struct{}, 1),
	}

	// Wire up changelog notification callback
	trashStore.SetOnNewChangelog(func(section core.ChangelogSection) {
		app.NotifyChangelog(section)
	})

	// Watch & Drift Phase 2a — ProfileSyncRunner polls TRaSH upstream for new
	// commits between scheduled Pulls. Hourly internal cadence; user controls
	// only the on/off toggle via Settings → Pull section.
	app.ProfileSyncRunner = core.NewProfileSyncRunner(app)
	app.DriftRunner = core.NewDriftRunner(app)

	// Startup: clean up broken rules (arrProfileId=0). Historical builds
	// also reset LastSyncCommit here to force every rule to re-evaluate at
	// next pull, but that conflicts with the v2.5.8 intent of skipping
	// auto-sync at restart — the next pull tick would otherwise full-sync
	// every rule even when TRaSH has no new commits, defeating the point.
	// Now: rules keep their LastSyncCommit across restarts. Sync triggers
	// only when (a) TRaSH commit advances since last sync of that rule, or
	// (b) Arr-side drift is detected (Auto-sync detection), or
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

	// Wire post-pull callback: ProfileSyncRunner.runPullAndSync calls this
	// after CloneOrPull succeeds, so server-level helpers (which live in api
	// package and aren't reachable from core) still run on every scheduled
	// pull. Same call-site as today's pull-scheduler closure.
	// QS auto-sync runs on every pull (incl. the data-only Pull) — unchanged.
	app.AfterPullCallback = server.AutoSyncQualitySizes
	// Naming auto-sync writes to Arr, so it runs ONLY on the scheduled Pull-and-sync
	// (+ the naming card's Update all), never on the data-only Pull button.
	app.AfterSyncCallback = server.AutoSyncNaming

	// Background: clone/pull TRaSH repo on startup.
	//
	// Respect PullInterval=Disabled when the repo is already cloned: users who
	// explicitly disable pulls don't expect a pull on every container restart.
	// On first run (no .git) we still clone — the app has no CF/profile data
	// otherwise — and we still load the existing on-disk data into memory.
	utils.SafeGo("startup-trash-pull", func() {
		cfg := cfgStore.Get()

		// Local Source mode: load guide data from the local folder, no git. Skip
		// the clone/pull AND the git-dependent startup maintenance (commit-replay
		// migrations). The data comes from sourceDir(); commit hash stays empty.
		if trashStore.LocalSourceEnabled() {
			log.Printf("Local Source mode — loading guide data from the local folder (no git pull)")
			if err := trashStore.LoadFromDisk(); err != nil {
				log.Printf("Local source load failed: %v", err)
			}
			return
		}

		repoCloned := false
		if _, err := os.Stat(filepath.Join(trashStore.DataDir(), ".git")); err == nil { // #nosec G703 -- DataDir is server-side config, not request input; constant ".git" suffix; read-only Stat
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
			// Naming auto-sync is intentionally NOT run at startup — it follows
			// the pull schedule (AfterPullCallback) + manual actions only, so a
			// restart never re-touches naming. A guide change that landed while
			// the container was down is picked up at the next scheduled pull.
			app.CleanupStaleRules()
			app.MigratePriorAvailableGroups()
			app.MigratePriorSyncedCFs()
			app.MigrateExcludedCFs()
			// v3 — convert the deprecated global AutoSync.Paused flag
			// to per-instance Instance.AutoSyncPaused. Idempotent.
			app.MigrateGlobalPauseToInstances()
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

	// (Legacy trash-pull-scheduler goroutine retired in Phase B commit 2.
	// Scheduled Pull-and-sync now runs through the profile-sync-watcher
	// goroutine below, which reads ProfileSync.Interval/Specific and
	// dispatches to ProfileSyncRunner.runPullAndSync when Mode=auto.)

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
	mux.Handle("GET /{$}", &api.IndexHandler{Tmpl: indexTmpl, BasePath: basePath, Version: Version})
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

	// Profile Sync watcher — fires on ProfileSync.Interval (user-configurable
	// via /api/profile-sync). One-shot timer pattern: arm based on current
	// config, fire once, recompute next, re-arm. Handles:
	//   Interval == "0" / "" → Manual only (no automatic fire; manual button
	//                          via /api/profile-sync/run will land in Phase B)
	//   Interval == "specific" → wall-clock via core.NextPullTime
	//   Interval == <duration> → recurring (1m minimum via ParsePullInterval clamp)
	//
	// Phase B commit 2: this is the SOLE scheduler for Profile Sync. The
	// legacy trash-pull-scheduler goroutine has been retired. PullUpdateCh
	// wakes the loop on user config changes so cadence edits take effect
	// without waiting for the 60s poll-tick fallback.
	utils.SafeGo("profile-sync-watcher", func() {
		var timer *time.Timer
		var timerCh <-chan time.Time
		// armedAt + armedDuration track the current timer's expected fire time
		// so the poll-tick can detect when the user has changed Interval to a
		// shorter value and re-arm immediately instead of waiting for the
		// existing long timer to expire.
		var armedAt time.Time
		var armedDuration time.Duration
		// One-shot TZ warning: when Interval="specific" but TZ env is empty,
		// Go falls back to UTC silently. Unraid templates ship TZ; bare-Docker
		// users without --env TZ=... can be surprised by "Daily 03:00" firing
		// at 03:00 UTC. Warn at first arm so the misconfig is visible.
		var tzUnsetWarned bool

		stopTimer := func() {
			if timer != nil {
				timer.Stop()
				timer = nil
			}
			timerCh = nil
			armedDuration = 0
		}

		// Compute delay until next fire. Returns (0, false) to mean
		// "Manual mode — don't arm a timer". Defense-in-depth: any path
		// that yields a non-positive duration returns false so we never
		// busy-loop on time.NewTimer(0).
		nextDelay := func() (time.Duration, bool) {
			cfg := cfgStore.Get()
			if cfg.ProfileSync == nil {
				return 0, false
			}
			switch cfg.ProfileSync.Interval {
			case "", "0":
				return 0, false
			case "specific":
				if cfg.ProfileSync.Specific == nil {
					return 0, false
				}
				if !tzUnsetWarned && os.Getenv("TZ") == "" {
					log.Printf("WARNING: profile-sync uses wall-clock schedule but TZ env var is unset — fires in UTC. Set TZ in your container (e.g. America/New_York) for local-time scheduling.")
					tzUnsetWarned = true
				}
				next := core.NextPullTime(*cfg.ProfileSync.Specific)
				if next.IsZero() {
					return 0, false
				}
				d := time.Until(next)
				if d < time.Millisecond {
					// Scheduled time already passed (typically because the
					// container was down across it). Fire catch-up immediately.
					log.Printf("profile-sync: next scheduled time %s is in the past — firing catch-up now", next.Format(time.RFC3339))
					d = time.Millisecond
				}
				return d, true
			default:
				d := core.ParsePullInterval(cfg.ProfileSync.Interval)
				if d <= 0 {
					// Defense-in-depth: ParsePullInterval can return 0 for
					// "0"/"specific" (already handled above) but also if a
					// future caller passes whitespace or other edge input.
					// Refusing to arm prevents a tight busy-loop on
					// time.NewTimer(0).
					return 0, false
				}
				return d, true
			}
		}

		rearm := func() {
			stopTimer()
			// rearm() returns to manual mode (no timer) when nextDelay reports
			// !armed — e.g. when user toggled Interval to "0" mid-fire.
			d, armed := nextDelay()
			if !armed {
				app.SetNextPullAt(time.Time{}) // /api/trash/status countdown clears
				return
			}
			timer = time.NewTimer(d)
			timerCh = timer.C
			armedAt = time.Now()
			armedDuration = d
			app.SetNextPullAt(armedAt.Add(d))
		}

		// Lightweight poll picks up config changes the wake channel might
		// miss (e.g. wall-clock schedules that don't push to PullUpdateCh).
		// 60s resolution is invisible at typical detection cadences.
		pollTick := time.NewTicker(60 * time.Second)
		defer pollTick.Stop()
		defer stopTimer()
		defer app.SetNextPullAt(time.Time{})

		rearm() // initial arm based on migrated config

		for {
			select {
			case <-ctx.Done():
				return
			case <-app.PullUpdateCh:
				// User changed config via API — re-arm from the new state.
				rearm()
			case <-pollTick.C:
				d, armed := nextDelay()
				switch {
				case !armed && timer != nil:
					stopTimer()
					app.SetNextPullAt(time.Time{})
				case armed && timer == nil:
					rearm()
				case armed && timer != nil:
					// User changed cadence. Compute remaining on current
					// timer and compare to new desired delay. Re-arm if the
					// new delay is shorter — otherwise let the current timer
					// run out (avoids resetting the timer on every poll for
					// no functional gain).
					remaining := armedDuration - time.Since(armedAt)
					if d < remaining {
						rearm()
					}
				}
			case <-timerCh:
				if err := app.ProfileSyncRunner.Run(ctx); err != nil {
					log.Printf("profile-sync-watcher: %v", err)
				}
				rearm() // schedule next fire
			}
		}
	})

	// Delayed-apply scheduler — only does work when Profile Sync Mode is
	// "delayed" ("Wait before applying"). Detection still runs on the
	// profile-sync-watcher above (populating pendingChanges + notifying);
	// THIS goroutine applies each rule ApplyDelayMinutes after THAT rule's
	// changes were first detected (a per-rule debounce, not a fixed cadence).
	//
	// No timer to arm/reset: the deadline is computed from each rule's
	// persisted pendingChange DetectedAt, so the delay survives restarts for
	// free (a 7-day delay with 5 days elapsed still has 2 days left at boot;
	// nothing fires immediately on start). We just poll on a fixed tick and
	// let RunDelayedApply decide what's due. The wake channel collapses the
	// poll latency when the user changes Mode/delay in Settings.
	utils.SafeGo("delayed-apply-scheduler", func() {
		// 30s poll resolution — fine-grained enough that a 5-minute delay
		// fires within ~30s of its true deadline, cheap enough to ignore.
		pollTick := time.NewTicker(30 * time.Second)
		defer pollTick.Stop()
		defer app.SetNextApplyAt(time.Time{})

		runOnce := func() {
			next := app.RunDelayedApply()
			server.RunNamingDelayed() // naming side of "wait before applying"
			app.SetNextApplyAt(next)  // zero clears the countdown when nothing pending
		}
		runOnce() // evaluate any already-due rules at startup (respecting their persisted deadlines)

		for {
			select {
			case <-ctx.Done():
				return
			case <-app.ApplyUpdateCh:
				runOnce()
			case <-pollTick.C:
				runOnce()
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
