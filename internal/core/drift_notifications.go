package core

import (
	"fmt"
	"strings"
)

// DriftChangeSummary carries per-rule context for the drift-detected
// notification — the rule's identity, what diverged, and the count of
// detail entries so the message can name affected CFs without dumping
// every line.
type DriftChangeSummary struct {
	RuleID         string
	InstanceName   string
	ArrProfileName string
	AppType        string // "radarr" | "sonarr"
	Summary        []string
	Details        []DriftDetail
}

// NotifyDriftDetected fires the "Arr-side drift" notification — triggered
// when DriftRunner.RunOnce finds the current Arr profile diverges from
// the rule's target AND the drift signature is different from what was
// last notified about (fingerprint-based dedup happens in the caller).
//
// Dispatches to every notification agent that has OnDriftDetected enabled.
// No agents opted in → cheap no-op.
func (app *App) NotifyDriftDetected(summary DriftChangeSummary) {
	cfg := app.Config.Get()

	hasOptIn := false
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if agent.Events.OnDriftDetected {
			hasOptIn = true
			break
		}
	}
	if !hasOptIn {
		return
	}

	appLabel := summary.AppType
	if appLabel == "radarr" {
		appLabel = "Radarr"
	} else if appLabel == "sonarr" {
		appLabel = "Sonarr"
	}

	title := fmt.Sprintf("Clonarr: drift detected in %s on %s", summary.ArrProfileName, appLabel)

	var lines []string
	lines = append(lines, fmt.Sprintf("Someone edited **%s** directly in %s. Its current state no longer matches what Clonarr would sync.",
		summary.ArrProfileName, summary.InstanceName))
	if len(summary.Summary) > 0 {
		lines = append(lines, "")
		for _, s := range summary.Summary {
			lines = append(lines, "• "+s)
		}
	}
	if len(summary.Details) > 0 {
		const maxShown = 6
		shown := summary.Details
		extra := 0
		if len(shown) > maxShown {
			extra = len(shown) - maxShown
			shown = shown[:maxShown]
		}
		lines = append(lines, "")
		lines = append(lines, "**Changes:**")
		for _, d := range shown {
			lines = append(lines, "• "+formatDriftDetail(d))
		}
		if extra > 0 {
			lines = append(lines, fmt.Sprintf("...and %d more", extra))
		}
	}
	// Closing line reflects what happens next: in "Wait before applying" mode an
	// auto-sync-ON rule will be re-applied automatically after the delay, so say
	// when — not "re-sync manually" (notify mode / auto-sync-OFF rules still do).
	closing := "Open Clonarr, go to Sync Rules to review the changes, then re-sync."
	if cfg.ProfileSync != nil && cfg.ProfileSync.Mode == ProfileSyncModeDelayed && cfg.ProfileSync.ApplyDelayMinutes > 0 {
		ruleAutoSyncs := false
		for _, r := range cfg.AutoSync.Rules {
			if r.ID == summary.RuleID {
				ruleAutoSyncs = r.Enabled && r.OrphanedAt == "" && r.ProfileSource != "imported"
				break
			}
		}
		if ruleAutoSyncs {
			closing = "Auto-sync will put your saved state back in " + humanizeMinutes(cfg.ProfileSync.ApplyDelayMinutes) + "."
		}
	}
	lines = append(lines, "")
	lines = append(lines, closing)

	payload := NotificationPayload{
		Title:    title,
		Message:  strings.Join(lines, "\n"),
		Color:    appColor(summary.AppType), // per-instance app colour
		Severity: NotificationSeverityWarning,
		Route:    NotificationRouteDefault,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnDriftDetected {
			continue
		}
		app.DispatchNotificationAgent(agent, payload)
	}
}

