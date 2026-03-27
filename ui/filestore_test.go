package main

import (
	"os"
	"path/filepath"
	"testing"
)

// testItem is a minimal FileStoreItem for testing.
type testItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	AppType string `json:"appType"`
}

func (t testItem) GetID() string      { return t.ID }
func (t testItem) GetName() string    { return t.Name }
func (t testItem) GetAppType() string { return t.AppType }

func tempStore(t *testing.T) *FileStore[testItem] {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "store")
	fs := NewFileStore[testItem](dir)
	if err := fs.EnsureDir(); err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestFileStore_PutAndGet(t *testing.T) {
	fs := tempStore(t)

	item := testItem{ID: "1", Name: "Alpha", AppType: "radarr"}
	if err := fs.Put(item); err != nil {
		t.Fatal(err)
	}

	got, ok := fs.Get("1")
	if !ok {
		t.Fatal("item not found after Put")
	}
	if got.Name != "Alpha" || got.AppType != "radarr" {
		t.Fatalf("got %+v, want Alpha/radarr", got)
	}
}

func TestFileStore_GetNotFound(t *testing.T) {
	fs := tempStore(t)
	_, ok := fs.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestFileStore_List(t *testing.T) {
	fs := tempStore(t)

	fs.Put(testItem{ID: "1", Name: "A", AppType: "radarr"})
	fs.Put(testItem{ID: "2", Name: "B", AppType: "sonarr"})
	fs.Put(testItem{ID: "3", Name: "C", AppType: "radarr"})

	all := fs.List("")
	if len(all) != 3 {
		t.Fatalf("List(\"\") = %d items, want 3", len(all))
	}

	radarr := fs.List("radarr")
	if len(radarr) != 2 {
		t.Fatalf("List(radarr) = %d items, want 2", len(radarr))
	}

	sonarr := fs.List("sonarr")
	if len(sonarr) != 1 {
		t.Fatalf("List(sonarr) = %d items, want 1", len(sonarr))
	}
}

func TestFileStore_Delete(t *testing.T) {
	fs := tempStore(t)
	fs.Put(testItem{ID: "1", Name: "A", AppType: "radarr"})

	if err := fs.Delete("1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := fs.Get("1"); ok {
		t.Fatal("item still exists after Delete")
	}
}

func TestFileStore_DeleteNotFound(t *testing.T) {
	fs := tempStore(t)
	if err := fs.Delete("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent item")
	}
}

func TestFileStore_Update(t *testing.T) {
	fs := tempStore(t)
	fs.Put(testItem{ID: "1", Name: "Old", AppType: "radarr"})

	err := fs.Update(testItem{ID: "1", Name: "New", AppType: "radarr"})
	if err != nil {
		t.Fatal(err)
	}

	got, ok := fs.Get("1")
	if !ok {
		t.Fatal("item not found after Update")
	}
	if got.Name != "New" {
		t.Fatalf("name = %q, want New", got.Name)
	}

	// Old file should be cleaned up
	entries, _ := os.ReadDir(fs.dir)
	jsonFiles := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			jsonFiles++
		}
	}
	if jsonFiles != 1 {
		t.Fatalf("expected 1 json file after rename, got %d", jsonFiles)
	}
}

func TestFileStore_UpdateNotFound(t *testing.T) {
	fs := tempStore(t)
	err := fs.Update(testItem{ID: "missing", Name: "X", AppType: "radarr"})
	if err == nil {
		t.Fatal("expected error updating nonexistent item")
	}
}

func TestFileStore_NameCollision(t *testing.T) {
	fs := tempStore(t)

	// Two items with the same name but different IDs
	fs.Put(testItem{ID: "1", Name: "Same Name", AppType: "radarr"})
	fs.Put(testItem{ID: "2", Name: "Same Name", AppType: "radarr"})

	// Both should exist
	_, ok1 := fs.Get("1")
	_, ok2 := fs.Get("2")
	if !ok1 || !ok2 {
		t.Fatalf("one or both items missing: ok1=%v, ok2=%v", ok1, ok2)
	}

	all := fs.List("")
	if len(all) != 2 {
		t.Fatalf("expected 2 items, got %d", len(all))
	}
}

func TestFileStore_AddNewDedup(t *testing.T) {
	fs := tempStore(t)

	items := []testItem{
		{ID: "1", Name: "A", AppType: "radarr"},
		{ID: "2", Name: "B", AppType: "radarr"},
	}
	added, err := fs.AddNew(items, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Fatalf("first AddNew = %d, want 2", added)
	}

	// Add again — same Name+AppType should be deduped
	dupes := []testItem{
		{ID: "3", Name: "A", AppType: "radarr"}, // dupe
		{ID: "4", Name: "C", AppType: "radarr"}, // new
	}
	added, err = fs.AddNew(dupes, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Fatalf("second AddNew = %d, want 1 (C only)", added)
	}

	all := fs.List("")
	if len(all) != 3 {
		t.Fatalf("total items = %d, want 3", len(all))
	}
}

func TestFileStore_AddNewWithAssignID(t *testing.T) {
	fs := tempStore(t)

	items := []testItem{
		{Name: "X", AppType: "radarr"},
	}
	counter := 0
	added, err := fs.AddNew(items, func(item *testItem) {
		counter++
		item.ID = "generated"
	})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || counter != 1 {
		t.Fatalf("added=%d counter=%d, want 1/1", added, counter)
	}

	got, ok := fs.Get("generated")
	if !ok {
		t.Fatal("item with assigned ID not found")
	}
	if got.Name != "X" {
		t.Fatalf("name = %q, want X", got.Name)
	}
}

func TestFileStore_EmptyDir(t *testing.T) {
	fs := NewFileStore[testItem](filepath.Join(t.TempDir(), "nonexistent"))
	// List/Get on nonexistent dir should not panic
	if items := fs.List(""); items != nil {
		t.Fatalf("expected nil, got %v", items)
	}
	if _, ok := fs.Get("x"); ok {
		t.Fatal("expected not found")
	}
}
