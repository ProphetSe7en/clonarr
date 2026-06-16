package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newBackupTestStore builds a ConfigStore in a temp dir with explicit backup
// tuning so the rotation/throttle/dedup paths are testable without waiting on
// the real 5-minute interval.
func newBackupTestStore(t *testing.T, keep int, minInterval time.Duration) *ConfigStore {
	t.Helper()
	dir := t.TempDir()
	return &ConfigStore{
		config:            DefaultConfig(),
		filePath:          filepath.Join(dir, "clonarr.json"),
		backupKeep:        keep,
		backupMinInterval: minInterval,
	}
}

func countConfigBackups(t *testing.T, cs *ConfigStore) int {
	t.Helper()
	dir := filepath.Join(filepath.Dir(cs.filePath), "backups", "clonarr")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "clonarr-") && strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

func save(t *testing.T, cs *ConfigStore) {
	t.Helper()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if err := cs.saveLocked(); err != nil {
		t.Fatalf("saveLocked: %v", err)
	}
}

// Distinct content each save (defeats dedup) → rotation prunes to backupKeep.
func TestConfigBackup_WritesAndRotates(t *testing.T) {
	cs := newBackupTestStore(t, 3, 0)
	for i := 0; i < 5; i++ {
		cs.config.PullInterval = fmt.Sprintf("interval-%d", i)
		save(t, cs)
	}
	if n := countConfigBackups(t, cs); n != 3 {
		t.Errorf("rotation: want 3 backups (keep=3), got %d", n)
	}
}

// Identical content across saves → only the first backup is written.
func TestConfigBackup_DedupSkipsIdentical(t *testing.T) {
	cs := newBackupTestStore(t, 10, 0)
	cs.config.PullInterval = "fixed"
	for i := 0; i < 3; i++ {
		save(t, cs)
	}
	if n := countConfigBackups(t, cs); n != 1 {
		t.Errorf("dedup: want 1 backup for identical saves, got %d", n)
	}
}

// A second (different) save within the throttle window is skipped.
func TestConfigBackup_ThrottleSkipsRecent(t *testing.T) {
	cs := newBackupTestStore(t, 10, time.Hour)
	cs.config.PullInterval = "a"
	save(t, cs)
	cs.config.PullInterval = "b" // different content, but inside the throttle
	save(t, cs)
	if n := countConfigBackups(t, cs); n != 1 {
		t.Errorf("throttle: want 1 backup within interval, got %d", n)
	}
}

// A backup failure must never fail the underlying config save.
func TestConfigBackup_FailureDoesNotFailSave(t *testing.T) {
	dir := t.TempDir()
	// Put a regular file where the backups dir needs to be, so MkdirAll of
	// <dir>/backups/clonarr fails (ENOTDIR) and the backup is skipped.
	if err := os.WriteFile(filepath.Join(dir, "backups"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	cs := &ConfigStore{
		config:            DefaultConfig(),
		filePath:          filepath.Join(dir, "clonarr.json"),
		backupKeep:        10,
		backupMinInterval: 0,
	}
	cs.mu.Lock()
	err := cs.saveLocked()
	cs.mu.Unlock()
	if err != nil {
		t.Errorf("saveLocked must succeed even when the backup fails: %v", err)
	}
	if _, err := os.Stat(cs.filePath); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}
