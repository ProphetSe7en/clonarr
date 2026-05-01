package api

import (
	"bytes"
	"clonarr/internal/arr"
	"clonarr/internal/core"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo runs `git init` + a stub commit in dir. LoadFromDisk
// requires a .git directory + valid HEAD; without this the parse path
// short-circuits before reading any CF JSON.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "init", "-q"},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "test"},
		{"git", "-C", dir, "config", "commit.gpgsign", "false"},
		{"git", "-C", dir, "commit", "--allow-empty", "-q", "-m", "init"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

// setupTestAppWithCFs builds an App with a CustomCFStore + a TrashStore
// pre-populated by writing real CF JSON files into the TRaSH data dir
// and calling LoadFromDisk. Going through the actual file-loading path
// (rather than reaching into private fields) keeps these tests honest:
// they exercise the same code that production uses.
//
// trashCFsByApp maps app type → list of (trashID, name) pairs for the
// TRaSH-published CFs to seed. customCFs are added to the custom store
// directly via Add().
func setupTestAppWithCFs(t *testing.T, trashCFsByApp map[string][][2]string, customCFs []core.CustomCF) *core.App {
	t.Helper()
	tempDir := t.TempDir()

	// Config — minimal, just enough for the handlers to load.
	config := core.NewConfigStore(tempDir)
	dummyCfg := core.Config{}
	cfgData, _ := json.MarshalIndent(dummyCfg, "", "  ")
	os.WriteFile(filepath.Join(tempDir, "clonarr.json"), cfgData, 0644)
	if err := config.Load(); err != nil {
		t.Fatalf("Load config: %v", err)
	}

	// TRaSH data — real JSON files in {tempDir}/trash-guides/docs/json/{app}/cf/.
	// LoadFromDisk parses these into AppData.CustomFormats keyed by trash_id.
	// LoadFromDisk requires a .git dir, so we init an empty repo first.
	trashDir := filepath.Join(tempDir, "trash-guides")
	if err := os.MkdirAll(trashDir, 0755); err != nil {
		t.Fatalf("mkdir trashDir: %v", err)
	}
	initGitRepo(t, trashDir)
	trash := core.NewTrashStore(tempDir)
	for app, cfs := range trashCFsByApp {
		cfDir := filepath.Join(trashDir, "docs", "json", app, "cf")
		if err := os.MkdirAll(cfDir, 0755); err != nil {
			t.Fatalf("mkdir cfDir: %v", err)
		}
		for _, pair := range cfs {
			trashID, name := pair[0], pair[1]
			data := fmt.Sprintf(`{"trash_id":%q,"name":%q}`, trashID, name)
			fpath := filepath.Join(cfDir, trashID+".json")
			if err := os.WriteFile(fpath, []byte(data), 0644); err != nil {
				t.Fatalf("write trash CF: %v", err)
			}
		}
	}
	if err := trash.LoadFromDisk(); err != nil {
		t.Fatalf("LoadFromDisk: %v", err)
	}

	// Custom CF store — Add() handles ID generation if missing.
	customStore := core.NewCustomCFStore(filepath.Join(tempDir, "custom"))
	if len(customCFs) > 0 {
		if _, err := customStore.Add(customCFs); err != nil {
			t.Fatalf("seed custom CFs: %v", err)
		}
	}

	return &core.App{
		Config:    config,
		Trash:     trash,
		CustomCFs: customStore,
		DebugLog:  core.NewDebugLogger(tempDir),
	}
}

// postCustomCF wraps the create handler with a single-CF body. A
// minimal one-spec body is included so the request passes the
// no-conditions validation; tests that want to exercise spec-level
// validation use postCustomCFWithSpecs directly.
func postCustomCF(t *testing.T, server *Server, name, appType string) *httptest.ResponseRecorder {
	t.Helper()
	return postCustomCFWithSpecs(t, server, name, appType, []arr.ArrSpecification{
		{Name: "default", Implementation: "ReleaseTitleSpecification"},
	})
}

// postCustomCFWithSpecs lets tests pass a custom specifications slice.
// Used to exercise the new conditions-validation paths (empty list,
// whitespace-only spec name, etc.) without changing the happy-path
// helper that the existing collision tests rely on.
func postCustomCFWithSpecs(t *testing.T, server *Server, name, appType string, specs []arr.ArrSpecification) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]any{
		"cfs": []core.CustomCF{{Name: name, AppType: appType, Specifications: specs}},
	}
	bodyBytes, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/custom-cfs", bytes.NewReader(bodyBytes))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleCreateCustomCFs(w, r)
	return w
}

