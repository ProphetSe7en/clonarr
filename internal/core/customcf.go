package core

import (
	"clonarr/internal/arr"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CustomCF represents a user-imported or user-created custom format not found in TRaSH guides.
type CustomCF struct {
	ID       string `json:"id"` // synthetic ID: "custom:<hex>"
	Name     string `json:"name"`
	AppType  string `json:"appType"`  // "radarr" or "sonarr"
	Category string `json:"category"` // user-chosen category (default: "Custom")

	// CF definition
	IncludeInRename bool                   `json:"includeInRename,omitempty"`
	ArrID           int                    `json:"arrId,omitempty"`
	Specifications  []arr.ArrSpecification `json:"specifications,omitempty"`

	// TRaSH schema fields (visible only when contributor fields are enabled).
	TrashID     string         `json:"trashId,omitempty"`
	TrashScores map[string]int `json:"trashScores,omitempty"`
	Description string         `json:"description,omitempty"`

	// Source info
	SourceInstance string `json:"sourceInstance,omitempty"` // instance name it was imported from
	ImportedAt     string `json:"importedAt,omitempty"`     // RFC3339
}

// FileItem interface methods for CustomCF.
func (cf *CustomCF) GetID() string      { return cf.ID }
func (cf *CustomCF) GetName() string    { return cf.Name }
func (cf *CustomCF) SetName(n string)   { cf.Name = n }
func (cf *CustomCF) GetAppType() string { return cf.AppType }

// CustomCFStore manages custom CFs in app-type-scoped subdirectories.
// Files are stored in {dir}/{appType}/cf/ to avoid cross-app name collisions.
// Same-named CFs in different apps (e.g. "MyLQ" in both Radarr and Sonarr)
// are stored in separate directories and never collide.
type CustomCFStore struct {
	dir    string
	stores map[string]*FileStore[CustomCF, *CustomCF]
}

var knownAppTypes = []string{"radarr", "sonarr"}

func NewCustomCFStore(dir string) *CustomCFStore {
	s := &CustomCFStore{
		dir:    dir,
		stores: make(map[string]*FileStore[CustomCF, *CustomCF], len(knownAppTypes)),
	}
	for _, appType := range knownAppTypes {
		subdir := filepath.Join(dir, appType, "cf")
		if err := os.MkdirAll(subdir, 0755); err != nil {
			log.Printf("custom-cf: failed to create directory %s: %v", subdir, err)
		}
		s.stores[appType] = NewFileStore[CustomCF, *CustomCF](subdir)
	}
	return s
}

func (cs *CustomCFStore) storeFor(appType string) *FileStore[CustomCF, *CustomCF] {
	return cs.stores[appType]
}

// MigrateFilenames renames ID-based filenames to sanitized name-based filenames on startup.
func (cs *CustomCFStore) MigrateFilenames() {
	for appType, store := range cs.stores {
		if n := store.MigrateFilenames(); n > 0 {
			log.Printf("custom-cf: migrated %d %s filenames to name-based", n, appType)
		}
	}
}

// NormalizeSpecFields takes a json.RawMessage that may be either Arr's
// array form (with full UI metadata: order/label/type/advanced/selectOptions
// per field) or already-condensed object form ({name: value, ...}), and
// returns the condensed object form. Idempotent — already-object input
// is returned unchanged.
//
// Arr's API returns each spec's fields as an array of UI-hint objects,
// e.g. for a LanguageSpecification:
//
//	[{"order":0,"name":"value","label":"Language","value":12,"type":"select",
//	  "advanced":false,"selectOptions":[{"value":-1,"name":"Any",...}, ... 60 entries ...]},
//	 {"order":1,"name":"exceptLanguage","label":"Except Language","value":false,
//	  "type":"checkbox","advanced":false,"selectOptions":[]}]
//
// The condensed form drops everything except name → value, producing:
//
//	{"value":12,"exceptLanguage":false}
//
// Functionally identical (sync engine + UI editor parse both forms via
// arrSpecToEditorSpec on the frontend), but ~50KB → ~50 bytes for
// LanguageSpecifications because of the embedded selectOptions list.
//
// On any parse failure or unrecognized shape, returns the input as-is —
// safe fallback that preserves data.
func NormalizeSpecFields(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	// Try array form first.
	var asArray []map[string]any
	if err := json.Unmarshal(raw, &asArray); err == nil {
		condensed := make(map[string]any, len(asArray))
		for _, f := range asArray {
			name, ok := f["name"].(string)
			if !ok || name == "" {
				continue
			}
			condensed[name] = f["value"]
		}
		out, err := json.Marshal(condensed)
		if err != nil {
			return raw
		}
		return out
	}
	// Already object form (or some other shape) — return unchanged.
	return raw
}

// NormalizeStoredFields scans every custom CF on disk and rewrites any
// specification's `fields` from Arr-array form to condensed object form.
// One-shot migration — idempotent (already-object fields are no-ops).
//
// Logs a single summary line per app-type when changes happen, silent
// when everything is already normalized.
func (cs *CustomCFStore) NormalizeStoredFields() {
	for appType, store := range cs.stores {
		if store == nil {
			continue
		}
		all := store.List("")
		var changedCFs, totalSavedBytes int
		for _, cf := range all {
			specs := cf.Specifications
			anyChange := false
			oldSize := 0
			newSize := 0
			for i := range specs {
				before := specs[i].Fields
				oldSize += len(before)
				after := NormalizeSpecFields(before)
				newSize += len(after)
				if !bytesEqual(before, after) {
					specs[i].Fields = after
					anyChange = true
				}
			}
			if anyChange {
				cf.Specifications = specs
				if err := store.Update(cf); err != nil {
					log.Printf("custom-cf: normalize %s/%s — save failed: %v", appType, cf.Name, err)
					continue
				}
				changedCFs++
				totalSavedBytes += oldSize - newSize
			}
		}
		if changedCFs > 0 {
			log.Printf("custom-cf: normalized fields on %d %s custom CF(s) — saved %d bytes", changedCFs, appType, totalSavedBytes)
		}
	}
}

func bytesEqual(a, b json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func GenerateCustomID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "custom:fallback"
	}
	return "custom:" + hex.EncodeToString(b)
}