// NotifyDriftReconciled fires when a rule that was previously in drift
// is no longer in drift (user fixed the Arr-side change, or clonarr
// re-synced). Lets the user confirm the issue resolved without checking
// the UI manually.
func (app *App) NotifyDriftReconciled(summary DriftChangeSummary) {
	cfg := app.Config.Get()

	hasOptIn := false
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if agent.Events.OnDriftReconciled {
			hasOptIn = true
			break
		}
	}
	if !hasOptIn {
		return
	}

	appLabel := summary.AppType
	if appLabel == "radarr" {
		appLabel = "Radarr"
	} else if appLabel == "sonarr" {
		appLabel = "Sonarr"
	}

	payload := NotificationPayload{
		Title: fmt.Sprintf("Clonarr: drift resolved on %s for %s", appLabel, summary.ArrProfileName),
		Message: fmt.Sprintf("The earlier drift in **%s** on %s is gone. The profile now matches what Clonarr would sync.",
			summary.ArrProfileName, summary.InstanceName),
		Color:    appColor(summary.AppType), // per-instance app colour
		Severity: NotificationSeverityInfo,
		Route:    NotificationRouteDefault,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnDriftReconciled {
			continue
		}
		app.DispatchNotificationAgent(agent, payload)
	}
}

// NotifyDriftCorrected fires when Apply-automatically mode synced an auto-sync-ON
// rule back over a direct Arr edit. Uses the OnDriftReconciled toggle (it is a
// drift-resolution event) but says clearly that clonarr did the correction, so the
// user never sees a misleading "re-sync manually" for drift that was already fixed.
// Embed uses the Arr app's brand colour.
func (app *App) NotifyDriftCorrected(summary DriftChangeSummary) {
	cfg := app.Config.Get()

	hasOptIn := false
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if agent.Events.OnDriftReconciled {
			hasOptIn = true
			break
		}
	}
	if !hasOptIn {
		return
	}

	appLabel := summary.AppType
	if appLabel == "radarr" {
		appLabel = "Radarr"
	} else if appLabel == "sonarr" {
		appLabel = "Sonarr"
	}

	msg := fmt.Sprintf("**%s** on %s was changed directly in Arr. Auto-sync put it back to your saved state.",
		summary.ArrProfileName, summary.InstanceName)
	if len(summary.Details) > 0 {
		lines := make([]string, 0, len(summary.Details))
		for _, d := range summary.Details {
			if len(lines) >= 20 {
				lines = append(lines, "- ...")
				break
			}
			lines = append(lines, "- "+formatDriftDetail(d))
		}
		msg += "\n\n**Reverted:**\n" + strings.Join(lines, "\n")
	} else if len(summary.Summary) > 0 {
		msg += "\n\n" + strings.Join(summary.Summary, "\n")
	}

	payload := NotificationPayload{
		Title:    fmt.Sprintf("Drift corrected on %s · %s", appLabel, summary.ArrProfileName),
		Message:  msg,
		Color:    appColor(summary.AppType),
		Severity: NotificationSeverityInfo,
		Route:    NotificationRouteDefault,
	}
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if !agent.Events.OnDriftReconciled {
			continue
		}
		app.DispatchNotificationAgent(agent, payload)
	}
}

// formatDriftDetail produces a short human line for one DriftDetail
// entry. CF score diffs read as "FLUX: 100 → 51"; setting diffs read
// as "Cutoff: HDTV-1080p → Bluray-1080p"; quality-allowed diffs read
// as "Bluray-720p allowed: false → true".
func formatDriftDetail(d DriftDetail) string {
	switch d.Field {
	case "score":
		return fmt.Sprintf("%s score: %v → %v", d.CFName, d.Current, d.Target)
	case "quality":
		return fmt.Sprintf("Quality %s allowed: %v → %v", d.CFName, d.Current, d.Target)
	case "group":
		return fmt.Sprintf("Quality %s group: %v → %v", d.CFName, d.Current, d.Target)
	case "upgradeAllowed":
		return fmt.Sprintf("Upgrade allowed: %v → %v", d.Current, d.Target)
	case "cutoff":
		return fmt.Sprintf("Cutoff: %v → %v", d.Current, d.Target)
	case "minFormatScore":
		return fmt.Sprintf("Min Format Score: %v → %v", d.Current, d.Target)
	case "cutoffFormatScore":
		return fmt.Sprintf("Cutoff Format Score: %v → %v", d.Current, d.Target)
	case "minUpgradeFormatScore":
		return fmt.Sprintf("Min Upgrade Format Score: %v → %v", d.Current, d.Target)
	case "language":
		return fmt.Sprintf("Language: %v → %v", d.Current, d.Target)
	default:
		return fmt.Sprintf("%s: %v → %v", d.Field, d.Current, d.Target)
	}
}
