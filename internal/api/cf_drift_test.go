package api

import (
	"testing"

	"clonarr/internal/core"
)

// makeCFDriftFixture builds a minimal Config + AppData pair sufficient
// for cfTrashIDManagedByRules to evaluate. Profile "p1" enables a
// default-on group containing trash id "managed-default", a default-off
// group containing "managed-optional", and FormatItems containing the
// required CF "managed-required". Rule r-A targets instance inst-A
// with that profile; rule r-disabled targets the same instance with
// Enabled=false.
func makeCFDriftFixture() (core.Config, *core.AppData) {
	defTrue := true
	defFalse := false
	profile := &core.TrashQualityProfile{
		TrashID: "p1",
		Name:    "Test Profile",
		FormatItems: map[string]string{
			"Managed Required": "managed-required",
		},
	}
	groupOn := &core.TrashCFGroup{
		Name:    "[Default On Group]",
		TrashID: "grp-on",
		Default: "true",
		CustomFormats: []core.CFGroupEntry{
			{Name: "Managed Default", TrashID: "managed-default", Default: &defTrue},
		},
	}
	groupOn.QualityProfiles.Include = map[string]string{"Test Profile": "p1"}
	groupOff := &core.TrashCFGroup{
		Name:    "[Default Off Group]",
		TrashID: "grp-off",
		Default: "false",
		CustomFormats: []core.CFGroupEntry{
			{Name: "Managed Optional", TrashID: "managed-optional", Default: &defFalse},
		},
	}
	groupOff.QualityProfiles.Include = map[string]string{"Test Profile": "p1"}

	ad := &core.AppData{
		CustomFormats: map[string]*core.TrashCF{
			"managed-default":  {Name: "Managed Default"},
			"managed-optional": {Name: "Managed Optional"},
			"managed-required": {Name: "Managed Required"},
			"orphan":           {Name: "Orphan CF"},
		},
		CFGroups: []*core.TrashCFGroup{groupOn, groupOff},
		Profiles: []*core.TrashQualityProfile{profile},
	}

	cfg := core.Config{
		Instances: []core.Instance{
			{ID: "inst-A", Type: "radarr", URL: "http://r/", APIKey: "k"},
			{ID: "inst-B", Type: "radarr", URL: "http://r2/", APIKey: "k"},
		},
		AutoSync: core.AutoSyncConfig{
			Rules: []core.AutoSyncRule{
				{ID: "r-A", InstanceID: "inst-A", TrashProfileID: "p1", Enabled: true},
				{ID: "r-disabled", InstanceID: "inst-A", TrashProfileID: "p1", Enabled: false},
			},
		},
	}
	return cfg, ad
}

func TestCFTrashIDManagedByRules_DefaultOnGroupAccepted(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	if !cfTrashIDManagedByRules("managed-default", "inst-A", cfg, ad) {
		t.Errorf("default-on group CF should be managed")
	}
}

func TestCFTrashIDManagedByRules_RequiredFormatItemAccepted(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	if !cfTrashIDManagedByRules("managed-required", "inst-A", cfg, ad) {
		t.Errorf("required FormatItem CF should be managed")
	}
}

func TestCFTrashIDManagedByRules_OptedInViaSelectedCFs(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	cfg.AutoSync.Rules[0].SelectedCFs = []string{"managed-optional"}
	if !cfTrashIDManagedByRules("managed-optional", "inst-A", cfg, ad) {
		t.Errorf("CF opted in via SelectedCFs should be managed")
	}
}

func TestCFTrashIDManagedByRules_DefaultOffNotManaged(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	if cfTrashIDManagedByRules("managed-optional", "inst-A", cfg, ad) {
		t.Errorf("default-off CF should NOT be managed without explicit opt-in")
	}
}