// List returns custom CFs filtered by app type. If appType is empty, returns all.
func (cs *CustomCFStore) List(appType string) []CustomCF {
	if appType != "" {
		store := cs.storeFor(appType)
		if store == nil {
			return nil
		}
		return store.List("")
	}
	var result []CustomCF
	for _, store := range cs.stores {
		result = append(result, store.List("")...)
	}
	return result
}

// Add saves one or more custom CFs. Generates IDs for items that don't have one.
// Skips duplicates (same Name within same app type). Returns the number added.
func (cs *CustomCFStore) Add(cfs []CustomCF) (int, error) {
	for i := range cfs {
		if cfs[i].ID == "" {
			cfs[i].ID = GenerateCustomID()
		}
	}
	byApp := make(map[string][]CustomCF)
	for _, cf := range cfs {
		byApp[cf.AppType] = append(byApp[cf.AppType], cf)
	}
	total := 0
	for appType, items := range byApp {
		store := cs.storeFor(appType)
		if store == nil {
			continue
		}
		n, _, err := store.Add(items)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// Get returns a single custom CF by ID, searching all app-type stores.
func (cs *CustomCFStore) Get(id string) (CustomCF, bool) {
	for _, store := range cs.stores {
		if cf, ok := store.Get(id); ok {
			return cf, true
		}
	}
	return CustomCF{}, false
}

// Delete removes a custom CF by ID, searching all app-type stores.
func (cs *CustomCFStore) Delete(id string) error {
	for _, store := range cs.stores {
		if err := store.Delete(id); err == nil {
			return nil
		}
	}
	return fmt.Errorf("item %s not found", id)
}

// Update replaces an existing custom CF.
// Note: does not handle cross-app-type moves. If a CF's appType changes,
// the old file remains as an orphan. The UI prevents this (appType is read-only
// during edit), but direct API calls could trigger it.
func (cs *CustomCFStore) Update(cf CustomCF) error {
	store := cs.storeFor(cf.AppType)
	if store == nil {
		return fmt.Errorf("unknown app type: %s", cf.AppType)
	}
	return store.Update(cf)
}

// migrateFromFlatDir migrates custom CFs from the old flat /config/custom-cfs/
// directory to the new app-type-scoped structure ({dir}/{appType}/cf/).
// Strips " (N)" suffixes that were added by the old cross-app collision handling.
// Idempotent — no-op if old directory doesn't exist.
func (cs *CustomCFStore) MigrateFromFlatDir(oldDir string) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return // old dir doesn't exist — nothing to migrate
	}

	var migrated int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(oldDir, e.Name()))
		if err != nil {
			log.Printf("custom-cf migration: failed to read %s: %v", e.Name(), err)
			continue
		}
		var cf CustomCF
		if err := json.Unmarshal(data, &cf); err != nil {
			log.Printf("custom-cf migration: failed to parse %s: %v", e.Name(), err)
			continue
		}

		// Strip "(N)" collision suffix from name
		cf.Name = stripNumericSuffix(cf.Name)

		store := cs.storeFor(cf.AppType)
		if store == nil {
			log.Printf("custom-cf migration: skipping %s (unknown appType %q)", e.Name(), cf.AppType)
			continue
		}

		if n, _, err := store.Add([]CustomCF{cf}); err != nil {
			log.Printf("custom-cf migration: failed to migrate %s: %v", e.Name(), err)
		} else if n > 0 {
			migrated++
		}
	}

	if migrated > 0 {
		log.Printf("custom-cf migration: migrated %d CFs from %s to app-scoped directories", migrated, oldDir)
	}

	// Remove old flat directory
	if err := os.RemoveAll(oldDir); err != nil {
		log.Printf("custom-cf migration: failed to remove old dir %s: %v", oldDir, err)
	} else {
		log.Printf("custom-cf migration: removed old directory %s", oldDir)
	}
}

// stripNumericSuffix removes " (N)" collision suffix from a name.
// e.g. "!PL LQ (2)" → "!PL LQ", "!PL WEB Tier 01 (3)" → "!PL WEB Tier 01"
func stripNumericSuffix(name string) string {
	idx := strings.LastIndex(name, " (")
	if idx < 0 {
		return name
	}
	rest := name[idx+2:]
	if !strings.HasSuffix(rest, ")") {
		return name
	}
	numStr := rest[:len(rest)-1]
	if _, err := strconv.Atoi(numStr); err != nil {
		return name
	}
	return name[:idx]
}
