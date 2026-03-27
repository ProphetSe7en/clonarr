package main

import "testing"

func TestResolveSyncBehavior_Nil(t *testing.T) {
	got := ResolveSyncBehavior(nil)
	want := DefaultSyncBehavior()
	if got != want {
		t.Fatalf("nil → %+v, want %+v", got, want)
	}
}

func TestResolveSyncBehavior_FullyPopulated(t *testing.T) {
	b := &SyncBehavior{AddMode: "do_not_add", RemoveMode: "allow_custom", ResetMode: "do_not_adjust"}
	got := ResolveSyncBehavior(b)
	if got != *b {
		t.Fatalf("got %+v, want %+v", got, *b)
	}
}

func TestResolveSyncBehavior_PartialFillsDefaults(t *testing.T) {
	b := &SyncBehavior{AddMode: "add_new"}
	got := ResolveSyncBehavior(b)
	if got.AddMode != "add_new" {
		t.Fatalf("AddMode = %q, want add_new", got.AddMode)
	}
	if got.RemoveMode != "remove_custom" {
		t.Fatalf("RemoveMode = %q, want remove_custom (default)", got.RemoveMode)
	}
	if got.ResetMode != "reset_to_zero" {
		t.Fatalf("ResetMode = %q, want reset_to_zero (default)", got.ResetMode)
	}
}

func TestResolveSyncBehavior_EmptyStruct(t *testing.T) {
	b := &SyncBehavior{}
	got := ResolveSyncBehavior(b)
	want := DefaultSyncBehavior()
	if got != want {
		t.Fatalf("empty → %+v, want %+v", got, want)
	}
}
