package core

import (
	"testing"
)

// TestApplyOrphanMarking_MarksFreshOrphan covers the basic mark transition:
// a rule (and its history entry) for an Arr profile that isn't in the valid
// set gets OrphanedAt set. A CleanupEvent is emitted for the user-facing
// notification path.
func TestApplyOrphanMarking_MarksFreshOrphan(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "rule-1", InstanceID: "inst-A", ArrProfileID: 10},
		}},
		SyncHistory: []SyncHistoryEntry{
			{InstanceID: "inst-A", ArrProfileID: 10, ProfileName: "Quality SD"},
		},
	}
	valid := map[string]map[int]bool{
		"inst-A": {1: true, 2: true}, // 10 is missing
	}
	now := "2026-04-27T12:00:00Z"

	events := applyOrphanMarking(cfg, valid, map[string]string{"inst-A": "Radarr"}, now)

	if cfg.AutoSync.Rules[0].OrphanedAt != now {
		t.Errorf("rule OrphanedAt: want %q, got %q", now, cfg.AutoSync.Rules[0].OrphanedAt)
	}
	if cfg.SyncHistory[0].OrphanedAt != now {
		t.Errorf("history OrphanedAt: want %q, got %q", now, cfg.SyncHistory[0].OrphanedAt)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
	if events[0].ProfileName != "Quality SD" || events[0].InstanceName != "Radarr" || events[0].ArrProfileID != 10 {
		t.Errorf("event mismatch: %+v", events[0])
	}
}

// TestApplyOrphanMarking_Idempotent verifies that running mark twice on
// an already-orphaned rule preserves the original timestamp and does NOT
// emit a duplicate event. This matters because cleanup runs on every Arr
// probe (TRaSH pull, History tab open) — repeated probes shouldn't bury
// the user in repeat notifications.
func TestApplyOrphanMarking_Idempotent(t *testing.T) {
	original := "2026-04-27T08:00:00Z"
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "r", InstanceID: "inst-A", ArrProfileID: 10, OrphanedAt: original},
		}},
		SyncHistory: []SyncHistoryEntry{
			{InstanceID: "inst-A", ArrProfileID: 10, ProfileName: "Old", OrphanedAt: original},
		},
	}
	valid := map[string]map[int]bool{"inst-A": {}}

	events := applyOrphanMarking(cfg, valid, nil, "2026-04-27T12:00:00Z")

	if cfg.AutoSync.Rules[0].OrphanedAt != original {
		t.Errorf("rule timestamp clobbered: want %q, got %q", original, cfg.AutoSync.Rules[0].OrphanedAt)
	}
	if cfg.SyncHistory[0].OrphanedAt != original {
		t.Errorf("history timestamp clobbered: want %q, got %q", original, cfg.SyncHistory[0].OrphanedAt)
	}
	if len(events) != 0 {
		t.Errorf("want no events on repeat, got %d", len(events))
	}
}

// TestApplyOrphanMarking_Reverses verifies that a previously-orphaned
// rule whose Arr profile reappears gets OrphanedAt cleared. Covers the
// case where the user manually recreates the profile in Arr (e.g. via
// API with the original ID), or restores from an Arr backup.
func TestApplyOrphanMarking_Reverses(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "r", InstanceID: "inst-A", ArrProfileID: 10, OrphanedAt: "2026-04-26T08:00:00Z"},
		}},
		SyncHistory: []SyncHistoryEntry{
			{InstanceID: "inst-A", ArrProfileID: 10, OrphanedAt: "2026-04-26T08:00:00Z"},
		},
	}
	valid := map[string]map[int]bool{"inst-A": {10: true}}

	applyOrphanMarking(cfg, valid, nil, "2026-04-27T12:00:00Z")

	if cfg.AutoSync.Rules[0].OrphanedAt != "" {
		t.Errorf("rule OrphanedAt should be cleared, got %q", cfg.AutoSync.Rules[0].OrphanedAt)
	}
	if cfg.SyncHistory[0].OrphanedAt != "" {
		t.Errorf("history OrphanedAt should be cleared, got %q", cfg.SyncHistory[0].OrphanedAt)
	}
}

