package core

import (
	"clonarr/internal/core/agents"
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
	PullInterval         string                           `json:"pullInterval"`                   // Go duration (e.g. "24h", "1h"), "0" to disable, or "specific" for PullSchedule
	PullSchedule         *PullSchedule                    `json:"pullSchedule,omitempty"`         // nil unless a wall-clock pull schedule has been saved
	SyncSchedule         *SyncSchedule                    `json:"syncSchedule,omitempty"`         // DEPRECATED — retained only to migrate v2 configs; converted to Auto-sync drift detection on load, then cleared
	DevMode              bool                             `json:"devMode"`                        // Advanced Mode — enables Profile Builder, Scoring Sandbox, CF Group Builder and Prowlarr settings
	TrashSchemaFields    bool                             `json:"trashSchemaFields"`              // Show TRaSH-schema fields (trash_id, trash_scores, group, description) in CF editor, Profile Builder, CF Group Builder
	DebugLogging         bool                             `json:"debugLogging"`                   // Write detailed operations to /config/debug.log
	QualitySizeOverrides map[string]map[string]QSOverride `json:"qualitySizeOverrides,omitempty"` // instanceID → quality name → override
	QualitySizeAutoSync  map[string]QSAutoSync            `json:"qualitySizeAutoSync,omitempty"`  // instanceID → auto-sync settings
	SyncHistory          []SyncHistoryEntry               `json:"syncHistory,omitempty"`
	CleanupKeep          map[string][]string              `json:"cleanupKeep,omitempty"` // instanceID → CF names to keep during delete-all
	AutoSync             AutoSyncConfig                   `json:"autoSync,omitempty"`
	DriftWatch           *DriftWatch                      `json:"driftWatch,omitempty"`           // Arr-side drift detection state; populated by the drift-detection runner
	ProfileSync          *ProfileSync                     `json:"profileSync,omitempty"`          // Unified Profile Sync subsystem. Populated via migration from PullInterval/PullSchedule on first load after upgrade. nil = pre-migration state.
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

// PullSchedule holds the wall-clock pull schedule used when PullInterval is "specific".
// The scheduler interprets Time in the process local timezone (the container's TZ).
type PullSchedule struct {
	Mode       string `json:"mode"`       // "daily", "weekly", "monthly"
	Time       string `json:"time"`       // "HH:MM" (24h format)
	DayOfWeek  int    `json:"dayOfWeek"`  // 0=Sunday..6=Saturday (weekly)
	DayOfMonth int    `json:"dayOfMonth"` // 1-28 (monthly)
}

// DriftWatch holds the configuration for the Arr-side drift detection
// subsystem.
//
// Mode controls how the detected drift is handled:
//   - "off":    no drift checks performed
//   - "detect": surface drift in UI; notify on auto-sync-ON rules; never write
//   - "fix":    detect + silently reconcile on auto-sync-ON rules
//
// Schedule reuses PullSchedule directly — no current divergence justifies a
// separate type. Both schedules share validation (DayOfMonth clamp at
// validatePullSchedule). When Mode == "off", Schedule is ignored.
//
// LastRun (RFC3339 string) and NextRun (RFC3339 string) are populated by the
// scheduler; LastResult by the DriftWatcher after each run. String timestamps
// match the codebase convention (LastSyncTime / LastSync etc. on
// AutoSyncRule and SyncHistoryEntry) — keeps JSON output uniform.
type DriftWatch struct {
	Mode       string          `json:"mode"`                 // "off" | "detect" | "fix"
	Schedule   *PullSchedule   `json:"schedule,omitempty"`
	LastRun    string          `json:"lastRun,omitempty"`    // RFC3339 timestamp
	NextRun    string          `json:"nextRun,omitempty"`    // RFC3339 timestamp — set by scheduler so UI can render countdown without recomputing
	LastResult *DriftRunResult `json:"lastResult,omitempty"`
}

// DriftRunResult is the summary written by DriftWatcher after each run.
// Persists to clonarr.json so UI can render "last check: X ago" without an
// in-memory cache that resets on restart.
type DriftRunResult struct {
	DriftsDetected int      `json:"driftsDetected"`
	DriftsFixed    int      `json:"driftsFixed"`
	Errors         []string `json:"errors,omitempty"`
}

// (ProfileSync types + migrateProfileSync moved to profile_sync.go.)

// DriftResult is the per-rule (or per-QS-set) outcome from one DriftWatcher
// run. Stored in the watcher's in-memory cache, surfaced via /api/watch/drift.
// Not persisted to clonarr.json — recomputed on next watcher tick.
type DriftResult struct {
	RuleID        string        `json:"ruleId,omitempty"`
	QSType        string        `json:"qsType,omitempty"` // for QS drift
	CheckedAt     string        `json:"checkedAt"`        // RFC3339
	DriftDetected bool          `json:"driftDetected"`
	DriftSummary  []string      `json:"driftSummary,omitempty"` // human-readable
	Details       []DriftDetail `json:"details,omitempty"`
}

// DriftDetail is one field-level diff between current Arr state and the
// clonarr target. Used to render the per-card expand-on-click summary.
//
// NOTE: Current/Target are `any` so they can hold scores (int), names
// (string), or quality-item lists. On JSON round-trip, numeric values come
// back as float64 — downstream diff-equality logic must normalize.
type DriftDetail struct {
	Field   string `json:"field"`            // "score" | "cf-membership" | "cutoff" | ...
	CFName  string `json:"cfName,omitempty"` // for CF-related drifts
	Current any    `json:"current"`
	Target  any    `json:"target"`
}

// SyncSchedule is the DEPRECATED v2 periodic force-sync schedule. Retained
// only so v2 configs still parse on load — its single load-time migration
// (ConfigStore.load) converts an enabled schedule to Auto-sync drift
// detection, then clears the field. Nothing writes it anymore; only
// Enabled is read.
type SyncSchedule struct {
	Enabled bool `json:"enabled"`
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

// AutoSyncConfig holds global auto-sync settings, notification agents, and rules.
// This is the top-level configuration object for the auto-sync subsystem.
type AutoSyncConfig struct {
	Enabled bool `json:"enabled"`
	// Paused is a global kill-switch for non-user-initiated sync. When true,
	// AutoSyncAfterPull (run on TRaSH pull and on container startup) skips
	// every rule. Manual actions — "Sync All", per-rule "Sync now", per-
	// profile "Save & Sync" — are unaffected. Default false.
	Paused bool `json:"paused,omitempty"`
	// NotificationAgents stores zero or more independently configured notification
	// providers. Multiple enabled entries are supported, including multiple entries
	// of the same Type (e.g. two Discord channels for different alert levels).
	// The full provider configuration and lifecycle are managed by the agents package.
	NotificationAgents []NotificationAgent `json:"notificationAgents,omitempty"`
	Rules              []AutoSyncRule      `json:"rules,omitempty"`
}

// NotificationAgent is the config shape of one notification provider entry.
// This is a type alias for agents.Agent — the canonical definition and all
// provider logic live in the agents sub-package. The alias allows the rest
// of the core package to use the domain-specific name.
type NotificationAgent = agents.Agent

// AgentEvents controls which application events trigger notifications for an agent.
// Type alias for agents.Events.
type AgentEvents = agents.Events

// NotificationConfig holds provider-specific credentials and options for one agent.
// This is a union struct — each provider uses only its relevant fields.
// Adding a new provider requires extending agents.Config and registering a
// Provider implementation.
type NotificationConfig = agents.Config

// AutoSyncRule defines one auto-sync binding (profile → instance).
type AutoSyncRule struct {
	ID                string   `json:"id"`
	Enabled           bool     `json:"enabled"`
	InstanceID        string   `json:"instanceId"`
	ProfileSource     string   `json:"profileSource"` // "trash" or "imported"
	TrashProfileID    string   `json:"trashProfileId,omitempty"`
	ImportedProfileID string   `json:"importedProfileId,omitempty"`
	ArrProfileID      int      `json:"arrProfileId"`          // target Arr profile to update
	// SelectedCFs: explicit user opt-ins — CFs the user wants synced beyond
	// what TRaSH already defaults for this profile. Layered additively over
	// ComputeTrashDefaults at sync time. Pre-v2.5.8 rules stored the full
	// sync set here (including CFs that were also TRaSH defaults); the v2.5.8
	// migration splits inherited defaults out so this field becomes pure
	// opt-ins. Duplicates with trash_defaults are harmless (set union).
	SelectedCFs []string `json:"selectedCFs,omitempty"`
	// ExcludedCFs: explicit user opt-outs — CFs that ARE in TRaSH defaults
	// for this profile (formatItems or default-on group) but the user
	// chose to remove via the rule editor. Subtracted from the resolved
	// effective set at sync time. Persists across syncs and across TRaSH
	// structural changes — opt-outs survive even if TRaSH moves the CF
	// to a different group. Empty for fresh rules.
	ExcludedCFs []string `json:"excludedCFs,omitempty"`
	// KeepArrCFIDs lists Arr CF IDs that must NOT be zeroed by ResetMode='reset_to_zero'.
	// Populated by Compare-flow apply when the user opts to keep CFs in the "Extra in Arr"
	// section (Arr-only customs not in any TRaSH cf-group, e.g. user-imported release-group
	// CFs like FLUX / SiC). Without this, Sync All would zero them on every run.
	// Empty for rules created via Save & Sync from profile-detail editor — that flow
	// uses scoreOverrides/extraCFs (trash-id keyed) which doesn't apply to Arr-only CFs.
	KeepArrCFIDs     []int           `json:"keepArrCFIDs,omitempty"`
	ScoreOverrides   map[string]int  `json:"scoreOverrides,omitempty"`   // per-CF score overrides (trash_id → score)
	QualityOverrides map[string]bool `json:"qualityOverrides,omitempty"` // legacy flat quality override (name → allowed). Used when QualityStructure is empty.
	QualityStructure []QualityItem   `json:"qualityStructure,omitempty"` // full structure override (replaces TRaSH items). Trumps QualityOverrides when set.
	Behavior         *SyncBehavior   `json:"behavior,omitempty"`         // sync behavior rules (nil = defaults)
	Overrides        *SyncOverrides  `json:"overrides,omitempty"`        // user overrides (min score, language, cutoff, etc.)
	// Description: free-form user notes about this sync rule — what the
	// profile is for, why specific customizations were made, etc.
	// Markdown supported (rendered via the same minimal subset as CF
	// descriptions). Surfaced as a hover-info icon next to the Arr
	// profile name in the Sync Rules table.
	Description string `json:"description,omitempty"`
	LastSyncCommit string `json:"lastSyncCommit,omitempty"`
	// LastSyncTime is the timestamp of the last SUCCESSFUL sync — bumped by
	// the auto-sync engine (UpdateAutoSyncRuleCommit), manual /api/sync/apply,
	// and Restore. NOT bumped on sync failure (UpdateAutoSyncRuleError leaves
	// it alone) so the "● unsynced" indicator survives across failed auto-sync
	// ticks until the user fixes the underlying error.
	LastSyncTime  string `json:"lastSyncTime,omitempty"`
	LastSyncError string `json:"lastSyncError,omitempty"`
	// UpdatedAt is bumped every time the rule's user-facing settings change
	// (Save-only PUT with a real diff, sync apply success, auto-sync engine
	// success). Compared against LastSyncTime to surface "● unsynced" on the
	// rule card when saved overrides haven't been pushed to Arr yet.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// PriorAvailableGroups is a snapshot of which cf-groups were available
	// for this rule's profile at last successful sync. Map of
	// group_trash_id → was_default_enabled_at_last_sync. Used to detect
	// brand-new groups added by TRaSH restructures so they aren't auto-
	// disabled by restoreFromSyncHistory's "no group activity = opted out"
	// heuristic. Empty for pre-fix rules; populated lazily at startup
	// via LastSyncCommit + git lookup, then on every successful sync.
	PriorAvailableGroups map[string]bool `json:"priorAvailableGroups,omitempty"`
	// PriorSyncedCFs is a CF-level snapshot from the last successful sync —
	// trash_ids of every CF that ended up scored in the Arr profile, no
	// matter whether it was reached via profile.formatItems, an explicit
	// SelectedCF, or a group-default expansion. Populated on every
	// successful sync (auto + manual). Drives two recovery paths:
	//   1) ExpandSelectedCFsForBrandNewGroups uses it to re-include CFs
	//      that TRaSH moved from profile.formatItems INTO an existing
	//      default-on cf-group — the per-group PriorAvailableGroups check
	//      misses this case because the group itself isn't "brand new".
	//   2) restoreFromSyncHistory uses it so the editor preserves CFs
	//      that were previously synced via the formatItems direct path
	//      (which never lived in SelectedCFs), instead of silently
	//      flipping them off after the rule re-opens.
	// Empty for legacy rules until their first post-fix sync.
	PriorSyncedCFs []string `json:"priorSyncedCFs,omitempty"`
	// OrphanedAt is set (RFC3339 timestamp) when clonarr's drift-check
	// detects that ArrProfileID no longer resolves in the target Arr
	// instance. Auto-sync skips orphaned rules; the UI exposes Restore
	// (re-create profile in Arr from last synced intent) and Remove
	// (permanent delete) actions. Empty when the rule is in normal
	// operation. Soft-tombstone replaces the previous auto-delete cleanup.
	OrphanedAt string `json:"orphanedAt,omitempty"`
	// WatchState + PendingChanges — Profile Sync detection state per rule.
	// WatchState carries the SHA fingerprint of the most-recent affected-
	// trash-id set so notifications dedupe across consecutive detection
	// ticks. PendingChanges accumulates union-merged change events for
	// the rule (TRaSH-side today; Arr-drift detection adds entries later) — surfaced as the
	// rule's badge + backlog timeline in the UI. Both default empty.
	// Cleared when a successful sync applies the change set.
	WatchState     *WatchState     `json:"watchState,omitempty"`
	PendingChanges []PendingChange `json:"pendingChanges,omitempty"`
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

// ResolveSyncBehavior returns a fully populated SyncBehavior, filling in
// defaults for empty fields. Unrecognized values fall back to the default for
// that field rather than being passed through as-is — this keeps the sync
// engine on a known code path even if a stale client posts a removed mode.
func ResolveSyncBehavior(b *SyncBehavior) SyncBehavior {
	if b == nil {
		return DefaultSyncBehavior()
	}
	r := *b
	if !IsValidEnumValue(SyncBehaviorAddModes, r.AddMode) {
		r.AddMode = "add_missing"
	}
	if !IsValidEnumValue(SyncBehaviorRemoveModes, r.RemoveMode) {
		r.RemoveMode = "remove_custom"
	}
	if !IsValidEnumValue(SyncBehaviorResetModes, r.ResetMode) {
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
// The string slices come directly from the sync result's *Details fields,
// human-readable and display-ready (e.g. "BHDStudio: 1000 -> 2240").
// CFCommits resolves each "Updated: <name>" CFDetails entry back to the
// TRaSH-Guides commits that touched the CF between the prior sync and
// this one. Keyed by CF name. nil for pre-v3.0.x entries and for syncs
// where no TRaSH-side attribution was available.
type SyncChanges struct {
	CFDetails       []string                    `json:"cfDetails,omitempty"`
	ScoreDetails    []string                    `json:"scoreDetails,omitempty"`
	QualityDetails  []string                    `json:"qualityDetails,omitempty"`
	SettingsDetails []string                    `json:"settingsDetails,omitempty"`
	CFCommits       map[string][]TrashCommitRef `json:"cfCommits,omitempty"`
	// CFSpecDiffs is the Phase 2 structured CF spec diff per Updated /
	// Created entry. Keyed by CF name so the History UI joins it with
	// the matching "Updated: <CF>" or "Created: <CF>" row. Nil on
	// pre-Phase-2 entries; the UI skips rendering when absent.
	CFSpecDiffs     map[string]*CFSpecDiff      `json:"cfSpecDiffs,omitempty"`
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
	// ExcludedCFs mirrors the rule's user opt-outs at the time of this sync.
	// Snapshotted so rollback / orphaned-profile rerun can reconstruct the
	// historical state correctly — without it, rollback would re-include
	// every CF the user had previously opted out of (the rollback path
	// builds the body from this snapshot, not the live rule). Empty for
	// pre-v2.5.8 entries → omitempty → reset_to_defaults behaviour matches
	// pre-feature semantics.
	ExcludedCFs       []string        `json:"excludedCFs,omitempty"`
	ScoreOverrides    map[string]int  `json:"scoreOverrides,omitempty"`
	QualityOverrides  map[string]bool `json:"qualityOverrides,omitempty"` // legacy flat override (name → allowed)
	QualityStructure  []QualityItem   `json:"qualityStructure,omitempty"` // full structure override (trumps QualityOverrides)
	Overrides         *SyncOverrides  `json:"overrides,omitempty"`
	Behavior          *SyncBehavior   `json:"behavior,omitempty"`
	// KeepArrCFIDs mirrors the rule's pinned-extras list. Snapshotted on
	// every sync so a rerun from sync history (e.g. when the user has
	// deleted the rule, or for orphaned profiles) can still preserve the
	// Arr-only customs that were preserved on the original sync. Without
	// this snapshot, sync-history rerun would fall back to nil and the
	// reset_to_zero pass would wipe every pinned extra.
	KeepArrCFIDs  []int `json:"keepArrCFIDs,omitempty"`
	CFsCreated    int   `json:"cfsCreated"`
	CFsUpdated    int   `json:"cfsUpdated"`
	ScoresUpdated int   `json:"scoresUpdated"`
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
	// TriggerType records why this sync ran: "trash_update" (TRaSH-Guides
	// upstream advanced and AutoSync re-applied), "manual" (user clicked
	// Sync / Apply from the UI), "drift_apply" (drift detection rolled
	// the divergence back), "delayed_apply" (Wait-before-applying
	// schedule fired), "restore" (orphaned-profile restore), "rollback"
	// (revert to a previous history state). Empty on entries from before
	// the field existed; UI treats the absence as "Manual" so old rows
	// keep a sensible chip.
	TriggerType string `json:"triggerType,omitempty"`
	// OrphanedAt mirrors the field on the rule — set when the entry
	// belongs to a profile that has been detected as deleted in Arr.
	// Lets the UI gray out / badge orphaned history rows independently
	// of rule state (the rule may have been removed while history is
	// retained for diagnostics).
	OrphanedAt string `json:"orphanedAt,omitempty"`
	// Categories classifies what kind of state the sync touched, so the
	// History tab can filter to one channel at a time:
	//   - "profile" — entry changed profile-level state (cutoff, upgrade,
	//     quality items, scores, CF references on the profile).
	//   - "cf"      — entry touched a CF entity in Arr (created, updated,
	//     drift-applied).
	// A regular sync that touches both gets both tags; a drift-only
	// Apply gets only "cf"; a profile-only sync gets only "profile".
	// Empty on entries from before the field existed; the History tab
	// defaults to "All" filter so old rows stay visible without
	// migration.
	Categories []string `json:"categories,omitempty"`
}

// Instance represents a configured Radarr or Sonarr instance.
type Instance struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"` // "radarr" or "sonarr"
	URL    string `json:"url"`
	APIKey string `json:"apiKey"`
	// AutoSyncPaused, when true, skips non-user-initiated sync for this
	// instance only. AutoSyncAfterPull, the delayed-apply runner, and the
	// drift detector all silently drop rules belonging to a paused
	// instance. Manual actions ("Sync now", "Sync all", "Save & Sync"
	// from the profile editor) are unaffected. Default false. Replaces
	// the prior global AutoSync.Paused flag — see migrateGlobalPauseToInstances.
	AutoSyncPaused bool `json:"autoSyncPaused,omitempty"`

	// CFDriftFingerprints stores the sha256 fingerprint of the most
	// recent CFSpecDiff per managed CF on this instance. Key = CF trash
	// id; value = hex sha256 of the canonical diff form.
	//
	// Used by the CF spec drift detector to de-duplicate notifications:
	// identical fingerprint between two Check passes means the same
	// drift, do not re-notify. Fingerprint change means new or evolved
	// drift, fire NotifyCFDriftDetected. Empty / missing entry means no
	// drift on that CF, and a transition from non-empty to absent fires
	// NotifyCFDriftReconciled.
	//
	// Mirrors how AutoSyncRule.WatchState.LastDriftFingerprint persists
	// profile-drift state — kept on the entity the drift attaches to
	// (per-instance for CFs, per-rule for profiles) so each fingerprint
	// has a single canonical owner.
	//
	// Per-CF storage stays compact: ~64 bytes per drifted CF (trash id
	// plus hex fingerprint), and omitempty drops the map entirely from
	// the JSON for installations with no drift, so an instance never
	// pays storage cost for a feature it has not exercised.
	CFDriftFingerprints map[string]string `json:"cfDriftFingerprints,omitempty"`
}

// TrashRepo holds TRaSH-Guides repository settings.
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

	// Defend against legacy or hand-edited configs where pullSchedule.dayOfMonth
	// sits outside the API-validated 1..28 range. Without this, the scheduler
	// would silently disable (returns zero from nextPullTimeAt), with no UI
	// signal. Clamp to 28 + warn so the user gets a clue from the logs.
	if cs.config.PullSchedule != nil {
		if d := cs.config.PullSchedule.DayOfMonth; d < 1 || d > 28 {
			log.Printf("config: pullSchedule.dayOfMonth=%d clamped to 28 (months with fewer days make 29-31 unreliable)", d)
			cs.config.PullSchedule.DayOfMonth = 28
		}
	}
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

	// Profile Sync migration — populate ProfileSync from existing
	// PullInterval/PullSchedule the first time we load after upgrade. Idempotent
	// via the nil sentinel: once populated, subsequent loads skip this block.
	//
	// Defaults: Mode=auto + Sources={TrashUpstream:true, ArrDrift:false} —
	// reproduces today's Pull-and-sync behaviour. ArrDrift is opt-in (new
	// capability, off by default). Zero functional change for existing users
	// after the migration runs.
	profileSyncMigrated := cs.migrateProfileSync()

	// Migrate the deprecated v2 "Auto Sync Schedule" (periodic force-sync)
	// to the unified Auto-sync system. An enabled schedule meant "keep my
	// Arr profiles matching clonarr on a cadence", which Arr-drift detection
	// plus Apply-automatically now does precisely. Turn those on so the
	// user's intent survives the upgrade, then clear the old field so it
	// never lingers. Runs AFTER migrateProfileSync so ProfileSync already
	// carries the migrated Pull cadence (Interval/Specific) before we layer
	// the drift source + mode on top. Persisted below so the nil field
	// sticks to disk — without that, the on-disk schedule would re-trigger
	// this every boot.
	syncScheduleMigrated := false
	if cs.config.SyncSchedule != nil {
		if cs.config.SyncSchedule.Enabled {
			if cs.config.ProfileSync == nil {
				cs.config.ProfileSync = &ProfileSync{}
			}
			cs.config.ProfileSync.Sources.ArrDrift = true
			if cs.config.ProfileSync.Mode == "" {
				cs.config.ProfileSync.Mode = ProfileSyncModeAuto
			}
			log.Printf("config: migrated deprecated Auto Sync Schedule to Auto-sync drift detection (Direct edits in Radarr/Sonarr enabled)")
		}
		cs.config.SyncSchedule = nil
		syncScheduleMigrated = true
	}

	if profileSyncMigrated || syncScheduleMigrated {
		if err := cs.saveLocked(); err != nil {
			log.Printf("Warning: failed to persist Auto-sync migration: %v", err)
		}
	}
	return nil
}

// (migrateProfileSync moved to profile_sync.go.)

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
	if cs.config.PullSchedule != nil {
		ps := *cs.config.PullSchedule
		cfg.PullSchedule = &ps
	}
	if cs.config.DriftWatch != nil {
		dw := *cs.config.DriftWatch
		if cs.config.DriftWatch.Schedule != nil {
			ds := *cs.config.DriftWatch.Schedule
			dw.Schedule = &ds
		}
		if cs.config.DriftWatch.LastResult != nil {
			lr := *cs.config.DriftWatch.LastResult
			if len(cs.config.DriftWatch.LastResult.Errors) > 0 {
				lr.Errors = make([]string, len(cs.config.DriftWatch.LastResult.Errors))
				copy(lr.Errors, cs.config.DriftWatch.LastResult.Errors)
			}
			dw.LastResult = &lr
		}
		cfg.DriftWatch = &dw
	}
	if cs.config.ProfileSync != nil {
		ps := *cs.config.ProfileSync
		if cs.config.ProfileSync.Specific != nil {
			sp := *cs.config.ProfileSync.Specific
			ps.Specific = &sp
		}
		if cs.config.ProfileSync.LastResult != nil {
			lr := *cs.config.ProfileSync.LastResult
			if len(cs.config.ProfileSync.LastResult.Errors) > 0 {
				lr.Errors = make([]string, len(cs.config.ProfileSync.LastResult.Errors))
				copy(lr.Errors, cs.config.ProfileSync.LastResult.Errors)
			}
			ps.LastResult = &lr
		}
		cfg.ProfileSync = &ps
	}
	cfg.Instances = make([]Instance, len(cs.config.Instances))
	for i, inst := range cs.config.Instances {
		cfg.Instances[i] = inst
		if len(inst.CFDriftFingerprints) > 0 {
			cfg.Instances[i].CFDriftFingerprints = make(map[string]string, len(inst.CFDriftFingerprints))
			for k, v := range inst.CFDriftFingerprints {
				cfg.Instances[i].CFDriftFingerprints[k] = v
			}
		}
	}
	cfg.SyncHistory = make([]SyncHistoryEntry, len(cs.config.SyncHistory))
	for i, sh := range cs.config.SyncHistory {
		cfg.SyncHistory[i] = sh
		cfg.SyncHistory[i].SyncedCFs = make([]string, len(sh.SyncedCFs))
		copy(cfg.SyncHistory[i].SyncedCFs, sh.SyncedCFs)
		if len(sh.ExcludedCFs) > 0 {
			cfg.SyncHistory[i].ExcludedCFs = make([]string, len(sh.ExcludedCFs))
			copy(cfg.SyncHistory[i].ExcludedCFs, sh.ExcludedCFs)
		}
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
			if len(sh.Changes.CFCommits) > 0 {
				c.CFCommits = make(map[string][]TrashCommitRef, len(sh.Changes.CFCommits))
				for k, refs := range sh.Changes.CFCommits {
					if len(refs) == 0 {
						continue
					}
					copied := make([]TrashCommitRef, len(refs))
					copy(copied, refs)
					c.CFCommits[k] = copied
				}
			}
			if len(sh.Changes.CFSpecDiffs) > 0 {
				// Map of pointers - copy each diff to a fresh value
				// and re-point so callers can mutate the returned
				// config without leaking into the store.
				c.CFSpecDiffs = make(map[string]*CFSpecDiff, len(sh.Changes.CFSpecDiffs))
				for k, diff := range sh.Changes.CFSpecDiffs {
					if diff == nil {
						continue
					}
					copied := *diff
					c.CFSpecDiffs[k] = &copied
				}
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
			if len(r.ExcludedCFs) > 0 {
				cfg.AutoSync.Rules[i].ExcludedCFs = make([]string, len(r.ExcludedCFs))
				copy(cfg.AutoSync.Rules[i].ExcludedCFs, r.ExcludedCFs)
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
			if r.WatchState != nil {
				ws := *r.WatchState
				cfg.AutoSync.Rules[i].WatchState = &ws
			}
			if len(r.PendingChanges) > 0 {
				cfg.AutoSync.Rules[i].PendingChanges = append([]PendingChange(nil), r.PendingChanges...)
			}
			if len(r.PriorAvailableGroups) > 0 {
				cfg.AutoSync.Rules[i].PriorAvailableGroups = make(map[string]bool, len(r.PriorAvailableGroups))
				for k, v := range r.PriorAvailableGroups {
					cfg.AutoSync.Rules[i].PriorAvailableGroups[k] = v
				}
			}
			if len(r.PriorSyncedCFs) > 0 {
				cfg.AutoSync.Rules[i].PriorSyncedCFs = append([]string(nil), r.PriorSyncedCFs...)
			}
			if len(r.KeepArrCFIDs) > 0 {
				cfg.AutoSync.Rules[i].KeepArrCFIDs = append([]int(nil), r.KeepArrCFIDs...)
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
// Thread-safe operations for managing notification agents in the config.
// All methods acquire cs.mu and persist changes to disk atomically.

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

// GetInstance returns an instance by ID. Map fields on the returned value
// are deep-copied so callers can iterate without racing in-closure writes
// from Update (CFDriftFingerprints is mutated in-place by the drift apply
// path and by the drift detection persistence step).
func (cs *ConfigStore) GetInstance(id string) (Instance, bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, inst := range cs.config.Instances {
		if inst.ID == id {
			out := inst
			if len(inst.CFDriftFingerprints) > 0 {
				out.CFDriftFingerprints = make(map[string]string, len(inst.CFDriftFingerprints))
				for k, v := range inst.CFDriftFingerprints {
					out.CFDriftFingerprints[k] = v
				}
			}
			return out, true
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
