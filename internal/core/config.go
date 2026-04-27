package core

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Config holds the full application configuration, persisted to JSON.
type Config struct {
	Instances            []Instance                       `json:"instances"`
	TrashRepo            TrashRepo                        `json:"trashRepo"`
	PullInterval         string                           `json:"pullInterval"`                   // Go duration (e.g. "24h", "1h", "0" to disable)
	DevMode              bool                             `json:"devMode"`                        // Advanced Mode — enables Profile Builder, Scoring Sandbox, CF Group Builder and Prowlarr settings
	TrashSchemaFields    bool                             `json:"trashSchemaFields"`              // Show TRaSH-schema fields (trash_id, trash_scores, group, description) in CF editor, Profile Builder, CF Group Builder
	DebugLogging         bool                             `json:"debugLogging"`                   // Write detailed operations to /config/debug.log
	QualitySizeOverrides map[string]map[string]QSOverride `json:"qualitySizeOverrides,omitempty"` // instanceID → quality name → override
	QualitySizeAutoSync  map[string]QSAutoSync            `json:"qualitySizeAutoSync,omitempty"`  // instanceID → auto-sync settings
	SyncHistory          []SyncHistoryEntry               `json:"syncHistory,omitempty"`
	CleanupKeep          map[string][]string              `json:"cleanupKeep,omitempty"` // instanceID → CF names to keep during delete-all
	AutoSync             AutoSyncConfig                   `json:"autoSync,omitempty"`
	Prowlarr             ProwlarrConfig                   `json:"prowlarr,omitempty"`
	// Authentication — matches Radarr/Sonarr Security panel model.
	// Credentials (bcrypt password hash, API key) live separately in
	// /config/auth.json, NOT here, so this file can be exported/shared
	// without leaking secrets.
	Authentication         string `json:"authentication,omitempty"`         // "forms" (default) | "basic" | "none"
	AuthenticationRequired string `json:"authenticationRequired,omitempty"` // "enabled" | "disabled_for_local_addresses" (default)
	TrustedProxies         string `json:"trustedProxies,omitempty"`         // comma-separated IPs — reverse-proxy deployments
	TrustedNetworks        string `json:"trustedNetworks,omitempty"`        // comma-separated IPs/CIDRs for local-bypass; empty = Radarr-parity default
	SessionTTLDays         int    `json:"sessionTtlDays,omitempty"`         // default 30
}

// ProwlarrConfig holds Prowlarr connection settings for the Scoring Sandbox.
// RadarrCategories / SonarrCategories override the default [2000] / [5000]
// Newznab category IDs — needed for indexers whose definitions don't cascade
// the parent ID to sub-categories (private trackers often tag only sub-IDs
// like 2040, 2045). Empty slice means "use default".
type ProwlarrConfig struct {
	URL              string `json:"url"`
	APIKey           string `json:"apiKey"`
	Enabled          bool   `json:"enabled"`
	RadarrCategories []int  `json:"radarrCategories,omitempty"`
	SonarrCategories []int  `json:"sonarrCategories,omitempty"`
}

// AutoSyncConfig holds global auto-sync settings and rules.
type AutoSyncConfig struct {
	Enabled            bool                `json:"enabled"`
	NotificationAgents []NotificationAgent `json:"notificationAgents,omitempty"`
	Rules              []AutoSyncRule      `json:"rules,omitempty"`
}

// NotificationAgent is a configured notification provider instance.
type NotificationAgent struct {
	ID      string             `json:"id"`
	Name    string             `json:"name"` // user-defined label, e.g. "Discord #alerts"
	Type    string             `json:"type"` // registered provider type, e.g. "discord" | "gotify" | "pushover"
	Enabled bool               `json:"enabled"`
	Events  AgentEvents        `json:"events"`
	Config  NotificationConfig `json:"config"`
}

// AgentEvents controls which auto-sync events trigger this agent.
type AgentEvents struct {
	OnSyncSuccess bool `json:"onSyncSuccess"`
	OnSyncFailure bool `json:"onSyncFailure"`
	OnCleanup     bool `json:"onCleanup"`
	OnRepoUpdate  bool `json:"onRepoUpdate"`
	OnChangelog   bool `json:"onChangelog"`
}

