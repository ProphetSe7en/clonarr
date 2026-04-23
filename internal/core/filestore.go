package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileItem is the constraint for types stored in a FileStore.
// Methods are defined on pointer receivers of the concrete type.
type FileItem interface {
	GetID() string
	GetName() string
	SetName(string)
	GetAppType() string
}

// FileStore[T] provides generic file-backed CRUD for types whose pointer
// implements FileItem. T must be a struct type; the store works with *T
// internally to satisfy the interface.
type FileStore[T any, PT interface {
	*T
	FileItem
}] struct {
	mu  sync.RWMutex
	dir string
}

// NewFileStore creates a FileStore backed by the given directory.
func NewFileStore[T any, PT interface {
	*T
	FileItem
}](dir string) *FileStore[T, PT] {
	return &FileStore[T, PT]{dir: dir}
}

// Add saves one or more items. Skips duplicates (same Name + AppType).
// Returns the number of items actually added.
func (fs *FileStore[T, PT]) Add(items []T) (int, int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := os.MkdirAll(fs.dir, 0755); err != nil {
		return 0, 0, fmt.Errorf("create dir %s: %w", fs.dir, err)
	}

	existing := fs.listLocked("")
	existingKeys := make(map[string]bool, len(existing))
	for i := range existing {
		p := PT(&existing[i])
		existingKeys[p.GetName()+"\x00"+p.GetAppType()] = true
	}

	added := 0
	skipped := 0
	for i := range items {
		p := PT(&items[i])
		if existingKeys[p.GetName()+"\x00"+p.GetAppType()] {
			skipped++
			continue
		}
		if err := fs.writeItem(&items[i]); err != nil {
			return added, skipped, err
		}
		added++
	}
	return added, skipped, nil
}

// List returns all items, optionally filtered by app type.
func (fs *FileStore[T, PT]) List(appType string) []T {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.listLocked(appType)
}

// listLocked reads items from disk. Caller must hold mu.
func (fs *FileStore[T, PT]) listLocked(appType string) []T {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil
	}

	var result []T
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		p := PT(&item)
		if appType == "" || p.GetAppType() == appType {
			result = append(result, item)
		}
	}
	return result
}

// Get returns a single item by ID.
func (fs *FileStore[T, PT]) Get(id string) (T, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		var zero T
		return zero, false
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		if PT(&item).GetID() == id {
			return item, true
		}
	}
	var zero T
	return zero, false
}

// Delete removes an item by ID.
func (fs *FileStore[T, PT]) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", fs.dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(fs.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		if PT(&item).GetID() == id {
			return os.Remove(path)
		}
	}
	return fmt.Errorf("item %s not found", id)
}

// Update replaces an existing item (matched by ID).
func (fs *FileStore[T, PT]) Update(item T) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	p := PT(&item)
	newFilename := sanitizeFilename(p.GetName(), p.GetID()) + ".json"

	// Find and remove old file if it exists under a different name (migration/rename)
	found := false
	entries, err := os.ReadDir(fs.dir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if e.Name() == newFilename {
				found = true
				continue
			}
			path := filepath.Join(fs.dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var existing T
			if err := json.Unmarshal(data, &existing); err != nil {
				continue
			}
			if PT(&existing).GetID() == p.GetID() {
				os.Remove(path) // remove old name-based file
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("item %s not found", p.GetID())
	}

	return fs.writeItem(&item)
}

// writeItem writes a single item to disk. Caller must hold mu.
func (fs *FileStore[T, PT]) writeItem(item *T) error {
	p := PT(item)
	id := p.GetID()

	// Guard against path traversal / invalid IDs ever reaching the filesystem.
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid ID for filesystem storage: %q", id)
	}

	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal item: %w", err)
	}

	// Use sanitized name as filename for readability. Falls back to ID if name is empty.
	// The file contains the full item JSON (including ID), so the filename is purely cosmetic.
	filename := sanitizeFilename(p.GetName(), id) + ".json"
	path := filepath.Join(fs.dir, filename)

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write item: %w", err)
	}
	return os.Rename(tmp, path)
}

// sanitizeFilename converts a name to a safe filesystem filename.
// Strips path separators, .., and special characters. Falls back to ID if name is empty.
func sanitizeFilename(name, id string) string {
	if name == "" {
		name = id
	}
	// Lowercase, replace spaces/special chars with hyphens
	safe := strings.ToLower(name)
	var b strings.Builder
	for _, r := range safe {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	// Collapse multiple hyphens
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	if result == "" {
		result = id
	}
	return result
}

// MigrateFilenames renames files that use ID-based filenames (from the path
// traversal fix) to sanitized name-based filenames. Safe to call on startup —
// skips files that already have the correct name.
func (fs *FileStore[T, PT]) MigrateFilenames() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return 0
	}

	migrated := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(fs.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		p := PT(&item)
		expectedFilename := sanitizeFilename(p.GetName(), p.GetID()) + ".json"
		if e.Name() != expectedFilename {
			newPath := filepath.Join(fs.dir, expectedFilename)
			if err := os.WriteFile(newPath, data, 0644); err == nil {
				os.Remove(path)
				migrated++
			}
		}
	}
	return migrated
}
