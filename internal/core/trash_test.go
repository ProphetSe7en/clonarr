package core

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedTrashStoreForReset(t *testing.T) (*TrashStore, string) {
	t.Helper()
	dir := t.TempDir()
	ts := NewTrashStore(dir)

	if err := os.MkdirAll(filepath.Join(dir, "trash-guides", ".git"), 0755); err != nil {
		t.Fatalf("mkdir trash-guides: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "trash-guides", "sentinel.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("write repo sentinel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-pull.txt"), []byte("2026-05-11T12:00:00Z"), 0644); err != nil {
		t.Fatalf("write last-pull: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-pull-diff.json"), []byte(`{"prevCommit":"old","newCommit":"abc123","summary":"changed","time":"2026-05-11T12:00:00Z"}`), 0644); err != nil {
		t.Fatalf("write last diff: %v", err)
	}

	ts.data = &TrashData{
		LastPull:   time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC),
		CommitHash: "abc123",
		CommitDate: "2026-05-11 12:00:00 +0000",
		LastDiff: &PullDiff{
			PrevCommit: "old",
			NewCommit:  "abc123",
			Summary:    "changed",
			Time:       "2026-05-11T12:00:00Z",
		},
		Changelog: []ChangelogSection{{Date: "2026-05-11"}},
		Radarr: AppData{
			CustomFormats: map[string]*TrashCF{"cf1": {TrashID: "cf1"}},
			CFGroups:      []*TrashCFGroup{{Name: "group"}},
			Profiles:      []*TrashQualityProfile{{Name: "profile"}},
		},
		Sonarr: AppData{
			CustomFormats: map[string]*TrashCF{"cf2": {TrashID: "cf2"}},
			CFGroups:      []*TrashCFGroup{{Name: "group"}},
			Profiles:      []*TrashQualityProfile{{Name: "profile"}},
		},
	}
	ts.pullError = "previous failure"
	ts.lastChangelogDate = "2026-05-11"

	return ts, dir
}

func assertPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or stat failed with non-missing error: %v", path, err)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s missing: %v", path, err)
	}
}

func TestTrashStoreResetClearsLocalTrashData(t *testing.T) {
	ts, dir := seedTrashStoreForReset(t)

	if err := ts.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	assertPathMissing(t, filepath.Join(dir, "trash-guides"))
	assertPathMissing(t, filepath.Join(dir, "last-pull.txt"))
	assertPathMissing(t, filepath.Join(dir, "last-pull-diff.json"))

	st := ts.Status()
	if st.Cloned {
		t.Fatalf("Cloned = true, want false")
	}
	if st.CommitHash != "" {
		t.Fatalf("CommitHash = %q, want empty", st.CommitHash)
	}
	if st.LastPull != "" {
		t.Fatalf("LastPull = %q, want empty", st.LastPull)
	}
	if st.LastDiff != nil {
		t.Fatalf("LastDiff = %#v, want nil", st.LastDiff)
	}
	if st.PullError != "" {
		t.Fatalf("PullError = %q, want empty", st.PullError)
	}
	if st.RadarrCFs != 0 || st.SonarrCFs != 0 || st.RadarrGroups != 0 || st.SonarrGroups != 0 || st.RadarrProfs != 0 || st.SonarrProfs != 0 {
		t.Fatalf("counts not cleared: %+v", st)
	}
	if ts.lastChangelogDate != "" {
		t.Fatalf("lastChangelogDate = %q, want empty", ts.lastChangelogDate)
	}
}

func TestTrashStoreResetIdempotentWhenFilesMissing(t *testing.T) {
	ts := NewTrashStore(t.TempDir())

	if err := ts.Reset(); err != nil {
		t.Fatalf("first Reset: %v", err)
	}
	if err := ts.Reset(); err != nil {
		t.Fatalf("second Reset: %v", err)
	}

	st := ts.Status()
	if st.Cloned || st.CommitHash != "" || st.LastDiff != nil || st.PullError != "" {
		t.Fatalf("status not empty after idempotent reset: %+v", st)
	}
}