// NotificationConfig holds provider-specific credentials and settings.
// Fields are omitempty so unused providers add no JSON bloat.
// Adding a new provider = append fields here + register a NotificationAgentProvider.
type NotificationConfig struct {
	// Discord
	DiscordWebhook        string `json:"discordWebhook,omitempty"`
	DiscordWebhookUpdates string `json:"discordWebhookUpdates,omitempty"`
	// Gotify
	GotifyURL              string `json:"gotifyUrl,omitempty"`
	GotifyToken            string `json:"gotifyToken,omitempty"`
	GotifyPriorityCritical bool   `json:"gotifyPriorityCritical,omitempty"`
	GotifyPriorityWarning  bool   `json:"gotifyPriorityWarning,omitempty"`
	GotifyPriorityInfo     bool   `json:"gotifyPriorityInfo,omitempty"`
	GotifyCriticalValue    *int   `json:"gotifyCriticalValue,omitempty"`
	GotifyWarningValue     *int   `json:"gotifyWarningValue,omitempty"`
	GotifyInfoValue        *int   `json:"gotifyInfoValue,omitempty"`
	// Pushover
	PushoverUserKey  string `json:"pushoverUserKey,omitempty"`
	PushoverAppToken string `json:"pushoverAppToken,omitempty"`
}

