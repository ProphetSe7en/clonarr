package agents

import (
	"strings"
	"testing"
)

func TestGotifyValidate(t *testing.T) {
	tests := []struct {
		name    string
		agent   Agent
		wantErr string
	}{
		{
			name:    "missing credentials",
			agent:   Agent{Name: "Gotify", Type: "gotify"},
			wantErr: "gotify URL and token are required",
		},
		{
			name: "valid",
			agent: Agent{Name: "Gotify", Type: "gotify", Config: Config{
				GotifyURL:   "https://gotify.example.com",
				GotifyToken: "tok123",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAgent(tc.agent)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateAgent() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateAgent() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateAgent() error = %q, want contains %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestGotifyMaskAndPreserve(t *testing.T) {
	cfg := Config{GotifyURL: "https://gotify.example.com", GotifyToken: "tok123"}
	masked := MaskConfigByType("gotify", cfg)
	if masked.GotifyToken != maskedToken {
		t.Fatalf("gotify token not masked")
	}
	restored := PreserveConfigByType("gotify", masked, cfg)
	if restored.GotifyToken != cfg.GotifyToken {
		t.Fatalf("gotify token not preserved")
	}
}

func TestGotifyPriorityForSeverity(t *testing.T) {
	p := gotifyProvider{}
	critical, warning, info := 8, 5, 3
	cfg := Config{
		GotifyPriorityCritical: true,
		GotifyPriorityWarning:  true,
		GotifyPriorityInfo:     true,
		GotifyCriticalValue:    &critical,
		GotifyWarningValue:     &warning,
		GotifyInfoValue:        &info,
	}

	if got, ok := p.priorityForSeverity(cfg, SeverityCritical); !ok || got != 8 {
		t.Fatalf("critical priority = %d, enabled = %t", got, ok)
	}
	if got, ok := p.priorityForSeverity(cfg, SeverityWarning); !ok || got != 5 {
		t.Fatalf("warning priority = %d, enabled = %t", got, ok)
	}
	if got, ok := p.priorityForSeverity(cfg, SeverityInfo); !ok || got != 3 {
		t.Fatalf("info priority = %d, enabled = %t", got, ok)
	}

	cfg.GotifyPriorityInfo = false
	if _, ok := p.priorityForSeverity(cfg, SeverityInfo); ok {
		t.Fatalf("info severity should be disabled")
	}
}

func TestNormalizeGotifyMarkdown(t *testing.T) {
	input := "line\n**header**\n- item"
	out := normalizeGotifyMarkdown(input)
	if !strings.Contains(out, "\n\n**header**") {
		t.Fatalf("expected double newline before header, got %q", out)
	}
	if !strings.Contains(out, "\n\n- item") {
		t.Fatalf("expected double newline before list item, got %q", out)
	}
}
