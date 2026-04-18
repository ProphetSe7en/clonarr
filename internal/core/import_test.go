package core

import (
	"fmt"
	"os"
	"testing"
)

func TestParseRecyclarrClassic(t *testing.T) {
	data, err := os.ReadFile("test-import-classic.yml")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ParseRecyclarrYAML(data, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	fmt.Printf("Classic: %q (%s) — %d CFs, %d qualities\n", p.Name, p.AppType, len(p.FormatItems), len(p.Qualities))
	fmt.Printf("  Upgrade: %v, Cutoff: %s, MinScore: %d\n", p.UpgradeAllowed, p.Cutoff, p.MinFormatScore)
	for tid, score := range p.FormatItems {
		comment := p.FormatComments[tid]
		if comment != "" {
			fmt.Printf("  %s (%s): %d\n", tid[:12], comment, score)
		} else {
			fmt.Printf("  %s: %d\n", tid[:12], score)
		}
	}
}

func TestParseRecyclarrV8(t *testing.T) {
	data, err := os.ReadFile("test-import-v8.yml")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := ParseRecyclarrYAML(data, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}
	p := profiles[0]
	fmt.Printf("V8: %q (%s) — %d CFs, %d qualities\n", p.Name, p.AppType, len(p.FormatItems), len(p.Qualities))
	fmt.Printf("  Upgrade: %v, Cutoff: %s, MinScore: %d, CutoffScore: %d\n", p.UpgradeAllowed, p.Cutoff, p.MinFormatScore, p.CutoffScore)
	for _, q := range p.Qualities {
		fmt.Printf("  Quality: %s (allowed=%v, items=%v)\n", q.Name, q.Allowed, q.Items)
	}
}