// AutoSyncRule defines one auto-sync binding (profile → instance).
type AutoSyncRule struct {
	ID                string          `json:"id"`
	Enabled           bool            `json:"enabled"`
	InstanceID        string          `json:"instanceId"`
	ProfileSource     string          `json:"profileSource"` // "trash" or "imported"
	TrashProfileID    string          `json:"trashProfileId,omitempty"`
	ImportedProfileID string          `json:"importedProfileId,omitempty"`
	ArrProfileID      int             `json:"arrProfileId"`               // target Arr profile to update
	SelectedCFs       []string        `json:"selectedCFs,omitempty"`      // user's optional CF selections
	ScoreOverrides    map[string]int  `json:"scoreOverrides,omitempty"`   // per-CF score overrides (trash_id → score)
	QualityOverrides  map[string]bool `json:"qualityOverrides,omitempty"` // legacy flat quality override (name → allowed). Used when QualityStructure is empty.
	QualityStructure  []QualityItem   `json:"qualityStructure,omitempty"` // full structure override (replaces TRaSH items). Trumps QualityOverrides when set.
	Behavior          *SyncBehavior   `json:"behavior,omitempty"`         // sync behavior rules (nil = defaults)
	Overrides         *SyncOverrides  `json:"overrides,omitempty"`        // user overrides (min score, language, cutoff, etc.)
	LastSyncCommit    string          `json:"lastSyncCommit,omitempty"`
	LastSyncTime      string          `json:"lastSyncTime,omitempty"`
	LastSyncError     string          `json:"lastSyncError,omitempty"`
	// OrphanedAt is set (RFC3339 timestamp) when clonarr's drift-check
	// detects that ArrProfileID no longer resolves in the target Arr
	// instance. Auto-sync skips orphaned rules; the UI exposes Restore
	// (re-create profile in Arr from last synced intent) and Remove
	// (permanent delete) actions. Empty when the rule is in normal
	// operation. Soft-tombstone replaces the previous auto-delete cleanup.
	OrphanedAt string `json:"orphanedAt,omitempty"`
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

// SyncChanges captures the detailed changes made during a sync.
// Stored only when the sync actually modified something (not on no-op syncs).
// The string slices come directly from the sync result's *Details fields —
// human-readable and display-ready (e.g. "BHDStudio: 1000 → 2240").
type SyncChanges struct {
	CFDetails       []string `json:"cfDetails,omitempty"`
	ScoreDetails    []string `json:"scoreDetails,omitempty"`
	QualityDetails  []string `json:"qualityDetails,omitempty"`
	SettingsDetails []string `json:"settingsDetails,omitempty"`
}

// HasChanges returns true if any change category has entries.
func (c *SyncChanges) HasChanges() bool {
	return c != nil && (len(c.CFDetails) > 0 || len(c.ScoreDetails) > 0 ||
		len(c.QualityDetails) > 0 || len(c.SettingsDetails) > 0)
}

// SyncHistoryEntry records a completed sync operation.
type SyncHistoryEntry struct {
	InstanceID        string          `json:"instanceId"`
	InstanceType      string          `json:"instanceType,omitempty"` // "radarr" or "sonarr" — for orphan migration
	ProfileTrashID    string          `json:"profileTrashId"`
	ImportedProfileID string          `json:"importedProfileId,omitempty"`
	ProfileName       string          `json:"profileName"`
	ArrProfileID      int             `json:"arrProfileId"`
	ArrProfileName    string          `json:"arrProfileName"`
	SyncedCFs         []string        `json:"syncedCFs"`
	SelectedCFs       map[string]bool `json:"selectedCFs,omitempty"`
	ScoreOverrides    map[string]int  `json:"scoreOverrides,omitempty"`
	QualityOverrides  map[string]bool `json:"qualityOverrides,omitempty"` // legacy flat override (name → allowed)
	QualityStructure  []QualityItem   `json:"qualityStructure,omitempty"` // full structure override (trumps QualityOverrides)
	Overrides         *SyncOverrides  `json:"overrides,omitempty"`
	Behavior          *SyncBehavior   `json:"behavior,omitempty"`
	CFsCreated        int             `json:"cfsCreated"`
	CFsUpdated        int             `json:"cfsUpdated"`
	ScoresUpdated     int             `json:"scoresUpdated"`
	// LastSync bumps on every sync attempt for this profile (including no-op
	// auto-syncs) so callers can show "last activity" per profile. UI surfaces
	// it in the TRaSH Sync tab's per-profile row.
	LastSync string `json:"lastSync"`
	// AppliedAt is frozen at entry creation when the sync actually produced
	// changes — a stable "when these changes landed" timestamp. Empty on
	// baseline/no-op entries and on entries predating this field (in which
	// case UI falls back to LastSync).
	AppliedAt string       `json:"appliedAt,omitempty"`
	Changes   *SyncChanges `json:"changes,omitempty"`
	// OrphanedAt mirrors the field on the rule — set when the entry
	// belongs to a profile that has been detected as deleted in Arr.
	// Lets the UI gray out / badge orphaned history rows independently
	// of rule state (the rule may have been removed while history is
	// retained for diagnostics).
	OrphanedAt string `json:"orphanedAt,omitempty"`
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

// ConfigStore manages thread-safe config access and persistence.
type ConfigStore struct {
	mu       sync.Mutex // single mutex for all reads, writes, and saves
	config   *Config
	filePath string
}

func NewConfigStore(dir string) *ConfigStore {
	return &ConfigStore{
		config:   DefaultConfig(),
		filePath: filepath.Join(dir, "clonarr.json"),
	}
}

// Load reads config from disk. If the file doesn't exist, keeps defaults.
func (cs *ConfigStore) Load() error {
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

	// Migrate old flat notification fields to NotificationAgents slice.
	// Safe to call under lock — migrateFlatNotifications reads cs.filePath directly.
	cs.migrateFlatNotifications(data)

	// Backfill AppliedAt on sync history entries that predate the field.
	// Without this, existing entries keep showing "just now" in the History
	// tab (because the frontend falls back to LastSync when AppliedAt is
	// empty, and LastSync still bumps on no-op syncs). Seeding with
	// LastSync captures the current value and — crucially — freezes it, so
	// subsequent no-op bumps no longer drift the displayed "Last Changed"
	// timestamp. Best-effort: we don't know the original change time, but
	// freezing at migration time stops the wandering. New entries created
	// after this point set AppliedAt inline (see api/sync.go & autosync.go).
	var migrated int
	for i := range cs.config.SyncHistory {
		sh := &cs.config.SyncHistory[i]
		if sh.AppliedAt == "" && sh.Changes.HasChanges() {
			sh.AppliedAt = sh.LastSync
			migrated++
		}
	}
	if migrated > 0 {
		log.Printf("Migrated %d sync history entries to set AppliedAt = LastSync", migrated)
		if err := cs.saveLocked(); err != nil {
			log.Printf("Warning: failed to persist sync history migration: %v", err)
		}
	}
	return nil
}

// saveLocked writes config to disk. Must be called with mu held.
func (cs *ConfigStore) saveLocked() error {
	data, err := json.MarshalIndent(cs.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(cs.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Atomic write: temp file + rename. Mode 0600 — Arr API keys, webhook
	// URLs, and Gotify/Pushover tokens live here; prevent other users on the
	// same Docker host (or backup jobs running as other UIDs) from reading
	// secrets just because /config/ is readable.
	tmp := cs.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	return os.Rename(tmp, cs.filePath)
}

// Get returns a deep copy of the current config.
func (cs *ConfigStore) Get() Config {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cfg := *cs.config
	cfg.Instances = make([]Instance, len(cs.config.Instances))
	copy(cfg.Instances, cs.config.Instances)
	cfg.SyncHistory = make([]SyncHistoryEntry, len(cs.config.SyncHistory))
	for i, sh := range cs.config.SyncHistory {
		cfg.SyncHistory[i] = sh
		cfg.SyncHistory[i].SyncedCFs = make([]string, len(sh.SyncedCFs))
		copy(cfg.SyncHistory[i].SyncedCFs, sh.SyncedCFs)
		if len(sh.SelectedCFs) > 0 {
			cfg.SyncHistory[i].SelectedCFs = make(map[string]bool, len(sh.SelectedCFs))
			for k, v := range sh.SelectedCFs {
				cfg.SyncHistory[i].SelectedCFs[k] = v
			}
		}
		if len(sh.ScoreOverrides) > 0 {
			cfg.SyncHistory[i].ScoreOverrides = make(map[string]int, len(sh.ScoreOverrides))
			for k, v := range sh.ScoreOverrides {
				cfg.SyncHistory[i].ScoreOverrides[k] = v
			}
		}
		if len(sh.QualityOverrides) > 0 {
			cfg.SyncHistory[i].QualityOverrides = make(map[string]bool, len(sh.QualityOverrides))
			for k, v := range sh.QualityOverrides {
				cfg.SyncHistory[i].QualityOverrides[k] = v
			}
		}
		if len(sh.QualityStructure) > 0 {
			cfg.SyncHistory[i].QualityStructure = cloneQualityItems(sh.QualityStructure)
		}
		if sh.Overrides != nil {
			o := *sh.Overrides
			cfg.SyncHistory[i].Overrides = &o
		}
		if sh.Behavior != nil {
			b := *sh.Behavior
			cfg.SyncHistory[i].Behavior = &b
		}
		if sh.Changes != nil {
			c := *sh.Changes
			if len(sh.Changes.CFDetails) > 0 {
				c.CFDetails = make([]string, len(sh.Changes.CFDetails))
				copy(c.CFDetails, sh.Changes.CFDetails)
			}
			if len(sh.Changes.ScoreDetails) > 0 {
				c.ScoreDetails = make([]string, len(sh.Changes.ScoreDetails))
				copy(c.ScoreDetails, sh.Changes.ScoreDetails)
			}
			if len(sh.Changes.QualityDetails) > 0 {
				c.QualityDetails = make([]string, len(sh.Changes.QualityDetails))
				copy(c.QualityDetails, sh.Changes.QualityDetails)
			}
			if len(sh.Changes.SettingsDetails) > 0 {
				c.SettingsDetails = make([]string, len(sh.Changes.SettingsDetails))
				copy(c.SettingsDetails, sh.Changes.SettingsDetails)
			}
			cfg.SyncHistory[i].Changes = &c
		}
	}
	// Deep-copy QualitySizeOverrides (nested map)
	if cs.config.QualitySizeOverrides != nil {
		cfg.QualitySizeOverrides = make(map[string]map[string]QSOverride, len(cs.config.QualitySizeOverrides))
		for k, v := range cs.config.QualitySizeOverrides {
			inner := make(map[string]QSOverride, len(v))
			for ik, iv := range v {
				inner[ik] = iv
			}
			cfg.QualitySizeOverrides[k] = inner
		}
	}
	// Deep-copy QualitySizeAutoSync
	if cs.config.QualitySizeAutoSync != nil {
		cfg.QualitySizeAutoSync = make(map[string]QSAutoSync, len(cs.config.QualitySizeAutoSync))
		for k, v := range cs.config.QualitySizeAutoSync {
			cfg.QualitySizeAutoSync[k] = v
		}
	}
	// Deep-copy CleanupKeep
	if cs.config.CleanupKeep != nil {
		cfg.CleanupKeep = make(map[string][]string, len(cs.config.CleanupKeep))
		for k, v := range cs.config.CleanupKeep {
			cp := make([]string, len(v))
			copy(cp, v)
			cfg.CleanupKeep[k] = cp
		}
	}
	// Deep-copy NotificationAgents
	if len(cs.config.AutoSync.NotificationAgents) > 0 {
		cfg.AutoSync.NotificationAgents = make([]NotificationAgent, len(cs.config.AutoSync.NotificationAgents))
		for i, a := range cs.config.AutoSync.NotificationAgents {
			cfg.AutoSync.NotificationAgents[i] = a
			// NotificationConfig contains only scalars and *int pointers — copy the pointers
			nc := a.Config
			if a.Config.GotifyCriticalValue != nil {
				v := *a.Config.GotifyCriticalValue
				nc.GotifyCriticalValue = &v
			}
			if a.Config.GotifyWarningValue != nil {
				v := *a.Config.GotifyWarningValue
				nc.GotifyWarningValue = &v
			}
			if a.Config.GotifyInfoValue != nil {
				v := *a.Config.GotifyInfoValue
				nc.GotifyInfoValue = &v
			}
			cfg.AutoSync.NotificationAgents[i].Config = nc
		}
	}
	// Deep-copy AutoSync rules
	if len(cs.config.AutoSync.Rules) > 0 {
		cfg.AutoSync.Rules = make([]AutoSyncRule, len(cs.config.AutoSync.Rules))
		for i, r := range cs.config.AutoSync.Rules {
			cfg.AutoSync.Rules[i] = r
			if len(r.SelectedCFs) > 0 {
				cfg.AutoSync.Rules[i].SelectedCFs = make([]string, len(r.SelectedCFs))
				copy(cfg.AutoSync.Rules[i].SelectedCFs, r.SelectedCFs)
			}
			if len(r.ScoreOverrides) > 0 {
				cfg.AutoSync.Rules[i].ScoreOverrides = make(map[string]int, len(r.ScoreOverrides))
				for k, v := range r.ScoreOverrides {
					cfg.AutoSync.Rules[i].ScoreOverrides[k] = v
				}
			}
			if len(r.QualityOverrides) > 0 {
				cfg.AutoSync.Rules[i].QualityOverrides = make(map[string]bool, len(r.QualityOverrides))
				for k, v := range r.QualityOverrides {
					cfg.AutoSync.Rules[i].QualityOverrides[k] = v
				}
			}
			if len(r.QualityStructure) > 0 {
				cfg.AutoSync.Rules[i].QualityStructure = cloneQualityItems(r.QualityStructure)
			}
			if r.Behavior != nil {
				b := *r.Behavior
				cfg.AutoSync.Rules[i].Behavior = &b
			}
			if r.Overrides != nil {
				o := *r.Overrides
				cfg.AutoSync.Rules[i].Overrides = &o
			}
		}
	}
	return cfg
}

// Set replaces the config and saves to disk.
func (cs *ConfigStore) Set(cfg *Config) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.config = cfg
	return cs.saveLocked()
}

// Update atomically reads, modifies, and saves the config.
// The fn callback receives the live config pointer under the lock.
func (cs *ConfigStore) Update(fn func(*Config)) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	fn(cs.config)
	return cs.saveLocked()
}

// migrateFlatNotifications converts legacy flat notification fields in AutoSyncConfig
// to the new NotificationAgents slice. Called once on Load with the raw file bytes.
// Skips silently if NotificationAgents already has entries.
func (cs *ConfigStore) migrateFlatNotifications(raw []byte) {
	// Already migrated?
	if len(cs.config.AutoSync.NotificationAgents) > 0 {
		return
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return
	}
	autoSyncRaw, ok := root["autoSync"]
	if !ok {
		return
	}
	var as map[string]json.RawMessage
	if err := json.Unmarshal(autoSyncRaw, &as); err != nil {
		return
	}

	// Helper: unmarshal a field if present.
	str := func(key string) string {
		v, ok := as[key]
		if !ok {
			return ""
		}
		var s string
		json.Unmarshal(v, &s)
		return s
	}
	boolVal := func(key string, def bool) bool {
		v, ok := as[key]
		if !ok {
			return def
		}
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return def
		}
		return b
	}
	intPtr := func(key string, def int) *int {
		v, ok := as[key]
		if !ok {
			return &def
		}
		var n int
		if json.Unmarshal(v, &n) != nil {
			return &def
		}
		return &n
	}

	notifySuccess := boolVal("notifyOnSuccess", true)
	notifyFailure := boolVal("notifyOnFailure", true)
	notifyRepo := boolVal("notifyOnRepoUpdate", false)

	var agents []NotificationAgent

	// Discord
	discordWebhook := str("discordWebhook")
	if discordWebhook != "" {
		cv, wv, iv := 8, 5, 3
		_ = cv
		_ = wv
		_ = iv
		agents = append(agents, NotificationAgent{
			ID:      GenerateID(),
			Name:    "Discord",
			Type:    "discord",
			Enabled: boolVal("discordEnabled", true),
			Events: AgentEvents{
				OnSyncSuccess: notifySuccess,
				OnSyncFailure: notifyFailure,
				OnCleanup:     notifyFailure,
				OnRepoUpdate:  notifyRepo,
				OnChangelog:   notifyRepo,
			},
			Config: NotificationConfig{
				DiscordWebhook:        discordWebhook,
				DiscordWebhookUpdates: str("discordWebhookUpdates"),
			},
		})
	}

	// Gotify
	gotifyURL := str("gotifyUrl")
	gotifyToken := str("gotifyToken")
	if gotifyURL != "" || gotifyToken != "" {
		agents = append(agents, NotificationAgent{
			ID:      GenerateID(),
			Name:    "Gotify",
			Type:    "gotify",
			Enabled: boolVal("gotifyEnabled", false),
			Events: AgentEvents{
				OnSyncSuccess: notifySuccess,
				OnSyncFailure: notifyFailure,
				OnCleanup:     notifyFailure,
				OnRepoUpdate:  notifyRepo,
				OnChangelog:   notifyRepo,
			},
			Config: NotificationConfig{
				GotifyURL:              gotifyURL,
				GotifyToken:            gotifyToken,
				GotifyPriorityCritical: boolVal("gotifyPriorityCritical", true),
				GotifyPriorityWarning:  boolVal("gotifyPriorityWarning", true),
				GotifyPriorityInfo:     boolVal("gotifyPriorityInfo", false),
				GotifyCriticalValue:    intPtr("gotifyCriticalValue", 8),
				GotifyWarningValue:     intPtr("gotifyWarningValue", 5),
				GotifyInfoValue:        intPtr("gotifyInfoValue", 3),
			},
		})
	}

	// Pushover
	pushoverKey := str("pushoverUserKey")
	pushoverToken := str("pushoverAppToken")
	if pushoverKey != "" || pushoverToken != "" {
		agents = append(agents, NotificationAgent{
			ID:      GenerateID(),
			Name:    "Pushover",
			Type:    "pushover",
			Enabled: boolVal("pushoverEnabled", false),
			Events: AgentEvents{
				OnSyncSuccess: notifySuccess,
				OnSyncFailure: notifyFailure,
				OnCleanup:     notifyFailure,
				OnRepoUpdate:  notifyRepo,
				OnChangelog:   notifyRepo,
			},
			Config: NotificationConfig{
				PushoverUserKey:  pushoverKey,
				PushoverAppToken: pushoverToken,
			},
		})
	}

	if len(agents) == 0 {
		return
	}

	cs.config.AutoSync.NotificationAgents = agents
	log.Printf("Migrated %d notification provider(s) to NotificationAgents", len(agents))

	// Persist migration (strip old flat keys from JSON, write new agents field).
	// saveLocked expects mu to be held by the caller — this is safe because
	// migrateFlatNotifications is only ever called from Load(), which holds mu
	// for its entire duration. saveLocked itself does not re-acquire the lock.
	if err := cs.saveLocked(); err != nil {
		log.Printf("Warning: failed to persist notification migration: %v", err)
	}
}

// --- Notification Agent CRUD --------------------------------------------------

// GetNotificationAgent returns a notification agent by ID.
func (cs *ConfigStore) GetNotificationAgent(id string) (NotificationAgent, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, a := range cs.config.AutoSync.NotificationAgents {
		if a.ID == id {
			return a, true
		}
	}
	return NotificationAgent{}, false
}

// AddNotificationAgent appends a new notification agent with a generated ID.
// Multiple agents of the same type are permitted (e.g. two Discord channels).
func (cs *ConfigStore) AddNotificationAgent(agent NotificationAgent) (NotificationAgent, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	agent.ID = GenerateID()
	cs.config.AutoSync.NotificationAgents = append(cs.config.AutoSync.NotificationAgents, agent)
	return agent, cs.saveLocked()
}

// UpdateNotificationAgent replaces an existing notification agent by ID.
func (cs *ConfigStore) UpdateNotificationAgent(id string, agent NotificationAgent) (NotificationAgent, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, a := range cs.config.AutoSync.NotificationAgents {
		if a.ID == id {
			agent.ID = id
			cs.config.AutoSync.NotificationAgents[i] = agent
			return agent, cs.saveLocked()
		}
	}
	return NotificationAgent{}, fmt.Errorf("notification agent %s not found", id)
}

// DeleteNotificationAgent removes a notification agent by ID.
func (cs *ConfigStore) DeleteNotificationAgent(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i, a := range cs.config.AutoSync.NotificationAgents {
		if a.ID == id {
			cs.config.AutoSync.NotificationAgents = append(
				cs.config.AutoSync.NotificationAgents[:i],
				cs.config.AutoSync.NotificationAgents[i+1:]...,
			)
			return cs.saveLocked()
		}
	}
	return fmt.Errorf("notification agent %s not found", id)
}

// GetInstance returns an instance by ID.
func (cs *ConfigStore) GetInstance(id string) (Instance, bool) {
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
// If orphaned sync history/rules exist from a deleted instance with the same URL and type,
// they are migrated to the new instance ID (preserves data across instance re-creation).
func (cs *ConfigStore) AddInstance(inst Instance) (Instance, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	inst.ID = GenerateID()
	// Find orphaned data from a deleted instance.
	// Only migrate if exactly ONE orphan group exists (avoids cross-type contamination).
	activeIDs := make(map[string]bool)
	for _, i := range cs.config.Instances {
		activeIDs[i.ID] = true
	}
	orphanIDs := make(map[string]string) // orphan instance ID → type (if known)
	for _, h := range cs.config.SyncHistory {
		if !activeIDs[h.InstanceID] {
			if h.InstanceType != "" {
				orphanIDs[h.InstanceID] = h.InstanceType
			} else if _, exists := orphanIDs[h.InstanceID]; !exists {
				orphanIDs[h.InstanceID] = ""
			}
		}
	}
	for _, r := range cs.config.AutoSync.Rules {
		if !activeIDs[r.InstanceID] {
			if _, exists := orphanIDs[r.InstanceID]; !exists {
				orphanIDs[r.InstanceID] = ""
			}
		}
	}
	// Only migrate orphan that matches the new instance's type
	var orphanID string
	for id, orphanType := range orphanIDs {
		if orphanType == "" || orphanType == inst.Type {
			if orphanID != "" {
				// Multiple matching orphans — skip migration for safety
				orphanID = ""
				log.Printf("Multiple orphaned instances match type %s, skipping migration", inst.Type)
				break
			}
			orphanID = id
		}
	}
	// Migrate orphaned data to new instance
	if orphanID != "" {
		for i := range cs.config.SyncHistory {
			if cs.config.SyncHistory[i].InstanceID == orphanID {
				cs.config.SyncHistory[i].InstanceID = inst.ID
			}
		}
		for i := range cs.config.AutoSync.Rules {
			if cs.config.AutoSync.Rules[i].InstanceID == orphanID {
				cs.config.AutoSync.Rules[i].InstanceID = inst.ID
			}
		}
		// Migrate QS overrides and auto-sync settings
		if qs, ok := cs.config.QualitySizeOverrides[orphanID]; ok {
			if cs.config.QualitySizeOverrides == nil {
				cs.config.QualitySizeOverrides = make(map[string]map[string]QSOverride)
			}
			cs.config.QualitySizeOverrides[inst.ID] = qs
			delete(cs.config.QualitySizeOverrides, orphanID)
		}
		if qsa, ok := cs.config.QualitySizeAutoSync[orphanID]; ok {
			if cs.config.QualitySizeAutoSync == nil {
				cs.config.QualitySizeAutoSync = make(map[string]QSAutoSync)
			}
			cs.config.QualitySizeAutoSync[inst.ID] = qsa
			delete(cs.config.QualitySizeAutoSync, orphanID)
		}
		if ck, ok := cs.config.CleanupKeep[orphanID]; ok {
			if cs.config.CleanupKeep == nil {
				cs.config.CleanupKeep = make(map[string][]string)
			}
			cs.config.CleanupKeep[inst.ID] = ck
			delete(cs.config.CleanupKeep, orphanID)
		}
		log.Printf("Migrated orphaned data from deleted instance %s to new instance %s (%s)", orphanID, inst.ID, inst.Name)
	}
	cs.config.Instances = append(cs.config.Instances, inst)
	return inst, cs.saveLocked()
}

// UpdateInstance replaces an existing instance.
func (cs *ConfigStore) UpdateInstance(id string, inst Instance) (Instance, error) {
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
func (cs *ConfigStore) DeleteInstance(id string) error {
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
	// Keep sync history, auto-sync rules, and QS data as orphaned data.
	// They will be migrated to a new instance if one is added with the same URL/type,
	// or cleaned up by stale cleanup if the Arr profiles no longer exist.
	return cs.saveLocked()
}

// maxSyncHistoryPerProfile is the maximum number of change-bearing entries kept per
// instance+arrProfile pair. Entries without changes (no-op syncs) only update the
// timestamp on the most recent entry and don't consume a slot.
const maxSyncHistoryPerProfile = 10

// UpsertSyncHistory appends a sync history entry. When the entry carries actual
// changes (entry.Changes.HasChanges()), it's prepended as a new entry and the list
// is capped at maxSyncHistoryPerProfile. When there are no changes, only the
// LastSync timestamp on the most recent entry for that profile is updated (no new
// entry created). Entries are stored newest-first so existing code that iterates
// and breaks on first match automatically gets the latest.
func (cs *ConfigStore) UpsertSyncHistory(entry SyncHistoryEntry) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	hasChanges := entry.Changes.HasChanges()

	if !hasChanges {
		// No-op sync: just bump the timestamp on the newest entry for this profile.
		for i, sh := range cs.config.SyncHistory {
			if sh.InstanceID == entry.InstanceID && sh.ArrProfileID == entry.ArrProfileID {
				cs.config.SyncHistory[i].LastSync = entry.LastSync
				return cs.saveLocked()
			}
		}
		// First sync for this profile — fall through to append even without changes
		// so we have a baseline entry for future diffs.
	}

	// Prepend the new entry (newest-first).
	cs.config.SyncHistory = append([]SyncHistoryEntry{entry}, cs.config.SyncHistory...)

	// Cap: keep at most maxSyncHistoryPerProfile entries per instance+arrProfile.
	count := 0
	keep := make([]SyncHistoryEntry, 0, len(cs.config.SyncHistory))
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID == entry.InstanceID && sh.ArrProfileID == entry.ArrProfileID {
			count++
			if count > maxSyncHistoryPerProfile {
				continue // drop oldest
			}
		}
		keep = append(keep, sh)
	}
	cs.config.SyncHistory = keep

	return cs.saveLocked()
}

// GetSyncHistory returns all sync history entries for an instance (newest-first).
func (cs *ConfigStore) GetSyncHistory(instanceID string) []SyncHistoryEntry {
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

// GetLatestSyncEntry returns the most recent sync history entry for a specific
// instance + arrProfile. Returns nil if no entry exists. Used by Compare, Builder
// import, and other consumers that only need the current state.
func (cs *ConfigStore) GetLatestSyncEntry(instanceID string, arrProfileID int) *SyncHistoryEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID == instanceID && sh.ArrProfileID == arrProfileID {
			entry := sh
			return &entry
		}
	}
	return nil
}

