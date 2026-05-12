package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupTrashAPIServerWithCachedData(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()

	cfg := core.NewConfigStore(dir)
	if err := cfg.Set(core.DefaultConfig()); err != nil {
		t.Fatalf("set config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "trash-guides"), 0755); err != nil {
		t.Fatalf("mkdir trash-guides: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trash-guides", "sentinel.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("write repo sentinel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-pull.txt"), []byte("2026-05-11T12:00:00Z"), 0644); err != nil {
		t.Fatalf("write last-pull: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-pull-diff.json"), []byte(`{"prevCommit":"old","newCommit":"abc123","summary":"changed","time":"2026-05-11T12:00:00Z"}`), 0644); err != nil {
		t.Fatalf("write last diff: %v", err)
	}

	app := &core.App{
		Config:       cfg,
		Trash:        core.NewTrashStore(dir),
		DebugLog:     core.NewDebugLogger(dir),
		ActivityLog:  core.NewActivityLogger(dir),
		PullUpdateCh: make(chan string, 1),
	}
	return &Server{Core: app}, dir
}

func TestTrashAPIResetClearsStatus(t *testing.T) {
	server, dir := setupTrashAPIServerWithCachedData(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/trash/status", nil)
	statusW := httptest.NewRecorder()
	mux.ServeHTTP(statusW, statusReq)
	if statusW.Code != http.StatusOK {
		t.Fatalf("initial status = %d, want %d", statusW.Code, http.StatusOK)
	}
	var before core.TrashStatus
	if err := json.NewDecoder(statusW.Body).Decode(&before); err != nil {
		t.Fatalf("decode initial status: %v", err)
	}
	if !before.Cloned || before.CommitHash == "" || before.LastDiff == nil {
		t.Fatalf("initial status was not seeded as cloned: %+v", before)
	}

	resetReq := httptest.NewRequest(http.MethodPost, "/api/trash/reset", nil)
	resetW := httptest.NewRecorder()
	mux.ServeHTTP(resetW, resetReq)
	if resetW.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want %d: %s", resetW.Code, http.StatusOK, resetW.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(resetW.Body).Decode(&body); err != nil {
		t.Fatalf("decode reset body: %v", err)
	}
	if body["status"] != "reset" {
		t.Fatalf("status body = %q, want reset", body["status"])
	}

	statusReq = httptest.NewRequest(http.MethodGet, "/api/trash/status", nil)
	statusW = httptest.NewRecorder()
	mux.ServeHTTP(statusW, statusReq)
	if statusW.Code != http.StatusOK {
		t.Fatalf("post-reset status = %d, want %d", statusW.Code, http.StatusOK)
	}
	var after core.TrashStatus
	if err := json.NewDecoder(statusW.Body).Decode(&after); err != nil {
		t.Fatalf("decode post-reset status: %v", err)
	}
	if after.Cloned || after.CommitHash != "" || after.LastDiff != nil || after.PullError != "" {
		t.Fatalf("post-reset status not cleared: %+v", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "trash-guides")); !os.IsNotExist(err) {
		t.Fatalf("trash-guides was not deleted: %v", err)
	}
}

func TestTrashAPIResetBusyReturnsConflict(t *testing.T) {
	server, dir := setupTrashAPIServerWithCachedData(t)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	marker := filepath.Join(dir, "git-started")
	release := filepath.Join(dir, "git-release")
	fakeGit := filepath.Join(binDir, "git")
	script := "#!/bin/sh\n" +
		": > \"" + marker + "\"\n" +
		"while [ ! -f \"" + release + "\" ]; do sleep 0.05; done\n" +
		"exit 1\n"
	if err := os.WriteFile(fakeGit, []byte(script), 0755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		_ = os.WriteFile(release, []byte("release"), 0644)
	})

	pullReq := httptest.NewRequest(http.MethodPost, "/api/trash/pull", nil)
	pullW := httptest.NewRecorder()
	mux.ServeHTTP(pullW, pullReq)
	if pullW.Code != http.StatusAccepted {
		t.Fatalf("pull status = %d, want %d: %s", pullW.Code, http.StatusAccepted, pullW.Body.String())
	}
	waitForFile(t, marker)

	resetReq := httptest.NewRequest(http.MethodPost, "/api/trash/reset", nil)
	resetW := httptest.NewRecorder()
	mux.ServeHTTP(resetW, resetReq)
	if resetW.Code != http.StatusConflict {
		t.Fatalf("reset status = %d, want %d: %s", resetW.Code, http.StatusConflict, resetW.Body.String())
	}
	if !strings.Contains(resetW.Body.String(), "TRaSH pull/reset already in progress") {
		t.Fatalf("reset body missing busy error: %s", resetW.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "trash-guides")); err != nil {
		t.Fatalf("trash-guides was changed while busy: %v", err)
	}

	if err := os.WriteFile(release, []byte("release"), 0644); err != nil {
		t.Fatalf("release fake git: %v", err)
	}
	waitForNotPulling(t, server)
}

func TestTrashAPIResetSyncInProgressReturnsConflict(t *testing.T) {
	server, dir := setupTrashAPIServerWithCachedData(t)
	if err := server.Core.Config.Update(func(cfg *core.Config) {
		cfg.Instances = append(cfg.Instances, core.Instance{
			ID:     "inst-reset-busy",
			Name:   "Reset Busy",
			Type:   "radarr",
			URL:    "http://127.0.0.1:7878",
			APIKey: "test",
		})
	}); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	mu := server.Core.GetSyncMutex("inst-reset-busy")
	mu.Lock()
	defer mu.Unlock()

	mux := http.NewServeMux()
	server.RegisterRoutes(mux)
	resetReq := httptest.NewRequest(http.MethodPost, "/api/trash/reset", nil)
	resetW := httptest.NewRecorder()
	mux.ServeHTTP(resetW, resetReq)
	if resetW.Code != http.StatusConflict {
		t.Fatalf("reset status = %d, want %d: %s", resetW.Code, http.StatusConflict, resetW.Body.String())
	}
	if !strings.Contains(resetW.Body.String(), "Sync already in progress; try again after it finishes") {
		t.Fatalf("reset body missing sync-busy error: %s", resetW.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "trash-guides")); err != nil {
		t.Fatalf("trash-guides was changed while sync was busy: %v", err)
	}

	st := server.Core.Trash.Status()
	if !st.Cloned || st.CommitHash == "" || st.LastDiff == nil {
		t.Fatalf("status was unexpectedly cleared while sync was busy: %+v", st)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForNotPulling(t *testing.T, server *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if !server.Core.Trash.Status().Pulling {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for pull lock to release")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
