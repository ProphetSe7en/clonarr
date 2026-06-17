package core

import "strings"

// NamingChange describes one naming field that the scheduled auto-sync loop
// re-applied after a TRaSH guide change, for the optional notification.
type NamingChange struct {
	FieldLabel string // human label, e.g. "Movie file"
	Scheme     string // the scheme the field follows, e.g. "plex-imdb"
	OldPattern string // pattern on the instance before this apply ("" if unset)
	NewPattern string // pattern applied (the guide's new pattern for the scheme)
}

// NotifyNamingAutoSync fires the optional "naming auto-sync applied" notification:
// the scheduled loop re-applied a field because TRaSH changed that scheme's
// pattern (old → new). Off by default; dispatches only to agents with
// OnNamingAutoSync. No opt-in → cheap no-op. NOT fired for manual Sync or
// apply-on-enable — those are user-initiated and already shown in the UI.
func (app *App) NotifyNamingAutoSync(instanceName string, changes []NamingChange) {
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

	lines := []string{"A TRaSH guide update changed naming on **" + instanceName + "**. Auto-sync applied:"}
	for _, c := range changes {
		old := c.OldPattern
		if old == "" {
			old = "(not set)"
		}
		lines = append(lines,
			"",
			"**"+c.FieldLabel+"**, following "+c.Scheme,
			"Old: `"+old+"`",
			"New: `"+c.NewPattern+"`",
		)
	}

	payload := NotificationPayload{
		Title:    "Clonarr: naming auto-sync applied on " + instanceName,
		Message:  strings.Join(lines, "\n"),
		Color:    0x3fb950, // accent-green (matches sync-success)
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