func TestTrashStoreResetBusyLeavesFilesIntact(t *testing.T) {
	ts, dir := seedTrashStoreForReset(t)

	ts.pullMu.Lock()
	err := ts.Reset()
	ts.pullMu.Unlock()

	if !errors.Is(err, ErrTrashBusy) {
		t.Fatalf("Reset error = %v, want ErrTrashBusy", err)
	}
	assertPathExists(t, filepath.Join(dir, "trash-guides"))
	assertPathExists(t, filepath.Join(dir, "last-pull.txt"))
	assertPathExists(t, filepath.Join(dir, "last-pull-diff.json"))

	st := ts.Status()
	if !st.Cloned || st.CommitHash == "" || st.LastDiff == nil || st.PullError == "" {
		t.Fatalf("status was unexpectedly cleared while busy: %+v", st)
	}
}

func TestTrashStoreResetWaitsForRepoReaders(t *testing.T) {
	ts, dir := seedTrashStoreForReset(t)

	ts.repoMu.RLock()
	done := make(chan error, 1)
	go func() {
		done <- ts.Reset()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if ts.Status().Pulling {
			break
		}
		if time.Now().After(deadline) {
			ts.repoMu.RUnlock()
			t.Fatalf("timed out waiting for Reset to acquire pull lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-done:
		ts.repoMu.RUnlock()
		t.Fatalf("Reset completed while repo read lock was held: %v", err)
	default:
	}
	assertPathExists(t, filepath.Join(dir, "trash-guides"))
	assertPathExists(t, filepath.Join(dir, "last-pull.txt"))
	assertPathExists(t, filepath.Join(dir, "last-pull-diff.json"))

	ts.repoMu.RUnlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Reset after repo read lock released: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Reset after repo read lock released")
	}

	assertPathMissing(t, filepath.Join(dir, "trash-guides"))
	assertPathMissing(t, filepath.Join(dir, "last-pull.txt"))
	assertPathMissing(t, filepath.Join(dir, "last-pull-diff.json"))
}

// CompareCFCategories drives the unified group-sort across both backend and
// frontend (the JS _compareCFCategories mirrors this). The contract:
//   - Tier 0: regular TRaSH categories (alphabetical within tier)
//   - Tier 1: SQP-prefix categories
//   - Tier 2: "Other" / unrecognised
//   - Tier 3: Custom
//
// Within tier, alphabetical on category name.
func TestCompareCFCategories_Tiering(t *testing.T) {
	cases := []struct {
		a, b string
		want int // -1, 0, +1
		desc string
	}{
		// Within tier 0 — pure alphabetical
		{"Audio", "HDR Formats", -1, "tier-0 alphabetical: A before H"},
		{"HDR Formats", "Audio", 1, "tier-0 alphabetical: reverse"},
		{"Audio", "Audio", 0, "same category equal"},
		// Tier 0 wins over tier 1
		{"Anime", "SQP", -1, "tier-0 before tier-1 SQP"},
		{"SQP-1", "Audio", 1, "tier-1 SQP-1 after tier-0 Audio"},
		// Tier 0 wins over tier 3 Custom
		{"Audio", "Custom", -1, "tier-0 before tier-3 Custom"},
		{"Custom", "Audio", 1, "Custom after regular category"},
		// Tier 1: all SQP-prefix grouped together, alphabetical within
		{"SQP", "SQP-1", -1, "SQP before SQP-1 alphabetical"},
		{"SQP-1", "SQP-4 (MA Hybrid)", -1, "alphabetical within SQP tier"},
		{"SQP-4 (MA Hybrid) Optional", "SQP-1", 1, "alphabetical reverse"},
		// Other goes between SQP and Custom
		{"SQP-anything", "Other", -1, "SQP-anything before Other"},
		{"Other", "Custom", -1, "Other before Custom"},
		// Empty-string treated as Other
		{"", "Custom", -1, "empty string treated as Other, before Custom"},
		{"Audio", "", -1, "tier-0 before empty-string Other"},
	}
	for _, c := range cases {
		got := CompareCFCategories(c.a, c.b)
		// Normalise to -1/0/+1
		switch {
		case got < 0:
			got = -1
		case got > 0:
			got = 1
		}
		if got != c.want {
			t.Errorf("%s: CompareCFCategories(%q, %q) = %d, want %d",
				c.desc, c.a, c.b, got, c.want)
		}
	}
}

// CompareCFGroups drives sort across cf-groups using the TRaSH `group` integer.
// The contract:
//   - Tier 3 (custom): user-authored groups always last
//   - Tier 1 (has explicit Group): sorted by integer, alphabetical tiebreak
//   - Tier 2 (no Group): alphabetical fallback in middle band
func TestCompareCFGroups_Tiering(t *testing.T) {
	intp := func(n int) *int { return &n }

	cases := []struct {
		desc    string
		aName   string
		aGroup  *int
		aCustom bool
		bName   string
		bGroup  *int
		bCustom bool
		want    int // -1, 0, +1
	}{
		// Both have Group — compare by integer
		{"both have group, lower wins", "[Audio]", intp(1), false, "[German]", intp(11), false, -1},
		{"both have group, higher loses", "[German]", intp(11), false, "[Audio]", intp(1), false, 1},
		{"both have group, same value → alphabetical", "[Banana]", intp(5), false, "[Apple]", intp(5), false, 1},
		{"both have group, same value, alpha A wins", "[Apple]", intp(5), false, "[Banana]", intp(5), false, -1},
		// Group=0 is a real value, not "absent" — lowest possible
		{"group=0 is highest priority, beats group=1", "[Z]", intp(0), false, "[A]", intp(1), false, -1},
		{"group=0 distinguishable from nil", "[Z]", intp(0), false, "[A]", nil, false, -1},
		// Tier 1 vs Tier 2 — explicit group always beats no-group
		{"has-group beats no-group", "[Anime]", intp(81), false, "[Other]", nil, false, -1},
		{"no-group loses to has-group", "[Other]", nil, false, "[Anime]", intp(81), false, 1},
		// Both Tier 2 — alphabetical
		{"both no-group, alphabetical", "[Apple]", nil, false, "[Banana]", nil, false, -1},
		{"both no-group, alphabetical reverse", "[Banana]", nil, false, "[Apple]", nil, false, 1},
		// Custom always last regardless of Group
		{"custom always last vs has-group", "[My Custom]", intp(1), true, "[Other]", nil, false, 1},
		{"custom always last vs no-group", "[My Custom]", nil, true, "[Other]", nil, false, 1},
		{"non-custom beats custom even when custom has Group=0", "[Other]", nil, false, "[My Custom]", intp(0), true, -1},
		// Both custom — alphabetical fallback
		{"both custom alphabetical", "[A]", nil, true, "[B]", nil, true, -1},
		// Equal
		{"identical → 0", "[Same]", intp(5), false, "[Same]", intp(5), false, 0},
	}
	for _, c := range cases {
		got := CompareCFGroups(c.aName, c.aGroup, c.aCustom, c.bName, c.bGroup, c.bCustom)
		switch {
		case got < 0:
			got = -1
		case got > 0:
			got = 1
		}
		if got != c.want {
			t.Errorf("%s: CompareCFGroups(%q,%v,%v, %q,%v,%v) = %d, want %d",
				c.desc, c.aName, c.aGroup, c.aCustom, c.bName, c.bGroup, c.bCustom, got, c.want)
		}
	}
}

// Ensure the SQP detection is case-insensitive — TRaSH normally uses the
// upper-case "[SQP]" prefix but defensive coding for any drift.
func TestCategoryTier_SQPCaseInsensitive(t *testing.T) {
	cases := map[string]int{
		"SQP":                1,
		"sqp-1":              1,
		"SqP-4 (MA Hybrid)":  1,
		"sqp-something":      1,
		"Audio":              0,
		"Custom":             3,
		"Other":              2,
		"":                   2,
		"Streaming Services": 0,
		"Squad":              0, // does not start with SQP — the literal Q matters
	}
	for cat, want := range cases {
		got := CategoryTier(cat)
		if got != want {
			t.Errorf("CategoryTier(%q) = %d, want %d", cat, got, want)
		}
	}
}
