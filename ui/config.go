package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/mitchellh/copystructure"
)

// Config holds the full application configuration, persisted to JSON.
type Config struct {
	Instances            []Instance                       `json:"instances"`
	TrashRepo            TrashRepo                        `json:"trashRepo"`
	PullInterval         string                           `json:"pullInterval"` // Go duration (e.g. "24h", "1h", "0" to disable)
	DevMode              bool                             `json:"devMode"`      // Enable TRaSH developer tools (TRaSH JSON export)
	DebugLogging         bool                             `json:"debugLogging"` // Write detailed operations to /config/debug.log
	QualitySizeOverrides map[string]map[string]QSOverride `json:"qualitySizeOverrides,omitempty"` // instanceID → quality name → override
	QualitySizeAutoSync  map[string]QSAutoSync             `json:"qualitySizeAutoSync,omitempty"`  // instanceID → auto-sync settings
	SyncHistory          []SyncHistoryEntry               `json:"syncHistory,omitempty"`
	CleanupKeep          map[string][]string              `json:"cleanupKeep,omitempty"` // instanceID → CF names to keep during delete-all
	AutoSync             AutoSyncConfig                   `json:"autoSync,omitempty"`
	Prowlarr             ProwlarrConfig                   `json:"prowlarr,omitempty"`
}

// ProwlarrConfig holds Prowlarr connection settings for the Scoring Sandbox.
type ProwlarrConfig struct {
	URL     string `json:"url"`
	APIKey  string `json:"apiKey"`
	Enabled bool   `json:"enabled"`
}

// AutoSyncConfig holds global auto-sync settings and rules.
type AutoSyncConfig struct {
	Enabled            bool           `json:"enabled"`
	NotifyOnSuccess    bool           `json:"notifyOnSuccess"`
	NotifyOnFailure    bool           `json:"notifyOnFailure"`
	NotifyOnRepoUpdate bool           `json:"notifyOnRepoUpdate"` // Discord notification when TRaSH repo has new commits
	DiscordWebhook     string         `json:"discordWebhook,omitempty"`
	Rules              []AutoSyncRule `json:"rules,omitempty"`
}

// AutoSyncRule defines one auto-sync binding (profile → instance).
type AutoSyncRule struct {
	ID                string         `json:"id"`
	Enabled           bool           `json:"enabled"`
	InstanceID        string         `json:"instanceId"`
	ProfileSource     string         `json:"profileSource"`               // "trash" or "imported"
	TrashProfileID    string         `json:"trashProfileId,omitempty"`
	ImportedProfileID string         `json:"importedProfileId,omitempty"`
	ArrProfileID      int            `json:"arrProfileId"`                // target Arr profile to update
	SelectedCFs       []string       `json:"selectedCFs,omitempty"`       // user's optional CF selections
	Behavior          *SyncBehavior  `json:"behavior,omitempty"`          // sync behavior rules (nil = defaults)
	Overrides         *SyncOverrides `json:"overrides,omitempty"`         // user overrides (min score, language, cutoff, etc.)
	LastSyncCommit    string         `json:"lastSyncCommit,omitempty"`
	LastSyncTime      string         `json:"lastSyncTime,omitempty"`
	LastSyncError     string         `json:"lastSyncError,omitempty"`
}

// SyncBehavior controls how the sync engine handles CF additions, score overrides, and removals.
type SyncBehavior struct {
	AddMode    string `json:"addMode"`    // "add_missing" (default), "add_new", "do_not_add"
	RemoveMode string `json:"removeMode"` // "remove_custom" (default), "allow_custom"
	ResetMode  string `json:"resetMode"`  // "reset_to_zero" (default), "do_not_adjust"
}

// DefaultSyncBehavior returns sync behavior matching current (pre-feature) defaults.
func DefaultSyncBehavior() SyncBehavior {
	return SyncBehavior{
		AddMode:    "add_missing",
		RemoveMode: "remove_custom",
		ResetMode:  "reset_to_zero",
	}
}

