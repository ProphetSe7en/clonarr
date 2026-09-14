package api

import (
	"clonarr/internal/core"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleDriftCheckQuery covers the sidebar Check passing the Auto-sync
// toggles: drift=0 must skip the profile/CF drift pass (which records
// DriftWatch.LastRun), and no parameters must keep running it.
func TestHandleDriftCheckQuery(t *testing.T) {
	lastRun := func(app *core.App) string {
		if dw := app.Config.Get().DriftWatch; dw != nil {
			return dw.LastRun
		}
		return ""
	}
	check := func(t *testing.T, app *core.App, target string) {
		t.Helper()
		w := httptest.NewRecorder()
		(&Server{Core: app}).handleDriftCheck(w, httptest.NewRequest(http.MethodPost, target, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want %d (body %s)", target, w.Code, http.StatusOK, w.Body.String())
		}
	}

	t.Run("drift=0 skips the drift pass", func(t *testing.T) {
		app := setupTestApp(t)
		app.DriftRunner = core.NewDriftRunner(app)
		check(t, app, "/api/drift/check?drift=0&namingUpdate=1")
		if got := lastRun(app); got != "" {
			t.Fatalf("DriftWatch.LastRun = %q, want empty (drift pass should not run)", got)
		}
	})

	t.Run("default runs the drift pass", func(t *testing.T) {
		app := setupTestApp(t)
		app.DriftRunner = core.NewDriftRunner(app)
		check(t, app, "/api/drift/check")
		if got := lastRun(app); got == "" {
			t.Fatal("DriftWatch.LastRun is empty, want it set by the drift pass")
		}
	})
}
