package api

import (
	"clonarr/internal/arr"
	"testing"
)

// TestNamingFormatUsesCustomFormats covers the helper used by
// scanUnusedByClonarr to decide whether the scan result should expose
// the "rename-flag is functional" info box on the frontend.
func TestNamingFormatUsesCustomFormats(t *testing.T) {
	cases := []struct {
		name     string
		instType string
		naming   arr.ArrNamingConfig
		want     bool
	}{
		{
			name:     "radarr default-trash format with token",
			instType: "radarr",
			naming: arr.ArrNamingConfig{
				"standardMovieFormat": "{Movie CleanTitle} ({Release Year}) {[Custom Formats]}{[Quality Full]}",
			},
			want: true,
		},
		{
			name:     "sonarr default-trash format with token",
			instType: "sonarr",
			naming: arr.ArrNamingConfig{
				"standardEpisodeFormat": "{Series TitleYear} - S{season:00}E{episode:00} {[Custom Formats]}",
			},
			want: true,
		},
		{
			name:     "radarr stripped format without token",
			instType: "radarr",
			naming: arr.ArrNamingConfig{
				"standardMovieFormat": "{Movie CleanTitle} ({Release Year}) {Quality Full}",
			},
			want: false,
		},
		{
			name:     "sonarr without token",
			instType: "sonarr",
			naming: arr.ArrNamingConfig{
				"standardEpisodeFormat": "{Series Title} - {Episode Title}",
			},
			want: false,
		},
		{
			name:     "missing key returns false",
			instType: "radarr",
			naming:   arr.ArrNamingConfig{},
			want:     false,
		},
		{
			name:     "non-string value returns false",
			instType: "radarr",
			naming: arr.ArrNamingConfig{
				"standardMovieFormat": 12345,
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := namingFormatUsesCustomFormats(tc.naming, tc.instType)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
