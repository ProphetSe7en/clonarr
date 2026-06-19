package core

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultLocalPath is the default Local Source folder: a sibling of the git
// clone under the config dir (<config>/local-source). Used when the user enables
// Local mode without specifying a path.
func (ts *TrashStore) DefaultLocalPath() string {
	return filepath.Join(filepath.Dir(ts.dataDir), "local-source")
}

// localSourcePath returns the configured local source path (may be empty).
func (ts *TrashStore) localSourcePath() string {
	ts.localMu.RLock()
	defer ts.localMu.RUnlock()
	return ts.localPath
}

// SeedLocalSource copies the current guide clone's docs/json + includes into the
// local source folder, so the user starts with a complete, identical set to edit
// (then overwrites the CF files they want). Requires the guide clone to be
// present (pulled at least once). Overwrites existing files at the destination.
func (ts *TrashStore) SeedLocalSource() error {
	dst := ts.localSourcePath()
	if dst == "" {
		return fmt.Errorf("local source path not configured")
	}
	if !ts.pullMu.TryLock() {
		return ErrTrashBusy
	}
	defer ts.pullMu.Unlock()

	// The clone must have data to copy.
	if _, err := os.Stat(filepath.Join(ts.dataDir, "docs", "json")); err != nil {
		return fmt.Errorf("guide clone has no docs/json to seed from (pull the guide first)")
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create local source dir: %w", err)
	}
	// docs/json is required; includes is best-effort (sparse checkouts may omit it).
	if err := copyDir(filepath.Join(ts.dataDir, "docs", "json"), filepath.Join(dst, "docs", "json")); err != nil {
		return fmt.Errorf("seed docs/json: %w", err)
	}
	if _, err := os.Stat(filepath.Join(ts.dataDir, "includes")); err == nil {
		if err := copyDir(filepath.Join(ts.dataDir, "includes"), filepath.Join(dst, "includes")); err != nil {
			return fmt.Errorf("seed includes: %w", err)
		}
	}
	// Also copy docs/updates.txt if present, so the changelog parses (optional).
	if _, err := os.Stat(filepath.Join(ts.dataDir, "docs", "updates.txt")); err == nil {
		_ = copyFile(filepath.Join(ts.dataDir, "docs", "updates.txt"), filepath.Join(dst, "docs", "updates.txt"))
	}
	return nil
}

// CleanLocalSource empties the local source folder (docs + includes). The folder
// itself is left in place so a re-seed can repopulate it.
func (ts *TrashStore) CleanLocalSource() error {
	dst := ts.localSourcePath()
	if dst == "" {
		return fmt.Errorf("local source path not configured")
	}
	if !ts.pullMu.TryLock() {
		return ErrTrashBusy
	}
	defer ts.pullMu.Unlock()

	for _, sub := range []string{"docs", "includes"} {
		if err := os.RemoveAll(filepath.Join(dst, sub)); err != nil {
			return fmt.Errorf("clean %s: %w", sub, err)
		}
	}
	return nil
}

// copyDir recursively copies the tree at src into dst (creating dst).
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single file, creating parent dirs as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src) // #nosec G304 -- src is a server-side guide-clone path, not request input
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst) // #nosec G304 -- dst is under the server-side local-source path
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
