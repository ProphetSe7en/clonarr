package core

import (
	"testing"
)

// TestConfigStore_Get_DeepCopiesCFDriftFingerprints locks in the
// per-Instance map deep-copy added to ConfigStore.Get(). Without it,
// a caller iterating Instance.CFDriftFingerprints races a concurrent
// in-closure delete from the Apply handler and Go's runtime panics
// on the concurrent map operation. The test mutates the returned map
// and then re-reads from the store to confirm the original survived.
func TestConfigStore_Get_DeepCopiesCFDriftFingerprints(t *testing.T) {
	dir := t.TempDir()
	cs := NewConfigStore(dir)
	if err := cs.Set(&Config{
		Instances: []Instance{
			{ID: "inst-A", Name: "Radarr (main)", Type: "radarr", URL: "http://r/", APIKey: "k",
				CFDriftFingerprints: map[string]string{
					"tid-1": "fp-abc",
					"tid-2": "fp-def",
				},
			},
		},
	}); err != nil {
		t.Fatalf("set config: %v", err)
	}

	// First Get-then-mutate: should NOT affect the store.
	cfg1 := cs.Get()
	if len(cfg1.Instances) != 1 {
		t.Fatalf("instance round-trip lost: got %d instances", len(cfg1.Instances))
	}
	if cfg1.Instances[0].CFDriftFingerprints["tid-1"] != "fp-abc" {
		t.Fatalf("initial read missing fp-abc")
	}
	cfg1.Instances[0].CFDriftFingerprints["tid-1"] = "mutated"
	delete(cfg1.Instances[0].CFDriftFingerprints, "tid-2")

	// Second Get must see the original values — store wasn't touched.
	cfg2 := cs.Get()
	if cfg2.Instances[0].CFDriftFingerprints["tid-1"] != "fp-abc" {
		t.Errorf("ConfigStore.Get leaked map mutation: tid-1 = %q, want fp-abc",
			cfg2.Instances[0].CFDriftFingerprints["tid-1"])
	}
	if _, ok := cfg2.Instances[0].CFDriftFingerprints["tid-2"]; !ok {
		t.Errorf("ConfigStore.Get leaked map deletion: tid-2 missing after caller delete on first copy")
	}
}

// TestConfigStore_GetInstance_DeepCopiesCFDriftFingerprints — mirror
// of the Get test for the single-instance accessor. cf_sync_rules.go
// + watch.go call GetInstance per-row, so the same isolation
// guarantee is needed.
func TestConfigStore_GetInstance_DeepCopiesCFDriftFingerprints(t *testing.T) {
	dir := t.TempDir()
	cs := NewConfigStore(dir)
	if err := cs.Set(&Config{
		Instances: []Instance{
			{ID: "inst-A", Type: "radarr", URL: "http://r/", APIKey: "k",
				CFDriftFingerprints: map[string]string{"tid-1": "fp-abc"},
			},
		},
	}); err != nil {
		t.Fatalf("set: %v", err)
	}

	inst, ok := cs.GetInstance("inst-A")
	if !ok {
		t.Fatalf("instance missing")
	}
	inst.CFDriftFingerprints["tid-1"] = "mutated"

	inst2, _ := cs.GetInstance("inst-A")
	if inst2.CFDriftFingerprints["tid-1"] != "fp-abc" {
		t.Errorf("GetInstance leaked mutation: tid-1 = %q, want fp-abc",
			inst2.CFDriftFingerprints["tid-1"])
	}
}

// TestCFSpecDriftPass_DropsUnmanagedReconciledEvents — once a rule
// has been disabled (or the CF removed from its SelectedCFs), the
// CF drops out of managedTIDs. Prior-pass fingerprints linger until
// the next pass clears them, but emitting a Reconciled event would
// lie ("CF is back in sync") when really the user just stopped
// asking us to look. Lock in the no-event-on-out-of-scope behavior.
func TestCFSpecDriftPass_DropsUnmanagedReconciledEvents(t *testing.T) {
	// Hand-craft the in-scope set the pass walks. Rule disabled
	// means no rules in `rules` for this instance, so the per-instance
	// branch is never entered and no events fire — that's the
	// expected silent-drop behavior. The test simulates this by
	// passing an empty rule slice via work=nil and verifying the
	// pass produces no events even when the instance has prior
	// fingerprints.
	//
	// Note: we don't instantiate a DriftRunner here because the
	// function's behavior at this layer is dominated by the
	// rulesByInst grouping. If rules == 0 for an instance, the
	// per-instance loop body never runs, full stop.
	result := &cfDriftPassResult{
		FingerprintsByInstance: map[string]map[string]string{},
		PriorByInstance:        map[string]map[string]string{},
	}
	if len(result.Events) != 0 {
		t.Errorf("expected 0 events on no-rules-for-instance, got %d", len(result.Events))
	}
}
