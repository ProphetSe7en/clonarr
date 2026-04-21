package core

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// App holds shared application state.
// CleanupEvent records a stale rule/history removal for frontend notification.
type CleanupEvent struct {
	ProfileName  string `json:"profileName"`
	InstanceName string `json:"instanceName"`
	ArrProfileID int    `json:"arrProfileId"`
	Timestamp    string `json:"timestamp"`
}

// AutoSyncEvent records an auto-sync result for frontend toast notification.
type AutoSyncEvent struct {
	InstanceName   string   `json:"instanceName"`
	ProfileName    string   `json:"profileName"`
	ArrProfileName string   `json:"arrProfileName,omitempty"`
	CFsCreated     int      `json:"cfsCreated"`
	CFsUpdated     int      `json:"cfsUpdated"`
	ScoresUpdated  int      `json:"scoresUpdated"`
	QualityUpdated bool     `json:"qualityUpdated"`
	SettingsCount  int      `json:"settingsCount"`
	Details        []string `json:"details,omitempty"` // e.g. "Repack/Proper: 5 → 6"
	Error          string   `json:"error,omitempty"`
	Timestamp      string   `json:"timestamp"`
}

type App struct {
	Config         *ConfigStore
	Trash          *TrashStore
	Profiles       *ProfileStore
	CustomCFs      *CustomCFStore
	CFGroups       *CFGroupStore
	DebugLog       *DebugLogger
	Version        string
	HTTPClient     *http.Client // shared HTTP client for Arr/Prowlarr API calls
	NotifyClient   *http.Client // shared HTTP client for Discord/Gotify notifications
	SafeClient     *http.Client // shared HTTP client with SSRF blocklist (Pushover, Discord)
	PullUpdateCh   chan string  // send new interval string to reschedule pull
	CleanupEvents  []CleanupEvent
	CleanupMu      sync.Mutex
	AutoSyncEvents []AutoSyncEvent
	AutoSyncMu     sync.Mutex
}

// parsePullInterval parses a pull interval string. Supports Go duration (1h, 30m, 24h).
// Returns 0 to disable. Defaults to 24h if empty.
func ParsePullInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 24 * time.Hour
	}
	if s == "0" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("Invalid PULL_INTERVAL %q, using 24h default: %v", s, err)
		return 24 * time.Hour
	}
	if d < time.Minute {
		log.Printf("PULL_INTERVAL %s too short, minimum 1m", s)
		return time.Minute
	}
	return d
}