// ResolveSyncBehavior returns a fully populated SyncBehavior, filling in defaults for empty fields.
func ResolveSyncBehavior(b *SyncBehavior) SyncBehavior {
	if b == nil {
		return DefaultSyncBehavior()
	}
	r := *b
	if r.AddMode == "" {
		r.AddMode = "add_missing"
	}
	if r.RemoveMode == "" {
		r.RemoveMode = "remove_custom"
	}
	if r.ResetMode == "" {
		r.ResetMode = "reset_to_zero"
	}
	return r
}

// QSAutoSync stores auto-sync settings for quality sizes per instance.
type QSAutoSync struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"` // quality size type: "movie", "series", "sqp-streaming", etc.
}

// QSOverride stores a per-quality custom size override for quality size sync.
type QSOverride struct {
	Min       float64 `json:"min"`
	Preferred float64 `json:"preferred"`
	Max       float64 `json:"max"`
}

// SyncHistoryEntry records a completed sync operation.
type SyncHistoryEntry struct {
	InstanceID     string            `json:"instanceId"`
	ProfileTrashID string            `json:"profileTrashId"`
	ProfileName    string            `json:"profileName"`
	ArrProfileID   int               `json:"arrProfileId"`
	ArrProfileName string            `json:"arrProfileName"`
	SyncedCFs      []string          `json:"syncedCFs"`
	SelectedCFs    map[string]bool   `json:"selectedCFs,omitempty"`
	Overrides      *SyncOverrides    `json:"overrides,omitempty"`
	Behavior       *SyncBehavior     `json:"behavior,omitempty"`
	CFsCreated     int               `json:"cfsCreated"`
	CFsUpdated     int               `json:"cfsUpdated"`
	ScoresUpdated  int               `json:"scoresUpdated"`
	LastSync       string            `json:"lastSync"`
}

// Instance represents a configured Radarr or Sonarr instance.
type Instance struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // "radarr" or "sonarr"
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
}

// TrashRepo holds TRaSH Guides repository settings.
type TrashRepo struct {
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

// DefaultConfig returns a new Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Instances: []Instance{},
		TrashRepo: TrashRepo{
			URL:    "https://github.com/TRaSH-Guides/Guides.git",
			Branch: "master",
		},
		PullInterval: "24h",
	}
}

// configStore manages thread-safe config access and persistence.
type configStore struct {
	mu       sync.Mutex // single mutex for all reads, writes, and saves
	config   *Config
	filePath string
}

func newConfigStore(dir string) *configStore {
	return &configStore{
		config:   DefaultConfig(),
		filePath: filepath.Join(dir, "clonarr.json"),
	}
}

// Load reads config from disk. If the file doesn't exist, keeps defaults.
func (cs *configStore) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // use defaults
		}
		return fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	cs.config = &cfg
	return nil
}

// saveLocked writes config to disk. Must be called with mu held.
func (cs *configStore) saveLocked() error {
	data, err := json.MarshalIndent(cs.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(cs.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Atomic write: temp file + rename
	tmp := cs.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	return os.Rename(tmp, cs.filePath)
}

// Get returns a deep copy of the current config.
func (cs *configStore) Get() Config {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	copied, err := copystructure.Copy(cs.config)
	if err != nil {
		log.Fatalf("BUG: config deep-copy failed: %v", err)
	}
	return *copied.(*Config)
}

// Set replaces the config and saves to disk.
func (cs *configStore) Set(cfg *Config) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.config = cfg
	return cs.saveLocked()
}

// Update atomically reads, modifies, and saves the config.
// The fn callback receives the live config pointer under the lock.
func (cs *configStore) Update(fn func(*Config)) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	fn(cs.config)
	return cs.saveLocked()
}

// GetInstance returns an instance by ID.
func (cs *configStore) GetInstance(id string) (Instance, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, inst := range cs.config.Instances {
		if inst.ID == id {
			return inst, true
		}
	}
	return Instance{}, false
}

// AddInstance adds a new instance with a generated ID.
func (cs *configStore) AddInstance(inst Instance) (Instance, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	inst.ID = generateID()
	cs.config.Instances = append(cs.config.Instances, inst)
	return inst, cs.saveLocked()
}

