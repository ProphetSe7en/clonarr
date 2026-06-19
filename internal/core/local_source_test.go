package core

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalSourceLoad locks in phase-2 behavior: with Local Source mode enabled,
// the store parses CF/score data from the local folder (not the git clone) and
// records an empty commit hash (there is no git). The user-edited score in the
// local CF file is what gets loaded.
func TestLocalSourceLoad(t *testing.T) {
	cloneRoot := t.TempDir() // would-be clone parent; intentionally has no .git
	localDir := t.TempDir()  // the local source

	cfDir := filepath.Join(localDir, "docs", "json", "radarr", "cf")
	if err := os.MkdirAll(cfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfJSON := `{
	  "trash_id": "test-id-2-0",
	  "trash_scores": { "sqp-4-ma-hybrid": 157, "default": -10 },
	  "name": "2.0 Stereo",
	  "specifications": [
	    { "name": "Stereo", "implementation": "ReleaseTitleSpecification",
	      "negate": false, "required": true, "fields": { "value": "\\bStereo\\b" } }
	  ]
	}`
	if err := os.WriteFile(filepath.Join(cfDir, "20-stereo.json"), []byte(cfJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := NewTrashStore(cloneRoot)
	ts.SetLocalSource(true, localDir)

	if got := ts.sourceDir(); got != localDir {
		t.Fatalf("sourceDir() = %q, want local %q", got, localDir)
	}

	if err := ts.LoadFromDisk(); err != nil {
		t.Fatalf("LoadFromDisk (local) failed: %v", err)
	}

	snap := ts.Snapshot()
	if snap.CommitHash != "" {
		t.Errorf("CommitHash = %q, want empty in Local mode", snap.CommitHash)
	}
	cf, ok := snap.Radarr.CustomFormats["test-id-2-0"]
	if !ok {
		t.Fatalf("CF test-id-2-0 not parsed from local source; got %d radarr CFs", len(snap.Radarr.CustomFormats))
	}
	if cf.Name != "2.0 Stereo" {
		t.Errorf("CF name = %q, want %q", cf.Name, "2.0 Stereo")
	}
	if cf.TrashScores["sqp-4-ma-hybrid"] != 157 {
		t.Errorf("sqp-4-ma-hybrid score = %d, want 157 (the local file's score must be used)", cf.TrashScores["sqp-4-ma-hybrid"])
	}
}

// TestLocalSourceDisabledUsesCloneDir is the regression guarantee: with Local
// Source disabled (the default), sourceDir() returns the git clone dir, so guide
// mode reads exactly what it read before this feature.
func TestLocalSourceDisabledUsesCloneDir(t *testing.T) {
	ts := NewTrashStore(t.TempDir())
	if ts.LocalSourceEnabled() {
		t.Fatal("Local Source should be disabled by default")
	}
	if ts.sourceDir() != ts.DataDir() {
		t.Errorf("sourceDir() = %q, want clone dir %q when disabled", ts.sourceDir(), ts.DataDir())
	}
	// A configured path while disabled must still resolve to the clone dir.
	ts.SetLocalSource(false, "/some/local/path")
	if ts.sourceDir() != ts.DataDir() {
		t.Errorf("sourceDir() = %q, want clone dir while disabled with a path set", ts.sourceDir())
	}
}

// TestLocalSourceSkipsGit locks in phase-3: the git entry points no-op in Local
// mode (so a pull can never git-reset over the user's local edits, and a bogus
// repo URL is never reached).
func TestLocalSourceSkipsGit(t *testing.T) {
	ts := NewTrashStore(t.TempDir())
	ts.SetLocalSource(true, t.TempDir())

	// CloneOrPull must no-op without running git, even with a URL that would
	// otherwise be cloned/reset.
	if err := ts.CloneOrPull("https://example.invalid/repo.git", "master"); err != nil {
		t.Errorf("CloneOrPull in Local mode should no-op, got error: %v", err)
	}
	// Reset must no-op too — the guide clone is left untouched in Local mode.
	if err := ts.Reset(); err != nil {
		t.Errorf("Reset in Local mode should no-op, got error: %v", err)
	}
}

// TestLocalSourceSeedAndClean locks in phase-4: Seed copies the guide clone's
// docs/json + includes into the local folder (a complete, parseable set), and
// Clean empties it.
func TestLocalSourceSeedAndClean(t *testing.T) {
	cloneRoot := t.TempDir() // NewTrashStore puts the clone at cloneRoot/trash-guides
	cloneCF := filepath.Join(cloneRoot, "trash-guides", "docs", "json", "radarr", "cf")
	if err := os.MkdirAll(cloneCF, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloneCF, "x.json"), []byte(`{"trash_id":"x","name":"X"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	incl := filepath.Join(cloneRoot, "trash-guides", "includes", "cf-descriptions")
	if err := os.MkdirAll(incl, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(incl, "x.md"), []byte("desc"), 0o644)

	ts := NewTrashStore(cloneRoot)
	localDir := filepath.Join(t.TempDir(), "local-source")
	ts.SetLocalSource(true, localDir)

	if err := ts.SeedLocalSource(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localDir, "docs", "json", "radarr", "cf", "x.json")); err != nil {
		t.Errorf("seeded CF missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localDir, "includes", "cf-descriptions", "x.md")); err != nil {
		t.Errorf("seeded include missing: %v", err)
	}
	// The seeded local source parses.
	if err := ts.LoadFromDisk(); err != nil {
		t.Fatalf("load after seed: %v", err)
	}
	if _, ok := ts.Snapshot().Radarr.CustomFormats["x"]; !ok {
		t.Errorf("CF x not parsed from seeded local source")
	}

	if err := ts.CleanLocalSource(); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localDir, "docs")); !os.IsNotExist(err) {
		t.Errorf("docs should be removed after clean, stat err = %v", err)
	}
}
