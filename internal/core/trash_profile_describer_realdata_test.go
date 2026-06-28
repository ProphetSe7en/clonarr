package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDescribeProfile_RealRadarrData verifies the describer against actual
// TRaSH data on disk. Skipped automatically if the data path isn't present
// (CI / fresh checkouts). When run on a machine with a clonarr clone, it
// renders the 4 standard Radarr profiles and asserts the key facts we
// designed the describer to extract.
//
// Set CLONARR_TRASH_DIR to point at an existing /data/trash-guides clone
// (the live container path works on dev machines).
func TestDescribeProfile_RealRadarrData(t *testing.T) {
	dataDir := os.Getenv("CLONARR_TRASH_DIR")
	if dataDir == "" {
		dataDir = "./testdata/trash-guides"
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Skipf("skipping: TRaSH data dir not available (%s)", dataDir)
	}

	groupDir := filepath.Join(dataDir, "docs", "json", "radarr", "cf-groups")
	groupFiles, err := os.ReadDir(groupDir)
	if err != nil {
		t.Fatalf("read cf-groups dir: %v", err)
	}
	var groups []*TrashCFGroup
	for _, g := range groupFiles {
		if g.IsDir() || !strings.HasSuffix(g.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(groupDir, g.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", g.Name(), err)
		}
		var grp TrashCFGroup
		if err := json.Unmarshal(data, &grp); err != nil {
			t.Fatalf("parse %s: %v", g.Name(), err)
		}
		groups = append(groups, &grp)
	}
	if len(groups) == 0 {
		t.Fatal("no cf-groups loaded")
	}

	profileDir := filepath.Join(dataDir, "docs", "json", "radarr", "quality-profiles")
	type expect struct {
		filename       string
		name           string
		wantAudio      bool
		wantHDR        bool
		wantCodec      string
		wantResContain string
		wantSizeNonEmpty bool
		wantTaglineNonEmpty bool
	}
	cases := []expect{
		{
			filename: "hd-bluray-web.json", name: "HD Bluray + WEB",
			wantAudio: false, wantHDR: false, wantCodec: "x264",
			wantResContain: "1080p", wantSizeNonEmpty: true, wantTaglineNonEmpty: true,
		},
		{
			filename: "uhd-bluray-web.json", name: "UHD Bluray + WEB",
			wantAudio: true, wantHDR: true, wantCodec: "x265",
			wantResContain: "2160p", wantSizeNonEmpty: true, wantTaglineNonEmpty: true,
		},
		{
			filename: "remux-web-1080p.json", name: "Remux + WEB 1080p",
			wantAudio: true, wantHDR: false, wantCodec: "x264",
			wantResContain: "1080p", wantSizeNonEmpty: true, wantTaglineNonEmpty: true,
		},
		{
			filename: "remux-web-2160p.json", name: "Remux + WEB 2160p",
			wantAudio: true, wantHDR: true, wantCodec: "x265",
			wantResContain: "2160p", wantSizeNonEmpty: true, wantTaglineNonEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(profileDir, tc.filename))
			if err != nil {
				t.Fatalf("read %s: %v", tc.filename, err)
			}
			var p TrashQualityProfile
			if err := json.Unmarshal(data, &p); err != nil {
				t.Fatalf("parse %s: %v", tc.filename, err)
			}
			desc := describeProfile("radarr", &p, groups, nil)

			// Marshal and print for visual verification
			pretty, _ := json.MarshalIndent(desc, "", "  ")
			t.Logf("=== %s ===\n%s", tc.name, pretty)

			if desc.Axes.Audio.Scored != tc.wantAudio {
				t.Errorf("Audio.Scored = %v, want %v", desc.Axes.Audio.Scored, tc.wantAudio)
			}
			if desc.Axes.HDR.Scored != tc.wantHDR {
				t.Errorf("HDR.Scored = %v, want %v", desc.Axes.HDR.Scored, tc.wantHDR)
			}
			if desc.Axes.Codec != tc.wantCodec {
				t.Errorf("Codec = %q, want %q", desc.Axes.Codec, tc.wantCodec)
			}
			if !strings.Contains(desc.Axes.Resolution, tc.wantResContain) {
				t.Errorf("Resolution = %q, want substring %q", desc.Axes.Resolution, tc.wantResContain)
			}
			if tc.wantSizeNonEmpty && desc.Axes.AvgSize == "" {
				t.Errorf("AvgSize empty (markdown parse failed?)")
			}
			if tc.wantTaglineNonEmpty && desc.Tagline == "" {
				t.Errorf("Tagline empty (markdown parse failed?)")
			}
			// HDR.OptIns sanity for UHD profiles
			if tc.wantHDR {
				if len(desc.Axes.HDR.OptIns) == 0 {
					t.Errorf("expected HDR.OptIns to be populated for UHD profile")
				}
			}
		})
	}
}