// UpdateInstance replaces an existing instance.
func (cs *configStore) UpdateInstance(id string, inst Instance) (Instance, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, existing := range cs.config.Instances {
		if existing.ID == id {
			inst.ID = id
			cs.config.Instances[i] = inst
			return inst, cs.saveLocked()
		}
	}
	return Instance{}, fmt.Errorf("instance %s not found", id)
}

// DeleteInstance removes an instance by ID and cleans up associated sync history.
func (cs *configStore) DeleteInstance(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	found := false
	for i, inst := range cs.config.Instances {
		if inst.ID == id {
			cs.config.Instances = append(cs.config.Instances[:i], cs.config.Instances[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("instance %s not found", id)
	}
	// Clean up sync history for deleted instance
	filtered := make([]SyncHistoryEntry, 0, len(cs.config.SyncHistory))
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID != id {
			filtered = append(filtered, sh)
		}
	}
	cs.config.SyncHistory = filtered
	// Clean up QS overrides, auto-sync, and cleanup keep list for deleted instance
	delete(cs.config.QualitySizeOverrides, id)
	delete(cs.config.QualitySizeAutoSync, id)
	delete(cs.config.CleanupKeep, id)
	// Clean up auto-sync rules for deleted instance
	filteredRules := make([]AutoSyncRule, 0, len(cs.config.AutoSync.Rules))
	for _, r := range cs.config.AutoSync.Rules {
		if r.InstanceID != id {
			filteredRules = append(filteredRules, r)
		}
	}
	cs.config.AutoSync.Rules = filteredRules
	return cs.saveLocked()
}

// UpsertSyncHistory adds or updates a sync history entry (keyed by instanceId + arrProfileId).
// This allows the same TRaSH profile to be synced to multiple Arr profiles on the same instance.
func (cs *configStore) UpsertSyncHistory(entry SyncHistoryEntry) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, sh := range cs.config.SyncHistory {
		if sh.InstanceID == entry.InstanceID && sh.ArrProfileID == entry.ArrProfileID {
			cs.config.SyncHistory[i] = entry
			return cs.saveLocked()
		}
	}
	cs.config.SyncHistory = append(cs.config.SyncHistory, entry)
	return cs.saveLocked()
}

// GetSyncHistory returns all sync history entries for an instance.
func (cs *configStore) GetSyncHistory(instanceID string) []SyncHistoryEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var entries []SyncHistoryEntry
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID == instanceID {
			entries = append(entries, sh)
		}
	}
	return entries
}

// DeleteSyncHistory removes a sync history entry by instanceId + arrProfileId.
func (cs *configStore) DeleteSyncHistory(instanceID string, arrProfileID int) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, sh := range cs.config.SyncHistory {
		if sh.InstanceID == instanceID && sh.ArrProfileID == arrProfileID {
			cs.config.SyncHistory = append(cs.config.SyncHistory[:i], cs.config.SyncHistory[i+1:]...)
			return cs.saveLocked()
		}
	}
	return fmt.Errorf("sync history entry not found")
}

// migrateImportedProfiles moves any imported profiles from the old config
// file (clonarr.json) to per-file storage in /config/profiles/.
func migrateImportedProfiles(cs *configStore, ps *profileStore) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Check for legacy field by reading raw JSON
	data, err := os.ReadFile(cs.filePath)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	rawProfiles, ok := raw["importedProfiles"]
	if !ok || string(rawProfiles) == "null" {
		return
	}

	var profiles []ImportedProfile
	if err := json.Unmarshal(rawProfiles, &profiles); err != nil || len(profiles) == 0 {
		return
	}

	// Migrate to per-file storage
	if err := ps.Add(profiles); err != nil {
		log.Printf("Warning: failed to migrate imported profiles: %v", err)
		return
	}

	// Remove from config and save
	delete(raw, "importedProfiles")
	cleaned, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	tmp := cs.filePath + ".tmp"
	if err := os.WriteFile(tmp, cleaned, 0644); err != nil {
		return
	}
	os.Rename(tmp, cs.filePath)

	// Reload config to pick up cleaned version
	var cfg Config
	if err := json.Unmarshal(cleaned, &cfg); err == nil {
		cs.config = &cfg
	}

	log.Printf("Migrated %d imported profiles to per-file storage", len(profiles))
}

// generateID creates a random hex string.
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
