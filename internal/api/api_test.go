package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestApp(t *testing.T) *core.App {
	t.Helper()
	tempDir := t.TempDir()

	config := core.NewConfigStore(tempDir)
	// Create a dummy config so the store can load it
	dummyCfg := core.Config{
		DebugLogging: true,
	}
	cfgData, _ := json.MarshalIndent(dummyCfg, "", "  ")
	os.WriteFile(filepath.Join(tempDir, "clonarr.json"), cfgData, 0644)

	if err := config.Load(); err != nil {
		t.Fatalf("Failed to load test config: %v", err)
	}

	app := &core.App{
		Config:   config,
		DebugLog: core.NewDebugLogger(tempDir),
	}
	app.DebugLog.SetEnabled(true)
	return app
}

func TestHandleGetConfig(t *testing.T) {
	app := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	server := &Server{Core: app}
	server.handleGetConfig(w, req)

	res := w.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("Expected status OK, got %v", res.StatusCode)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if val, ok := response["debugLogging"].(bool); !ok || !val {
		t.Errorf("Expected debugLogging to be true, got %v", response["debugLogging"])
	}
}