func TestCFTrashIDManagedByRules_ExcludedRejected(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	// Single-rule setup so the exclusion path is the only enrollment
	// candidate. With multiple rules on the same instance, a CF
	// excluded by one rule may still be enrolled by another — that
	// case is covered separately.
	cfg.AutoSync.Rules = []core.AutoSyncRule{
		{ID: "r-A", InstanceID: "inst-A", TrashProfileID: "p1", Enabled: true,
			ExcludedCFs: []string{"managed-default"}},
	}
	if cfTrashIDManagedByRules("managed-default", "inst-A", cfg, ad) {
		t.Errorf("excluded CF should NOT be managed even if default-on, when no other rule enrolls it")
	}
}

func TestCFTrashIDManagedByRules_ExcludedInOneRuleEnrolledByAnother(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	// r-A excludes the CF, but r-disabled (also on inst-A) doesn't —
	// and with the post-Enabled-filter change, disabled rules count.
	// "Managed by ANY rule on this instance" is the contract: if any
	// rule needs the CF, it's a candidate for Apply.
	cfg.AutoSync.Rules[0].ExcludedCFs = []string{"managed-default"}
	if !cfTrashIDManagedByRules("managed-default", "inst-A", cfg, ad) {
		t.Errorf("CF excluded by one rule but enrolled by another should still be managed")
	}
}

func TestCFTrashIDManagedByRules_DisabledRuleStillEnrolls(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	// Drop the enabled rule so only the disabled one matches.
	// Enabled=false means "paused auto-sync schedule" — the rule is
	// still configured and its CFs still belong to clonarr's saved
	// spec, so drift detection processes them AND Apply must accept
	// them. Locks in that auto-sync state is irrelevant to "is this
	// CF managed" semantics.
	cfg.AutoSync.Rules = []core.AutoSyncRule{
		{ID: "r-disabled", InstanceID: "inst-A", TrashProfileID: "p1", Enabled: false},
	}
	if !cfTrashIDManagedByRules("managed-default", "inst-A", cfg, ad) {
		t.Errorf("disabled rule should still enroll CFs for Apply — paused auto-sync is not a reason to refuse")
	}
}

func TestCFTrashIDManagedByRules_OrphanedRuleIgnored(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	cfg.AutoSync.Rules = []core.AutoSyncRule{
		{ID: "r-orphan", InstanceID: "inst-A", TrashProfileID: "p1", Enabled: true, OrphanedAt: "2026-01-01T00:00:00Z"},
	}
	if cfTrashIDManagedByRules("managed-default", "inst-A", cfg, ad) {
		t.Errorf("orphaned rule must not enroll CFs for Apply")
	}
}

func TestCFTrashIDManagedByRules_WrongInstanceIgnored(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	if cfTrashIDManagedByRules("managed-default", "inst-B", cfg, ad) {
		t.Errorf("rules on other instance must not enroll CFs on this instance")
	}
}

func TestCFTrashIDManagedByRules_UnknownTrashIDRejected(t *testing.T) {
	cfg, ad := makeCFDriftFixture()
	if cfTrashIDManagedByRules("does-not-exist", "inst-A", cfg, ad) {
		t.Errorf("unknown trash id must be rejected")
	}
}

func TestCFTrashIDManagedByRules_NoAppDataRejectsUnselectedTID(t *testing.T) {
	cfg, _ := makeCFDriftFixture()
	// Without AppData we can't compute TRaSH defaults, so only the
	// explicit SelectedCFs path can succeed. A tid relying on defaults
	// is rejected — defense-in-depth against an AppData-empty load.
	if cfTrashIDManagedByRules("managed-default", "inst-A", cfg, nil) {
		t.Errorf("default-on CF must be rejected when AppData is nil")
	}
	cfg.AutoSync.Rules[0].SelectedCFs = []string{"explicit"}
	if !cfTrashIDManagedByRules("explicit", "inst-A", cfg, nil) {
		t.Errorf("explicit SelectedCFs must be accepted even with nil AppData")
	}
}
