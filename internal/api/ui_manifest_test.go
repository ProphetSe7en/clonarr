package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleGetUIManifest verifies the manifest endpoint returns the expected
// shape and that backend enums (sync behaviors, auth modes, agent types) are
// all populated. The point of this test is to lock in the contract that the
// frontend depends on — if a field is renamed in Go, this test fails before
// the UI silently breaks.
func TestHandleGetUIManifest(t *testing.T) {
	server := &Server{Core: &core.App{}}

	req := httptest.NewRequest(http.MethodGet, "/api/ui/manifest", nil)
	w := httptest.NewRecorder()
	server.handleGetUIManifest(w, req)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var m UIManifest
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Enum lists must all be populated — empty arrays would mean validators
	// are disconnected from the manifest.
	if len(m.AppTypes) < 2 {
		t.Errorf("AppTypes: want >=2, got %d", len(m.AppTypes))
	}
	if len(m.SyncBehaviorAddModes) != 3 {
		t.Errorf("SyncBehaviorAddModes: want 3, got %d", len(m.SyncBehaviorAddModes))
	}
	if len(m.AuthModes) != 3 {
		t.Errorf("AuthModes: want 3, got %d", len(m.AuthModes))
	}

	// Bounds must be non-zero.
	if m.SessionTTLBounds.Max <= m.SessionTTLBounds.Min {
		t.Errorf("SessionTTLBounds invalid: %+v", m.SessionTTLBounds)
	}

	// Categories must include at least the well-known TRaSH groupings.
	if len(m.CFCategories) < 10 {
		t.Errorf("CFCategories: want >=10, got %d", len(m.CFCategories))
	}

	// Agent types: the 5 built-in providers must each appear with a
	// non-empty FieldSpec so the modal renders correctly.
	wantAgents := map[string]bool{"discord": true, "gotify": true, "pushover": true, "ntfy": true, "apprise": true}
	for _, a := range m.NotificationAgents {
		delete(wantAgents, a.Type)
		if a.Label == "" {
			t.Errorf("agent %q: empty Label", a.Type)
		}
		if len(a.FieldSpec.Groups) == 0 {
			t.Errorf("agent %q: empty FieldSpec.Groups", a.Type)
		}
	}
	for missing := range wantAgents {
		t.Errorf("agent %q missing from manifest", missing)
	}
}

// TestUIManifestEnumValuesValidate ensures every default value the
// SyncBehavior resolver picks is actually a valid enum entry — i.e. the
// enum lists are the source of truth and ResolveSyncBehavior reads from
// them rather than holding stale string literals.
func TestUIManifestEnumValuesValidate(t *testing.T) {
	d := core.DefaultSyncBehavior()
	if !core.IsValidEnumValue(core.SyncBehaviorAddModes, d.AddMode) {
		t.Errorf("default AddMode %q not in SyncBehaviorAddModes", d.AddMode)
	}
	if !core.IsValidEnumValue(core.SyncBehaviorRemoveModes, d.RemoveMode) {
		t.Errorf("default RemoveMode %q not in SyncBehaviorRemoveModes", d.RemoveMode)
	}
	if !core.IsValidEnumValue(core.SyncBehaviorResetModes, d.ResetMode) {
		t.Errorf("default ResetMode %q not in SyncBehaviorResetModes", d.ResetMode)
	}
}