// putCustomCF wraps the update handler.
func putCustomCF(t *testing.T, server *Server, id, newName, appType string) *httptest.ResponseRecorder {
	t.Helper()
	body := core.CustomCF{
		Name:    newName,
		AppType: appType,
		// Minimal valid spec — same convention as postCustomCF — so
		// the conditions-validation path passes for tests that are
		// only exercising rename/collision logic.
		Specifications: []arr.ArrSpecification{
			{Name: "default", Implementation: "ReleaseTitleSpecification"},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, "/api/custom-cfs/"+id, bytes.NewReader(bodyBytes))
	r.SetPathValue("id", id)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleUpdateCustomCF(w, r)
	return w
}

func decodeTestJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v (body=%q)", err, w.Body.String())
	}
	return m
}

// --- Create-path tests ---

func TestCreateCustomCF_AllowsTrashNameMatch(t *testing.T) {
	// User is free to name a custom CF the same as a TRaSH-published CF.
	// We don't dictate naming. The cross-usage flip-flop risk (TRaSH+custom
	// with the same name in different profiles syncing to the same Arr)
	// is detected at sync-plan time, not at create time.
	app := setupTestAppWithCFs(t, map[string][][2]string{
		"radarr": {{"abc123", "PCOK"}},
	}, nil)
	server := &Server{Core: app}

	w := postCustomCF(t, server, "PCOK", "radarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

func TestCreateCustomCF_RejectsCustomCollision(t *testing.T) {
	app := setupTestAppWithCFs(t, nil, []core.CustomCF{
		{Name: "MyFormat", AppType: "radarr", Category: "Custom"},
	})
	server := &Server{Core: app}

	w := postCustomCF(t, server, "MyFormat", "radarr")

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	body := decodeTestJSON(t, w)
	if body["code"] != "name_collision_existing" {
		t.Errorf("code = %v, want name_collision_existing", body["code"])
	}
}

func TestCreateCustomCF_AllowsUniqueName(t *testing.T) {
	app := setupTestAppWithCFs(t, map[string][][2]string{
		"radarr": {{"abc123", "PCOK"}},
	}, []core.CustomCF{
		{Name: "OtherCF", AppType: "radarr", Category: "Custom"},
	})
	server := &Server{Core: app}

	w := postCustomCF(t, server, "MyUniqueName", "radarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

func TestCreateCustomCF_AllowsSameNameDifferentAppType(t *testing.T) {
	// TRaSH ships "PCOK" for radarr. A sonarr custom named "PCOK"
	// is fine — different on-disk dirs, different Arr instances.
	app := setupTestAppWithCFs(t, map[string][][2]string{
		"radarr": {{"abc123", "PCOK"}},
	}, nil)
	server := &Server{Core: app}

	w := postCustomCF(t, server, "PCOK", "sonarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

func TestCreateCustomCF_CaseSensitiveDistinct(t *testing.T) {
	// Match Arr's own rule: byte-exact case-sensitive. TRaSH "PCOK"
	// and a custom "Pcok" are distinct names — Arr accepts both.
	app := setupTestAppWithCFs(t, map[string][][2]string{
		"radarr": {{"abc123", "PCOK"}},
	}, nil)
	server := &Server{Core: app}

	w := postCustomCF(t, server, "Pcok", "radarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

// --- Update-path tests ---

func TestUpdateCustomCF_AllowsRenameToOwnName(t *testing.T) {
	app := setupTestAppWithCFs(t, nil, []core.CustomCF{
		{Name: "MyCF", AppType: "radarr", Category: "Custom"},
	})
	// Discover the generated ID.
	cfs := app.CustomCFs.List("radarr")
	if len(cfs) != 1 {
		t.Fatalf("expected 1 seeded CF, got %d", len(cfs))
	}
	id := cfs[0].ID
	server := &Server{Core: app}

	w := putCustomCF(t, server, id, "MyCF", "radarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

func TestUpdateCustomCF_AllowsRenameToTrashName(t *testing.T) {
	// Renaming a custom CF to a name that matches a TRaSH-published CF
	// is allowed. Common case: user wants to drop the `!` prefix that
	// v2.4 forced onto their customs and revert to their original name,
	// which may share a name with a TRaSH CF. We don't block — they
	// own the naming choice.
	app := setupTestAppWithCFs(t, map[string][][2]string{
		"radarr": {{"abc123", "PCOK"}},
	}, []core.CustomCF{
		{Name: "!PCOK", AppType: "radarr", Category: "Custom"},
	})
	cfs := app.CustomCFs.List("radarr")
	id := cfs[0].ID
	server := &Server{Core: app}

	w := putCustomCF(t, server, id, "PCOK", "radarr")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}

func TestUpdateCustomCF_RejectsRenameToOtherCustomName(t *testing.T) {
	app := setupTestAppWithCFs(t, nil, []core.CustomCF{
		{Name: "FirstCF", AppType: "radarr", Category: "Custom"},
		{Name: "SecondCF", AppType: "radarr", Category: "Custom"},
	})
	cfs := app.CustomCFs.List("radarr")
	if len(cfs) != 2 {
		t.Fatalf("expected 2 seeded CFs, got %d", len(cfs))
	}
	// Pick the FirstCF and try renaming it to SecondCF.
	var firstID string
	for _, c := range cfs {
		if c.Name == "FirstCF" {
			firstID = c.ID
			break
		}
	}
	server := &Server{Core: app}

	w := putCustomCF(t, server, firstID, "SecondCF", "radarr")

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
	body := decodeTestJSON(t, w)
	if body["code"] != "name_collision_existing" {
		t.Errorf("code = %v, want name_collision_existing", body["code"])
	}
}

// --- In-batch duplicate test ---

func TestCreateCustomCF_RejectsBatchDuplicates(t *testing.T) {
	// Two entries in the same request both named "Foo" — the existing
	// in-batch check should fire before either is persisted.
	app := setupTestAppWithCFs(t, nil, nil)
	server := &Server{Core: app}

	specs := []arr.ArrSpecification{{Name: "default", Implementation: "ReleaseTitleSpecification"}}
	body := map[string]any{
		"cfs": []core.CustomCF{
			{Name: "Foo", AppType: "radarr", Specifications: specs},
			{Name: "Foo", AppType: "radarr", Specifications: specs},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/custom-cfs", bytes.NewReader(bodyBytes))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleCreateCustomCFs(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%q)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "name_collision_batch") {
		t.Errorf("expected name_collision_batch code in body, got %q", w.Body.String())
	}
}

// --- Specification validation tests ---

func TestCreateCustomCF_RejectsEmptySpecsList(t *testing.T) {
	// A CF with no conditions can't match anything in Arr — Arr returns
	// "specifications are required" on sync. Catch it at create time so
	// the user gets immediate feedback instead of a confusing 400 later.
	app := setupTestAppWithCFs(t, nil, nil)
	server := &Server{Core: app}

	w := postCustomCFWithSpecs(t, server, "EmptyCF", "radarr", nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%q)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no conditions") {
		t.Errorf("expected 'no conditions' message, got %q", w.Body.String())
	}
}

func TestCreateCustomCF_RejectsBlankSpecName(t *testing.T) {
	// Whitespace-only spec name slips past simple length checks but Arr
	// rejects it with "Condition name(s) cannot be empty or consist of
	// only spaces". This test pins the early-rejection behaviour so the
	// user can't accidentally save an unsyncable CF.
	app := setupTestAppWithCFs(t, nil, nil)
	server := &Server{Core: app}

	w := postCustomCFWithSpecs(t, server, "BadSpec", "radarr", []arr.ArrSpecification{
		{Name: "   ", Implementation: "ReleaseTitleSpecification"},
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%q)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no name") {
		t.Errorf("expected 'no name' message, got %q", w.Body.String())
	}
}

func TestCreateCustomCF_AllowsValidSpec(t *testing.T) {
	app := setupTestAppWithCFs(t, nil, nil)
	server := &Server{Core: app}

	w := postCustomCFWithSpecs(t, server, "GoodSpec", "radarr", []arr.ArrSpecification{
		{Name: "Match WEB-DL", Implementation: "ReleaseTitleSpecification"},
	})

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (body=%q)", w.Code, w.Body.String())
	}
}
