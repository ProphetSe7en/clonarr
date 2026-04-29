package core

import (
	"strings"
	"testing"
)

// TestMigratePrefix_AddsBangToUnprefixed verifies the migration prepends "!"
// to every custom CF that doesn't already have it, while leaving already-
// prefixed names untouched.
func TestMigratePrefix_AddsBangToUnprefixed(t *testing.T) {
	dir := t.TempDir()
	cs := NewCustomCFStore(dir)

	seed := []CustomCF{
		{ID: GenerateCustomID(), Name: "PCOK", AppType: "radarr", Category: "Custom"},
		{ID: GenerateCustomID(), Name: "!Already", AppType: "radarr", Category: "Custom"},
		{ID: GenerateCustomID(), Name: "Hulu", AppType: "sonarr", Category: "Custom"},
	}
	if _, err := cs.Add(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cs.MigratePrefix()

	for _, appType := range []string{"radarr", "sonarr"} {
		for _, ccf := range cs.List(appType) {
			if !strings.HasPrefix(ccf.Name, "!") {
				t.Errorf("%s/%q missing ! prefix after migration", appType, ccf.Name)
			}
		}
	}
}

// TestMigratePrefix_Idempotent verifies running the migration twice produces
// no change on the second run.
func TestMigratePrefix_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cs := NewCustomCFStore(dir)

	if _, err := cs.Add([]CustomCF{
		{ID: GenerateCustomID(), Name: "Foo", AppType: "radarr", Category: "Custom"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cs.MigratePrefix()
	first := cs.List("radarr")

	cs.MigratePrefix()
	second := cs.List("radarr")

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 CF after each migration, got %d / %d", len(first), len(second))
	}
	if first[0].Name != "!Foo" || second[0].Name != "!Foo" {
		t.Errorf("name should stay !Foo across runs, got %q / %q", first[0].Name, second[0].Name)
	}
}

// TestMigratePrefix_EmptyStore handles the empty-store case as a no-op.
func TestMigratePrefix_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	cs := NewCustomCFStore(dir)

	cs.MigratePrefix() // must not panic

	if got := len(cs.List("radarr")); got != 0 {
		t.Errorf("radarr list should stay empty, got %d", got)
	}
}