// TestApplyOrphanMarking_SkipsUnreachable verifies that instances NOT
// in validProfiles (e.g. unreachable Arr at probe time) are left
// completely untouched — no marks added or cleared. Critical safety
// invariant: a network blip must not cascade into mass-orphaning.
func TestApplyOrphanMarking_SkipsUnreachable(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "r1", InstanceID: "inst-A", ArrProfileID: 10},
			{ID: "r2", InstanceID: "inst-B", ArrProfileID: 20, OrphanedAt: "2026-04-26T08:00:00Z"},
		}},
		SyncHistory: []SyncHistoryEntry{
			{InstanceID: "inst-A", ArrProfileID: 10},
			{InstanceID: "inst-B", ArrProfileID: 20, OrphanedAt: "2026-04-26T08:00:00Z"},
		},
	}
	// Neither instance was probed (e.g. both unreachable).
	valid := map[string]map[int]bool{}

	events := applyOrphanMarking(cfg, valid, nil, "2026-04-27T12:00:00Z")

	if cfg.AutoSync.Rules[0].OrphanedAt != "" {
		t.Errorf("rule r1 should be untouched (unreachable instance), got OrphanedAt=%q", cfg.AutoSync.Rules[0].OrphanedAt)
	}
	if cfg.AutoSync.Rules[1].OrphanedAt != "2026-04-26T08:00:00Z" {
		t.Errorf("rule r2 OrphanedAt should be preserved (unreachable instance), got %q", cfg.AutoSync.Rules[1].OrphanedAt)
	}
	if cfg.SyncHistory[0].OrphanedAt != "" {
		t.Errorf("history h1 should be untouched")
	}
	if cfg.SyncHistory[1].OrphanedAt != "2026-04-26T08:00:00Z" {
		t.Errorf("history h2 OrphanedAt should be preserved")
	}
	if len(events) != 0 {
		t.Errorf("want no events for unreachable instances, got %d", len(events))
	}
}

// TestApplyOrphanMarking_EmptyProfileListMarksAll verifies the soft-
// tombstone safety property: when an instance returns 0 profiles
// (intentionally empty, not unreachable), every rule on that instance
// is marked orphaned. This is safe because OrphanedAt is reversible —
// a transient empty response gets cleared on the next probe when
// profiles return.
func TestApplyOrphanMarking_EmptyProfileListMarksAll(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "r1", InstanceID: "inst-A", ArrProfileID: 10},
			{ID: "r2", InstanceID: "inst-A", ArrProfileID: 20},
		}},
	}
	// Instance probed successfully, returned 0 profiles.
	valid := map[string]map[int]bool{"inst-A": {}}
	now := "2026-04-27T12:00:00Z"

	applyOrphanMarking(cfg, valid, nil, now)

	for _, r := range cfg.AutoSync.Rules {
		if r.OrphanedAt != now {
			t.Errorf("rule %s should be marked orphaned (empty profile list), got %q", r.ID, r.OrphanedAt)
		}
	}
}

// TestApplyOrphanMarking_DedupesEventsPerProfile verifies that when a
// profile has multiple history entries (same ArrProfileID, multiple
// syncs), only ONE CleanupEvent is emitted on first orphan transition.
// Otherwise the user gets a wall of identical "X deleted in Arr" toasts
// for a single profile, one per past sync entry.
func TestApplyOrphanMarking_DedupesEventsPerProfile(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			{ID: "r", InstanceID: "inst-A", ArrProfileID: 10},
		}},
		SyncHistory: []SyncHistoryEntry{
			{InstanceID: "inst-A", ArrProfileID: 10, ProfileName: "Foo"},
			{InstanceID: "inst-A", ArrProfileID: 10, ProfileName: "Foo"},
			{InstanceID: "inst-A", ArrProfileID: 10, ProfileName: "Foo"},
		},
	}
	valid := map[string]map[int]bool{"inst-A": {}}

	events := applyOrphanMarking(cfg, valid, nil, "2026-04-27T12:00:00Z")

	if len(events) != 1 {
		t.Errorf("want 1 dedup'd event, got %d", len(events))
	}
	// All 3 history entries should still get OrphanedAt set.
	for i, h := range cfg.SyncHistory {
		if h.OrphanedAt == "" {
			t.Errorf("history[%d] not marked orphaned", i)
		}
	}
}

// TestApplyOrphanMarking_MixedTransitions exercises mark + clear in the
// same pass, across two instances, to confirm the function handles the
// realistic case where one Arr lost a profile while another gained one.
func TestApplyOrphanMarking_MixedTransitions(t *testing.T) {
	cfg := &Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{
			// Rule on inst-A: profile 10 was alive, now gone → mark
			{ID: "r1", InstanceID: "inst-A", ArrProfileID: 10},
			// Rule on inst-B: profile 20 was orphaned, now back → clear
			{ID: "r2", InstanceID: "inst-B", ArrProfileID: 20, OrphanedAt: "2026-04-26"},
		}},
	}
	valid := map[string]map[int]bool{
		"inst-A": {1: true},        // 10 missing
		"inst-B": {20: true},       // 20 reappeared
	}
	now := "2026-04-27T12:00:00Z"

	applyOrphanMarking(cfg, valid, nil, now)

	if cfg.AutoSync.Rules[0].OrphanedAt != now {
		t.Errorf("r1 should be newly orphaned")
	}
	if cfg.AutoSync.Rules[1].OrphanedAt != "" {
		t.Errorf("r2 should be cleared")
	}
}
