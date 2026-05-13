package core

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestIsConnectionError covers every classifier branch the auto-disable
// gate and friendly-notification routing depend on. A false negative here
// turns a transient Arr restart into a permanently-disabled sync rule;
// a false positive masks a real config error as "Arr is unreachable".
func TestIsConnectionError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"connection refused", `Get "http://arr:8989/api/v3/customformat": dial tcp 192.168.1.1:8989: connect: connection refused`, true},
		{"no such host", `Get "http://arr:8989/api/v3/customformat": dial tcp: lookup arr: no such host`, true},
		{"network unreachable", `Get "http://arr:8989/api/v3/customformat": dial tcp 192.168.1.1:8989: connect: network is unreachable`, true},
		{"server closed idle connection", `Get "http://arr:8989/api/v3/customformat": http: server closed idle connection`, true},
		{"EOF suffix", `request failed: Get "http://arr:8989/api/v3/customformat": EOF`, true},
		{"unexpected EOF", `unexpected EOF while reading response`, true},
		{"broken pipe", `write tcp 1.2.3.4:5000->5.6.7.8:8989: write: broken pipe`, true},
		{"i/o timeout", `Get "http://arr:8989/...": dial tcp 192.168.1.1:8989: i/o timeout`, true},
		{"connection reset", `read tcp 1.2.3.4:5000->5.6.7.8:8989: read: connection reset by peer`, true},
		{"TLS handshake timeout", `Get "https://arr:443/...": net/http: TLS handshake timeout`, true},
		{"Client.Timeout exceeded", `Get "http://arr:8989/...": net/http: request canceled (Client.Timeout exceeded while awaiting headers)`, true},
		// Must NOT match
		{"HTTP 500 from Arr", `list CFs: HTTP 500: Internal Server Error`, false},
		{"HTTP 401", `list CFs: HTTP 401: Unauthorized`, false},
		{"profile not found", `TRaSH profile abc123 not found`, false},
		{"item not found", `item xyz not found`, false},
		{"empty", ``, false},
		{"EOF mid-message false-positive guard", `Some message about EOF handling that is not actually an error`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.msg != "" {
				err = errors.New(tc.msg)
			}
			got := IsConnectionError(err)
			if got != tc.want {
				t.Errorf("IsConnectionError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestFriendlyAutoSyncError covers the user-facing message routing.
func TestFriendlyAutoSyncError(t *testing.T) {
	cases := []struct {
		name         string
		msg          string
		shuttingDown bool
		mustContain  []string
		mustNotMatch []string
	}{
		{"401 auth", "list CFs: HTTP 401: Unauthorized", false, []string{"rejected the request", "API key"}, nil},
		{"403 forbidden", "list CFs: HTTP 403: Forbidden", false, []string{"rejected the request"}, nil},
		{"404 not found", "list CFs: HTTP 404: Not Found", false, []string{"not found", "deleted on the Arr side"}, nil},
		{"409 conflict", "update CF: HTTP 409: Conflict", false, []string{"rejected the sync as invalid"}, nil},
		{"422 unprocessable", "update CF: HTTP 422: Unprocessable Entity", false, []string{"rejected the sync as invalid"}, nil},
		{"500 server error", "list CFs: HTTP 500: Internal Server Error", false, []string{"returned a server error"}, nil},
		{"502 bad gateway", "list CFs: HTTP 502: Bad Gateway", false, []string{"returned a server error"}, nil},
		{"503 unavailable", "list CFs: HTTP 503: Service Unavailable", false, []string{"returned a server error"}, nil},
		{"504 gateway timeout", "list CFs: HTTP 504: Gateway Timeout", false, []string{"returned a server error"}, nil},
		// `HTTP 5` substring inside an Arr 422 body must NOT route as a server error.
		{"HTTP 5 false-positive guard", `list CFs: HTTP 422: {"error": "rate-limited by HTTP 503 fallback handler"}`, false, []string{"rejected the sync as invalid"}, []string{"returned a server error"}},
		{"deadline exceeded", "request failed: context deadline exceeded", false, []string{"did not respond in time"}, nil},
		{"context canceled — shutdown", "request failed: context canceled", true, []string{"clonarr is shutting down"}, nil},
		{"context canceled — not shutdown", "request failed: context canceled", false, []string{"will retry on next sync"}, []string{"shutting down"}},
		{"parse failure", "parse CFs: unexpected end of JSON input", false, []string{"unexpected response"}, nil},
		{"fallback strips URL prefix", `request failed: Get "http://arr:8989/api/v3/customformat": some weird error`, false, []string{"Radarr", "some weird error"}, []string{`Get "`, "http://"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New(tc.msg)
			got := FriendlyAutoSyncError(err, "Radarr", tc.shuttingDown)
			for _, want := range tc.mustContain {
				if want != "" && !strings.Contains(got, want) {
					t.Errorf("got %q, missing substring %q", got, want)
				}
			}
			for _, banned := range tc.mustNotMatch {
				if strings.Contains(got, banned) {
					t.Errorf("got %q, must not contain %q", got, banned)
				}
			}
		})
	}
	if got := FriendlyAutoSyncError(nil, "Radarr", false); got != "" {
		t.Errorf("nil err should produce empty string, got %q", got)
	}
}

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
		"inst-A": {1: true},  // 10 missing
		"inst-B": {20: true}, // 20 reappeared
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

func seedTrashGroupGitCommit(t *testing.T, dataDir string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataDir, "docs", "json", "radarr", "cf-groups"), 0755); err != nil {
		t.Fatalf("mkdir cf-groups: %v", err)
	}
	groupJSON := `{
  "name": "Test Group",
  "trash_id": "group-1",
  "default": "true",
  "quality_profiles": { "include": { "profile": "profile-1" } }
}`
	if err := os.WriteFile(filepath.Join(dataDir, "docs", "json", "radarr", "cf-groups", "group.json"), []byte(groupJSON), 0644); err != nil {
		t.Fatalf("write cf-group: %v", err)
	}

	cmds := [][]string{
		{"git", "-C", dataDir, "init", "-q"},
		{"git", "-C", dataDir, "config", "user.email", "test@example.com"},
		{"git", "-C", dataDir, "config", "user.name", "test"},
		{"git", "-C", dataDir, "config", "commit.gpgsign", "false"},
		{"git", "-C", dataDir, "add", "docs/json/radarr/cf-groups/group.json"},
		{"git", "-C", dataDir, "commit", "-q", "-m", "groups"},
	}
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(c, " "), err, out)
		}
	}
	out, err := exec.Command("git", "-C", dataDir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestMigratePriorAvailableGroupsLeavesNilWhenGitLookupFails(t *testing.T) {
	dir := t.TempDir()
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:             "rule-1",
			TrashProfileID: "profile-1",
			LastSyncCommit: "missing-commit",
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{
		Config:   cfg,
		Trash:    NewTrashStore(dir),
		DebugLog: NewDebugLogger(dir),
	}

	app.MigratePriorAvailableGroups()

	got := cfg.Get().AutoSync.Rules[0].PriorAvailableGroups
	if got != nil {
		t.Fatalf("PriorAvailableGroups = %#v, want nil so migration can retry", got)
	}
}

func TestMigratePriorAvailableGroupsPopulatesFromCommit(t *testing.T) {
	dir := t.TempDir()
	commit := seedTrashGroupGitCommit(t, filepath.Join(dir, "trash-guides"))
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:             "rule-1",
			TrashProfileID: "profile-1",
			LastSyncCommit: commit,
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{
		Config:   cfg,
		Trash:    NewTrashStore(dir),
		DebugLog: NewDebugLogger(dir),
	}

	app.MigratePriorAvailableGroups()

	got := cfg.Get().AutoSync.Rules[0].PriorAvailableGroups
	if got == nil || !got["group-1"] {
		t.Fatalf("PriorAvailableGroups = %#v, want group-1=true", got)
	}
}

func TestForceSyncAllRulesWithNoTrashCommitSkipsMigration(t *testing.T) {
	dir := t.TempDir()
	commit := seedTrashGroupGitCommit(t, filepath.Join(dir, "trash-guides"))
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:             "rule-1",
			Enabled:        true,
			TrashProfileID: "profile-1",
			LastSyncCommit: commit,
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{
		Config:   cfg,
		Trash:    NewTrashStore(dir),
		DebugLog: NewDebugLogger(dir),
	}

	app.ForceSyncAllRules()

	got := cfg.Get().AutoSync.Rules[0].PriorAvailableGroups
	if got != nil {
		t.Fatalf("PriorAvailableGroups = %#v, want nil when current TRaSH commit is empty", got)
	}
}

// TestCleanupDanglingCustomCFsOnRule_StripsBothMaps verifies the helper
// removes "custom:" IDs from BOTH SelectedCFs (slice) and ScoreOverrides
// (map), persists via Config.Update, and reports the removed IDs.
func TestCleanupDanglingCustomCFsOnRule_StripsBothMaps(t *testing.T) {
	dir := t.TempDir()
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:          "rule-1",
			SelectedCFs: []string{"trash-keep-1", "custom:dead1", "custom:keep-1", "custom:dead2"},
			ScoreOverrides: map[string]int{
				"trash-keep-2":   500,
				"custom:dead1":   100,
				"custom:keep-2":  200,
				"custom:dead3":   -50, // score-only orphan, not in SelectedCFs
			},
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{Config: cfg, DebugLog: NewDebugLogger(dir)}

	removed := app.CleanupDanglingCustomCFsOnRule("rule-1", []string{"custom:dead1", "custom:dead2", "custom:dead3"})

	got := cfg.Get().AutoSync.Rules[0]
	wantSelected := map[string]bool{"trash-keep-1": true, "custom:keep-1": true}
	if len(got.SelectedCFs) != len(wantSelected) {
		t.Fatalf("SelectedCFs after cleanup: got %v, want %d entries", got.SelectedCFs, len(wantSelected))
	}
	for _, id := range got.SelectedCFs {
		if !wantSelected[id] {
			t.Errorf("SelectedCFs unexpectedly retained %q", id)
		}
	}
	wantScores := map[string]int{"trash-keep-2": 500, "custom:keep-2": 200}
	if len(got.ScoreOverrides) != len(wantScores) {
		t.Fatalf("ScoreOverrides after cleanup: got %v, want %v", got.ScoreOverrides, wantScores)
	}
	for k, v := range wantScores {
		if got.ScoreOverrides[k] != v {
			t.Errorf("ScoreOverrides[%q] = %d, want %d", k, got.ScoreOverrides[k], v)
		}
	}
	if len(removed) != 3 {
		t.Errorf("removed IDs count = %d, want 3 (custom:dead1, custom:dead2, custom:dead3)", len(removed))
	}
	for _, id := range removed {
		if id != "custom:dead1" && id != "custom:dead2" && id != "custom:dead3" {
			t.Errorf("unexpected removed ID: %q", id)
		}
	}
}

// TestCleanupDanglingCustomCFsOnRule_NoopOnEmptyOrTrashOnly verifies the
// helper is a safe no-op for empty inputs and rejects non-"custom:"
// prefixed IDs (TRaSH orphans must NOT be auto-cleaned).
func TestCleanupDanglingCustomCFsOnRule_NoopOnEmptyOrTrashOnly(t *testing.T) {
	dir := t.TempDir()
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:             "rule-1",
			SelectedCFs:    []string{"trash-1", "custom:keep-1"},
			ScoreOverrides: map[string]int{"trash-1": 100, "custom:keep-1": 200},
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{Config: cfg, DebugLog: NewDebugLogger(dir)}

	// Empty input: no-op
	if got := app.CleanupDanglingCustomCFsOnRule("rule-1", nil); got != nil {
		t.Errorf("nil input: got removed=%v, want nil", got)
	}

	// TRaSH IDs are filtered out — must not be cleaned
	if got := app.CleanupDanglingCustomCFsOnRule("rule-1", []string{"trash-orphan-1", "trash-orphan-2"}); got != nil {
		t.Errorf("TRaSH IDs: got removed=%v, want nil (TRaSH IDs must survive)", got)
	}

	// Rule unchanged
	after := cfg.Get().AutoSync.Rules[0]
	if len(after.SelectedCFs) != 2 || len(after.ScoreOverrides) != 2 {
		t.Errorf("rule mutated by no-op call: SelectedCFs=%v ScoreOverrides=%v", after.SelectedCFs, after.ScoreOverrides)
	}
}

// TestCleanupDanglingCustomCFsOnRule_UnknownRule verifies the helper does
// not panic or mutate other rules when the targeted ruleID doesn't exist.
func TestCleanupDanglingCustomCFsOnRule_UnknownRule(t *testing.T) {
	dir := t.TempDir()
	cfg := NewConfigStore(dir)
	if err := cfg.Set(&Config{
		AutoSync: AutoSyncConfig{Rules: []AutoSyncRule{{
			ID:          "rule-1",
			SelectedCFs: []string{"custom:dead1"},
		}}},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &App{Config: cfg, DebugLog: NewDebugLogger(dir)}

	if got := app.CleanupDanglingCustomCFsOnRule("nonexistent-rule", []string{"custom:dead1"}); got != nil {
		t.Errorf("unknown rule: got removed=%v, want nil", got)
	}
	// rule-1 must remain untouched
	if got := cfg.Get().AutoSync.Rules[0].SelectedCFs; len(got) != 1 || got[0] != "custom:dead1" {
		t.Errorf("rule-1 mutated by call to other ruleID: SelectedCFs=%v", got)
	}
}
