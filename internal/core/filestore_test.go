package core

import (
	"os"
	"strings"
	"testing"
)

// testItem is a minimal FileItem implementation for testing.
type testItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	AppType string `json:"appType"`
	Value   string `json:"value"`
}

func (t *testItem) GetID() string      { return t.ID }
func (t *testItem) GetName() string    { return t.Name }
func (t *testItem) SetName(n string)   { t.Name = n }
func (t *testItem) GetAppType() string { return t.AppType }

func newTestStore(t *testing.T) *FileStore[testItem, *testItem] {
	t.Helper()
	dir := t.TempDir()
	return NewFileStore[testItem, *testItem](dir)
}

func TestFileStore_AddAndList(t *testing.T) {
	fs := newTestStore(t)

	items := []testItem{
		{ID: "1", Name: "Alpha", AppType: "radarr", Value: "a"},
		{ID: "2", Name: "Beta", AppType: "sonarr", Value: "b"},
		{ID: "3", Name: "Gamma", AppType: "radarr", Value: "c"},
	}

	added, _, err := fs.Add(items)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if added != 3 {
		t.Errorf("expected 3 added, got %d", added)
	}

	// List all
	all := fs.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 items, got %d", len(all))
	}

	// List filtered
	radarr := fs.List("radarr")
	if len(radarr) != 2 {
		t.Errorf("expected 2 radarr items, got %d", len(radarr))
	}

	sonarr := fs.List("sonarr")
	if len(sonarr) != 1 {
		t.Errorf("expected 1 sonarr item, got %d", len(sonarr))
	}
}

func TestFileStore_AddSkipsDuplicates(t *testing.T) {
	fs := newTestStore(t)

	items := []testItem{
		{ID: "1", Name: "Alpha", AppType: "radarr"},
	}
	added, _, err := fs.Add(items)
	if err != nil {
		t.Fatalf("first Add failed: %v", err)
	}
	if added != 1 {
		t.Errorf("expected 1, got %d", added)
	}

	// Add same name+appType with different ID — should be skipped
	items2 := []testItem{
		{ID: "2", Name: "Alpha", AppType: "radarr"},
	}
	added, _, err = fs.Add(items2)
	if err != nil {
		t.Fatalf("second Add failed: %v", err)
	}
	if added != 0 {
		t.Errorf("expected 0 (duplicate), got %d", added)
	}
}

func TestFileStore_GetFound(t *testing.T) {
	fs := newTestStore(t)

	_, _, _ = fs.Add([]testItem{
		{ID: "abc", Name: "Test", AppType: "radarr", Value: "hello"},
	})

	item, ok := fs.Get("abc")
	if !ok {
		t.Fatal("expected item to be found")
	}
	if item.Value != "hello" {
		t.Errorf("expected value 'hello', got %q", item.Value)
	}
}

func TestFileStore_GetNotFound(t *testing.T) {
	fs := newTestStore(t)

	_, ok := fs.Get("nonexistent")
	if ok {
		t.Error("expected item not found")
	}
}

func TestFileStore_Delete(t *testing.T) {
	fs := newTestStore(t)

	_, _, _ = fs.Add([]testItem{
		{ID: "1", Name: "Alpha", AppType: "radarr"},
		{ID: "2", Name: "Beta", AppType: "radarr"},
	})

	if err := fs.Delete("1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, ok := fs.Get("1")
	if ok {
		t.Error("expected item to be deleted")
	}

	// Other item still exists
	_, ok = fs.Get("2")
	if !ok {
		t.Error("expected other item to still exist")
	}
}

func TestFileStore_DeleteNotFound(t *testing.T) {
	fs := newTestStore(t)

	err := fs.Delete("nonexistent")
	if err == nil {
		t.Error("expected error for deleting nonexistent item")
	}
}

func TestFileStore_Update(t *testing.T) {
	fs := newTestStore(t)

	_, _, _ = fs.Add([]testItem{
		{ID: "1", Name: "Alpha", AppType: "radarr", Value: "old"},
	})

	err := fs.Update(testItem{ID: "1", Name: "Alpha", AppType: "radarr", Value: "new"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	item, ok := fs.Get("1")
	if !ok {
		t.Fatal("item not found after update")
	}
	if item.Value != "new" {
		t.Errorf("expected value 'new', got %q", item.Value)
	}
}

func TestFileStore_UpdateRename(t *testing.T) {
	fs := newTestStore(t)

	_, _, _ = fs.Add([]testItem{
		{ID: "1", Name: "OldName", AppType: "radarr", Value: "v1"},
	})

	err := fs.Update(testItem{ID: "1", Name: "NewName", AppType: "radarr", Value: "v2"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Old filename should be removed
	entries, _ := os.ReadDir(fs.dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file after rename, got %d", len(entries))
	}

	item, ok := fs.Get("1")
	if !ok {
		t.Fatal("item not found after rename")
	}
	if item.Name != "NewName" {
		t.Errorf("expected name 'NewName', got %q", item.Name)
	}
}

func TestFileStore_UpdateNotFound(t *testing.T) {
	fs := newTestStore(t)

	err := fs.Update(testItem{ID: "nonexistent", Name: "X", AppType: "radarr"})
	if err == nil {
		t.Error("expected error for updating nonexistent item")
	}
}

func TestFileStore_AppTypeSeparation(t *testing.T) {
	fs := newTestStore(t)

	// Add two items with the exact same name but different AppTypes.
	// Both should be saved into the store since their filenames should not collide.
	items := []testItem{
		{ID: "1", Name: "Identical", AppType: "radarr", Value: "a"},
		{ID: "2", Name: "Identical", AppType: "sonarr", Value: "b"},
	}

	added, skipped, err := fs.Add(items)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if added != 2 {
		t.Errorf("expected 2 added, got %d (skipped: %d)", added, skipped)
	}

	// Verify both physically exist as files
	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	// There should be exactly 2 JSON files with app-scoped names
	jsonCount := 0
	files := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			jsonCount++
			files[e.Name()] = true
		}
	}
	if jsonCount != 2 {
		t.Errorf("expected 2 JSON files, got %d", jsonCount)
	}
	if !files["identical-radarr.json"] {
		t.Error("expected identical-radarr.json to exist")
	}
	if !files["identical-sonarr.json"] {
		t.Error("expected identical-sonarr.json to exist")
	}

	// Verify we can retrieve both distinct items
	rItem, rok := fs.Get("1")
	sItem, sok := fs.Get("2")
	if !rok || !sok {
		t.Fatal("could not retrieve both items")
	}
	if rItem.Value != "a" || sItem.Value != "b" {
		t.Errorf("items corrupted or overwritten: radarr=%q, sonarr=%q", rItem.Value, sItem.Value)
	}
}
