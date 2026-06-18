package core

// NamingChange describes one naming field that the scheduled auto-sync loop
// re-applied after a TRaSH guide change, for the optional notification.
type NamingChange struct {
	FieldLabel string // human label, e.g. "Standard Movie Format"
	Scheme     string // the scheme the field follows, e.g. "plex-imdb"
	OldPattern string // pattern on the instance before this apply ("" if unset)
	NewPattern string // pattern applied (the scheme's pattern)
	Reason     string // "update" (TRaSH guide changed) or "drift" (corrected a direct Arr edit)
}

// namingField renders a labelled pattern value as a plain code block (monospace,
// uncoloured — neutral) under its label, for the naming notifications.
func namingField(label, value string) string {
	return label + "\n```\n" + value + "\n```\n"
}

// appColor is the Arr app's brand colour for a notification embed, matching the
// UI's --sonarr-color / --radarr-color.
func appColor(appType string) int {
	if appType == "radarr" {
		return 0xffc230 // Radarr gold
	}
	return 0x35c5f4 // Sonarr blue
}

// namingDiffMarked returns ONLY the changed segment of old → new — the shared
// prefix/suffix are dropped, since the full values are already shown in their own
// fields above. Renders "+ added", "- removed", or "old → new". Neutral; meant for
// a plain code block so it carries no colour.
func namingDiffMarked(oldP, newP string) string {
	p := 0
	for p < len(oldP) && p < len(newP) && oldP[p] == newP[p] {
		p++
	}
	s := 0
	for s < len(oldP)-p && s < len(newP)-p && oldP[len(oldP)-1-s] == newP[len(newP)-1-s] {
		s++
	}
	oldMid := oldP[p : len(oldP)-s]
	newMid := newP[p : len(newP)-s]
	switch {
	case oldMid == "" && newMid == "":
		return "(no change)"
	case oldMid == "":
		return "+" + newMid
	case newMid == "":
		return "-" + oldMid
	default:
		return oldMid + " → " + newMid
	}
}

// NotifyNamingAutoSync fires the optional "naming auto-sync applied" notification:
// the scheduled loop re-applied a field because TRaSH changed that scheme's
// pattern (old → new). Off by default; dispatches only to agents with
// OnNamingAutoSync. No opt-in → cheap no-op. NOT fired for manual Sync or
// apply-on-enable — those are user-initiated and already shown in the UI.
func (app *App) NotifyNamingAutoSync(instanceName, appType string, changes []NamingChange) {
	if len(changes) == 0 {
		return
	}
	cfg := app.Config.Get()

	hasOptIn := false
	for _, agent := range cfg.AutoSync.NotificationAgents {
		if agent.Events.OnNamingAutoSync {
			hasOptIn = true
			break
		}
	}
	if !hasOptIn {
		return
	}

	// One notification per changed format. The wording reflects WHY the field was
	// re-applied — a TRaSH guide update, or correction of a direct Arr edit (drift).
	// Full old/new render in plain code blocks; the Diff shows only the changed
	// segment (the full values are already above). Embed uses the Arr app colour.
	for _, c := range changes {
		old := c.OldPattern
		if old == "" {
			old = "(not set)"
		}
		var title, intro, oldLabel, newLabel string
		if c.Reason == "drift" {
			title = "Naming drift corrected · " + c.FieldLabel
			intro = "**" + c.FieldLabel + "** was changed directly in **" + instanceName + "**; auto-sync put it back to " + c.Scheme + ":"
			oldLabel, newLabel = "Was", "Restored to"
		} else {
			title = "Naming updated · " + c.FieldLabel
			intro = "A TRaSH guide update changed **" + c.FieldLabel + "** on **" + instanceName + "** (following " + c.Scheme + "):"
			oldLabel, newLabel = "Old name", "Updated"
		}
		msg := intro + "\n" +
			namingField(oldLabel, old) +
			namingField(newLabel, c.NewPattern) +
			"Diff\n```\n" + namingDiffMarked(c.OldPattern, c.NewPattern) + "\n```"
		payload := NotificationPayload{
			Title:    title,
			Message:  msg,
			Color:    appColor(appType),
			Severity: NotificationSeverityInfo,
			Route:    NotificationRouteDefault,
		}
		for _, agent := range cfg.AutoSync.NotificationAgents {
			if !agent.Events.OnNamingAutoSync {
				continue
			}
			app.DispatchNotificationAgent(agent, payload)
		}
	}
}

// NotifyNamingDriftDetected fires the Arr-drift notification for naming: a field
// that auto-syncs was changed directly in Arr, so it no longer matches the scheme.
// Reuses the existing "Arr drift detected" (OnDriftDetected) event so drift on
// profiles, custom formats, and naming all land under one toggle. One notification
// per format; no opt-in → cheap no-op. Flag only — never rewrites Arr.
func (app *App) NotifyNamingDriftDetected(events []namingDriftEvent) {
	if len(events) == 0 {
		return
	}
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

	for _, ev := range events {
		for _, f := range ev.Fields {
			msg := "Naming was changed directly in **" + ev.InstanceName + "**, so it no longer matches what clonarr applied:\n\n" +
				"**" + NamingFieldLabel(f.Field) + "**\n" +
				namingField("Now", f.Current)
			if f.Expected != "" {
				msg += namingField("Should be", f.Expected) +
					"Diff\n```\n" + namingDiffMarked(f.Current, f.Expected) + "\n```\n"
			}
			msg += "\nUse Sync in clonarr to re-apply."

			payload := NotificationPayload{
				Title:    "Naming drift · " + NamingFieldLabel(f.Field),
				Message:  msg,
				Color:    appColor(ev.AppType),
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
	}
}
