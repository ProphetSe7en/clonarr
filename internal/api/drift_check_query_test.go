package api

import (
	"clonarr/internal/core"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleDriftCheckNamingUpdateOnly covers the sidebar Check with only
// "TRaSH-Guides updates" turned on: drift=0 must skip the profile drift pass
// and still answer with an empty result list.
func TestHandleDriftCheckNamingUpdateOnly(t *testing.T) {
	app := setupTestApp(t)
	app.DriftRunner = core.NewDriftRunner(app)
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPost, "/api/drift/check?drift=0&namingUpdate=1", nil)
	w := httptest.NewRecorder()

	server.handleDriftCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
	}
}
