package core

import (
	"fmt"
	"log"
	"time"
)

// RunPullAndSync is the canonical Pull-and-sync flow shared by the manual
// Pull endpoint (handleTrashPull) and the scheduled Profile Sync runner
// (ProfileSyncRunner.runPullAndSync). Single source of truth ensures both
// paths produce identical telemetry: DebugLog op-trace, Trash.SetPullError
// on failure, ProfileSync state persistence, NotifyRepoUpdate when commit
// advanced, AfterPullCallback (server-level helpers like
// AutoSyncQualitySizes), and AutoSyncAfterPull for per-rule sync.
//
// source is the DebugLog trigger string — typically SourceManualPull or
// SourceAutoPullInterval. It also propagates to AutoSyncAfterPull so
// sync-history entries reflect what kicked off the run.
//
// Returns an error only on pull failure. Persist / notify / sync failures
// inside this method are logged but do not abort the flow.
func (app *App) RunPullAndSync(source string) error {
	cfg := app.Config.Get()
	remote := cfg.TrashRepo.URL
	branch := cfg.TrashRepo.Branch
	if remote == "" {
		return fmt.Errorf("pull-and-sync: TRaSH repo URL not configured")
	}
	if branch == "" {
		branch = "master"
	}

	op := app.DebugLog.BeginOp(OpTrash, source, "url="+remote+" branch="+branch)
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	prevCommit := app.Trash.CurrentCommit()
	if err := app.Trash.CloneOrPull(remote, branch); err != nil {
		log.Printf("pull-and-sync: pull failed: %v", err)
		app.DebugLog.Logf(LogError, "TRaSH pull failed: %v", err)
		app.Trash.SetPullError(err.Error())
		// Persist failure to ProfileSync state so /api/profile-sync surfaces it.
		if updErr := app.Config.Update(func(c *Config) {
			if c.ProfileSync == nil {
				return
			}
			c.ProfileSync.LastRun = time.Now().UTC().Format(time.RFC3339)
			c.ProfileSync.LocalHead = prevCommit
			// Leave UpstreamHead alone — last successful value remains the
			// best signal until the next successful pull.
		}); updErr != nil {
			log.Printf("pull-and-sync: persist error-state failed: %v", updErr)
		}
		endResult = "error: pull failed"
		return err
	}

	newCommit := app.Trash.CurrentCommit()
	commitChanged := prevCommit != "" && newCommit != prevCommit

	// Persist post-pull ProfileSync state. Best-effort — persist failures
	// don't abort the rest of the flow.
	if updErr := app.Config.Update(func(c *Config) {
		if c.ProfileSync == nil {
			return
		}
		c.ProfileSync.LastRun = time.Now().UTC().Format(time.RFC3339)
		c.ProfileSync.LocalHead = newCommit
		c.ProfileSync.UpstreamHead = newCommit // post-pull both heads match
	}); updErr != nil {
		log.Printf("pull-and-sync: persist pull-result failed: %v", updErr)
	}

	if commitChanged {
		app.NotifyRepoUpdate(prevCommit, newCommit)
		// Surface what actually changed in the upstream repo so users can
		// verify pulls did what they expected. One summary line per app +
		// up to 15 detail lines, then "...and N more".
		if diff, err := app.Trash.DiffPull(prevCommit, newCommit); err != nil {
			app.DebugLog.Logf(LogTrash, "Pull diff failed: %v (commit %s..%s)", err, shortHash(prevCommit), shortHash(newCommit))
		} else {
			app.DebugLog.Logf(LogTrash, "Pull completed — commit %s → %s", shortHash(prevCommit), shortHash(newCommit))
			if len(diff.Changes) == 0 {
				app.DebugLog.Logf(LogTrash, "No JSON file changes in this commit (only includes/cf-descriptions or other non-data files)")
			} else {
				for _, ap := range []string{"radarr", "sonarr"} {
					if sum := diff.SummaryByApp(ap); sum != "" {
						appLabel := "Radarr"
						if ap == "sonarr" {
							appLabel = "Sonarr"
						}
						app.DebugLog.Logf(LogTrash, "%s — %s", appLabel, sum)
					}
				}
				for _, line := range diff.DetailLines(15) {
					app.DebugLog.Logf(LogTrash, "  %s", line)
				}
			}
		}
	} else if prevCommit == "" {
		app.DebugLog.Logf(LogTrash, "Initial pull completed — commit %s", shortHash(newCommit))
	} else {
		app.DebugLog.Logf(LogTrash, "Pull completed — no upstream changes")
	}

	if app.AfterPullCallback != nil {
		app.AfterPullCallback()
	}
	// Scheduled-sync-only: naming auto-sync writes to Arr, so it runs here (the
	// Pull-and-sync path) but NOT on the data-only Pull (RunPullOnly).
	if app.AfterSyncCallback != nil {
		app.AfterSyncCallback()
	}
	app.DebugLog.Logf(LogAutoSync, "Running auto-sync")
	// AutoSyncAfterPull opens its own AUTOSYNC operation; it is not a
	// child of this TRASH op so the trace clearly separates the pull
	// from the rules it triggers.
	app.AutoSyncAfterPull(source)

	if commitChanged {
		endResult = "ok | new commit " + shortHash(newCommit)
	} else {
		endResult = "ok | no change"
	}
	return nil
}

