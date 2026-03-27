package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileStoreItem is the interface stored types must satisfy.
type FileStoreItem interface {
	GetID() string
	GetName() string
	GetAppType() string
}

// FileStore is a generic JSON-file-per-item store backed by a directory.
type FileStore[T FileStoreItem] struct {
	mu  sync.RWMutex
	dir string
}

// NewFileStore creates a store backed by dir.
func NewFileStore[T FileStoreItem](dir string) *FileStore[T] {
	return &FileStore[T]{dir: dir}
}

// EnsureDir creates the backing directory if it doesn't exist.
func (fs *FileStore[T]) EnsureDir() error {
	return os.MkdirAll(fs.dir, 0755)
}

// List returns all items, optionally filtered by app type (empty = all).
func (fs *FileStore[T]) List(appType string) []T {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.readAll(appType)
}

// Get returns a single item by ID.
func (fs *FileStore[T]) Get(id string) (T, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return fs.findByID(id)
}

// Delete removes an item by ID.
func (fs *FileStore[T]) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path, ok := fs.pathByID(id)
	if !ok {
		return fmt.Errorf("item %s not found", id)
	}
	return os.Remove(path)
}

// Put atomically writes an item. Filename is derived from item name;
// numeric suffix is added on collision with a different ID.
func (fs *FileStore[T]) Put(item T) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.putLocked(item)
}

// Update replaces an existing item (by ID). Cleans up the old file if the name changed.
func (fs *FileStore[T]) Update(item T) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	oldPath, ok := fs.pathByID(item.GetID())
	if !ok {
		return fmt.Errorf("item %s not found", item.GetID())
	}

	newFilename := sanitizeFilename(item.GetName()) + ".json"
	if filepath.Base(oldPath) != newFilename {
		os.Remove(oldPath) // name changed → remove old file
	}

	return fs.putLocked(item)
}

// AddNew saves items that don't already exist (dedups by Name+AppType).
// If assignID is non-nil it is called before writing each new item.
// Returns the count of items actually added.
func (fs *FileStore[T]) AddNew(items []T, assignID func(*T)) (int, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if err := os.MkdirAll(fs.dir, 0755); err != nil {
		return 0, fmt.Errorf("create dir: %w", err)
	}

	existing := fs.readAll("")
	seen := make(map[string]bool, len(existing))
	for _, item := range existing {
		seen[item.GetName()+"\x00"+item.GetAppType()] = true
	}

	added := 0
	for i := range items {
		key := items[i].GetName() + "\x00" + items[i].GetAppType()
		if seen[key] {
			continue
		}
		if assignID != nil {
			assignID(&items[i])
		}
		if err := fs.putLocked(items[i]); err != nil {
			return added, err
		}
		added++
	}
	return added, nil
}

// --- internal helpers (caller must hold appropriate lock) ---

func (fs *FileStore[T]) readAll(appType string) []T {
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
		if appType == "" || item.GetAppType() == appType {
			result = append(result, item)
		}
	}
	return result
}

func (fs *FileStore[T]) findByID(id string) (T, bool) {
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
		if item.GetID() == id {
			return item, true
		}
	}
	var zero T
	return zero, false
}

func (fs *FileStore[T]) pathByID(id string) (string, bool) {
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return "", false
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
		if item.GetID() == id {
			return path, true
		}
	}
	return "", false
}

func (fs *FileStore[T]) putLocked(item T) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	base := sanitizeFilename(item.GetName())
	filename := base + ".json"
	path := filepath.Join(fs.dir, filename)

	// Resolve collision: different item already at this path
	if raw, err := os.ReadFile(path); err == nil {
		var other T
		if json.Unmarshal(raw, &other) == nil && other.GetID() != item.GetID() {
			for i := 2; i < 100; i++ {
				filename = fmt.Sprintf("%s-%d.json", base, i)
				path = filepath.Join(fs.dir, filename)
				if _, err := os.Stat(path); err != nil {
					break
				}
			}
		}
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return os.Rename(tmp, path)
}
