package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// CFGroup is a user-created TRaSH-style custom-format group, saved locally
// so the user can iterate on it. Fields with JSON tags `name`/`trash_id`/etc.
// match TRaSH's on-disk cf-groups schema exactly; the ID/AppType/timestamps
// are Clonarr-only metadata and are stripped from the downloaded file.
type CFGroup struct {
	// Clonarr-internal fields.
	ID        string `json:"id"`      // synthetic "cfgroup:<hex>"
	AppType   string `json:"appType"` // "radarr" or "sonarr"
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`

	// TRaSH cf-group schema fields.
	Name             string `json:"name"`
	TrashID          string `json:"trash_id"`
	TrashDescription string `json:"trash_description"`
	// "true" when the group is default-on; omitted (empty string → omitempty)
	// when opt-in, matching TRaSH's convention in optional-*.json files.
	Default         string                 `json:"default,omitempty"`
	CustomFormats   []CFGroupCF            `json:"custom_formats"`
	QualityProfiles CFGroupQualityProfiles `json:"quality_profiles"`
}

// CFGroupCF is one custom format entry inside a CFGroup.
type CFGroupCF struct {
	Name     string `json:"name"`
	TrashID  string `json:"trash_id"`
	Required bool   `json:"required"`
}

// CFGroupQualityProfiles mirrors TRaSH's {"include": {"Profile Name": "trashId"}}.
type CFGroupQualityProfiles struct {
	Include map[string]string `json:"include"`
}

// FileItem interface methods for CFGroup (required by FileStore generic).
func (g *CFGroup) GetID() string      { return g.ID }
func (g *CFGroup) GetName() string    { return g.Name }
func (g *CFGroup) SetName(n string)   { g.Name = n }
func (g *CFGroup) GetAppType() string { return g.AppType }

// CFGroupStore manages cf-groups in app-type-scoped subdirectories.
// Layout: {dir}/{appType}/cf-groups/*.json — identical pattern to CustomCFStore
// so same-named groups across Radarr/Sonarr never collide on disk.
type CFGroupStore struct {
	dir    string
	stores map[string]*FileStore[CFGroup, *CFGroup]
}

// NewCFGroupStore creates a CFGroupStore rooted at `dir`. The underlying
// FileStores are created lazily per app-type.
func NewCFGroupStore(dir string) *CFGroupStore {
	s := &CFGroupStore{
		dir:    dir,
		stores: make(map[string]*FileStore[CFGroup, *CFGroup], len(knownAppTypes)),
	}
	for _, appType := range knownAppTypes {
		subdir := filepath.Join(dir, appType, "cf-groups")
		if err := os.MkdirAll(subdir, 0755); err != nil {
			log.Printf("cf-group: failed to create directory %s: %v", subdir, err)
		}
		s.stores[appType] = NewFileStore[CFGroup, *CFGroup](subdir)
	}
	return s
}

func (gs *CFGroupStore) storeFor(appType string) *FileStore[CFGroup, *CFGroup] {
	return gs.stores[appType]
}

// GenerateCFGroupID returns a synthetic id like "cfgroup:<24 hex chars>".
func GenerateCFGroupID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "cfgroup:fallback"
	}
	return "cfgroup:" + hex.EncodeToString(b)
}

// List returns all cf-groups for the given app type. Empty appType returns
// groups across all app types (used internally — UI always filters).
func (gs *CFGroupStore) List(appType string) []CFGroup {
	if appType != "" {
		store := gs.storeFor(appType)
		if store == nil {
			return nil
		}
		return store.List("")
	}
	var result []CFGroup
	for _, store := range gs.stores {
		result = append(result, store.List("")...)
	}
	return result
}

// Add saves a cf-group. Generates the ID server-side (same as CustomCF) so
// a malicious client can't inject path-traversal through the filename.
func (gs *CFGroupStore) Add(g CFGroup) (CFGroup, error) {
	if g.ID == "" {
		g.ID = GenerateCFGroupID()
	}
	store := gs.storeFor(g.AppType)
	if store == nil {
		return CFGroup{}, fmt.Errorf("unknown app type: %s", g.AppType)
	}
	if _, _, err := store.Add([]CFGroup{g}); err != nil {
		return CFGroup{}, err
	}
	return g, nil
}

// Get returns a single cf-group by ID. Searches all app-type stores.
func (gs *CFGroupStore) Get(id string) (CFGroup, bool) {
	for _, store := range gs.stores {
		if g, ok := store.Get(id); ok {
			return g, true
		}
	}
	return CFGroup{}, false
}

// Update replaces an existing cf-group (matched by ID).
func (gs *CFGroupStore) Update(g CFGroup) error {
	store := gs.storeFor(g.AppType)
	if store == nil {
		return fmt.Errorf("unknown app type: %s", g.AppType)
	}
	return store.Update(g)
}

// Delete removes a cf-group by ID. Searches all app-type stores so callers
// don't need to know the appType — mirrors CustomCFStore.Delete.
func (gs *CFGroupStore) Delete(id string) error {
	for _, store := range gs.stores {
		if err := store.Delete(id); err == nil {
			return nil
		}
	}
	return fmt.Errorf("item %s not found", id)
}

// MigrateFilenames is a no-op placeholder — CF groups never had ID-based
// filenames, so there's nothing to migrate. Keeping the method for parity
// with CustomCFStore so main.go can call both unconditionally.
func (gs *CFGroupStore) MigrateFilenames() {
	for appType, store := range gs.stores {
		if n := store.MigrateFilenames(); n > 0 {
			log.Printf("cf-group: migrated %d %s filenames to name-based", n, appType)
		}
	}
}