// RunPullOnly fetches the TRaSH repo without running per-rule auto-sync.
// Used by the user-facing "Pull" button (which is now data-only) and as
// the data-refresh step inside Update All / Update this profile actions
// that explicitly scope which rules get pushed to Arr.
//
// AfterPullCallback (e.g. AutoSyncQualitySizes) still fires — that's
// instance-level quality-size handling, not the per-rule sync work.
func (app *App) RunPullOnly(source string) error {
	cfg := app.Config.Get()
	remote := cfg.TrashRepo.URL
	branch := cfg.TrashRepo.Branch
	if remote == "" {
		return fmt.Errorf("pull-only: TRaSH repo URL not configured")
	}
	if branch == "" {
		branch = "master"
	}

	op := app.DebugLog.BeginOp(OpTrash, source, "url="+remote+" branch="+branch+" mode=pull-only")
	endResult := "error: unknown"
	defer func() { op.End(endResult) }()

	prevCommit := app.Trash.CurrentCommit()
	if err := app.Trash.CloneOrPull(remote, branch); err != nil {
		log.Printf("pull-only: pull failed: %v", err)
		app.DebugLog.Logf(LogError, "TRaSH pull failed: %v", err)
		app.Trash.SetPullError(err.Error())
		if updErr := app.Config.Update(func(c *Config) {
			if c.ProfileSync == nil {
				return
			}
			c.ProfileSync.LastRun = time.Now().UTC().Format(time.RFC3339)
			c.ProfileSync.LocalHead = prevCommit
		}); updErr != nil {
			log.Printf("pull-only: persist error-state failed: %v", updErr)
		}
		endResult = "error: pull failed"
		return err
	}

	newCommit := app.Trash.CurrentCommit()
	commitChanged := prevCommit != "" && newCommit != prevCommit

	if updErr := app.Config.Update(func(c *Config) {
		if c.ProfileSync == nil {
			return
		}
		c.ProfileSync.LastRun = time.Now().UTC().Format(time.RFC3339)
		c.ProfileSync.LocalHead = newCommit
		c.ProfileSync.UpstreamHead = newCommit
	}); updErr != nil {
		log.Printf("pull-only: persist pull-result failed: %v", updErr)
	}

	if commitChanged {
		app.NotifyRepoUpdate(prevCommit, newCommit)
		if diff, err := app.Trash.DiffPull(prevCommit, newCommit); err == nil {
			app.DebugLog.Logf(LogTrash, "Pull completed (no sync) — commit %s → %s", shortHash(prevCommit), shortHash(newCommit))
			for _, ap := range []string{"radarr", "sonarr"} {
				if sum := diff.SummaryByApp(ap); sum != "" {
					appLabel := "Radarr"
					if ap == "sonarr" {
						appLabel = "Sonarr"
					}
					app.DebugLog.Logf(LogTrash, "%s — %s", appLabel, sum)
				}
			}
		}
	} else if prevCommit == "" {
		app.DebugLog.Logf(LogTrash, "Initial pull completed (no sync) — commit %s", shortHash(newCommit))
	} else {
		app.DebugLog.Logf(LogTrash, "Pull completed — no upstream changes")
	}

	if app.AfterPullCallback != nil {
		app.AfterPullCallback()
	}

	if commitChanged {
		endResult = "ok | new commit " + shortHash(newCommit)
	} else {
		endResult = "ok | no change"
	}
	return nil
}