// GetProfileChangeHistory returns all history entries for a specific instance +
// arrProfile pair, newest-first (includes the baseline no-change entry if present).
// Used by the History tab.
func (cs *ConfigStore) GetProfileChangeHistory(instanceID string, arrProfileID int) []SyncHistoryEntry {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var entries []SyncHistoryEntry
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID == instanceID && sh.ArrProfileID == arrProfileID {
			entries = append(entries, sh)
		}
	}
	return entries
}

// DeleteSyncHistory removes a sync history entry by instanceId + arrProfileId.
// DeleteSyncHistory removes ALL sync history entries matching the
// (instanceID, arrProfileID) pair. A profile that has been synced multiple
// times accumulates multiple entries; the UI dedupes them to one row, so a
// single user-initiated delete must clear every matching entry — otherwise
// the row reappears (only one entry got removed) and the user perceives the
// delete as broken.
func (cs *ConfigStore) DeleteSyncHistory(instanceID string, arrProfileID int) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cleaned := make([]SyncHistoryEntry, 0, len(cs.config.SyncHistory))
	removed := false
	for _, sh := range cs.config.SyncHistory {
		if sh.InstanceID == instanceID && sh.ArrProfileID == arrProfileID {
			removed = true
			continue
		}
		cleaned = append(cleaned, sh)
	}
	if !removed {
		return fmt.Errorf("sync history entry not found")
	}
	cs.config.SyncHistory = cleaned
	return cs.saveLocked()
}

// MigrateImportedProfiles moves any imported profiles from the old config
// file (clonarr.json) to per-file storage in /config/profiles/.
func MigrateImportedProfiles(cs *ConfigStore, ps *ProfileStore) {
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
	if _, _, err := ps.Add(profiles); err != nil {
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
	if err := os.WriteFile(tmp, cleaned, 0600); err != nil {
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
func GenerateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
