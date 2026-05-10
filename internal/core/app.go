package core

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	ActivityLog    *ActivityLogger
	Version        string
	DevFeatures    bool         // set from CLONARR_DEV_FEATURES env at startup; gates contributor-only UI (TRaSH schema fields, Recyclarr import/export)
	HTTPClient     *http.Client // shared HTTP client for Arr/Prowlarr API calls
	NotifyClient   *http.Client // shared HTTP client for Discord/Gotify notifications
	SafeClient     *http.Client // shared HTTP client with SSRF blocklist (Pushover, Discord)
	PullUpdateCh   chan string  // wake the scheduler; payload is ignored so config stays authoritative
	SyncUpdateCh   chan struct{} // wake the auto-sync scheduler when SyncSchedule changes
	ShutdownCh     <-chan struct{} // closed on graceful shutdown; long-running waits (retry sleeps, etc.) should select on this to exit early
	NextPullAt     atomic.Value // time.Time; zero means no automatic pull is armed
	NextSyncAt     atomic.Value // time.Time; zero means no auto-sync schedule is armed
	CleanupEvents  []CleanupEvent
	CleanupMu      sync.Mutex
	AutoSyncEvents []AutoSyncEvent
	AutoSyncMu     sync.Mutex
}

// SetNextPullAt records the next automatic TRaSH pull time for /api/trash/status.
func (a *App) SetNextPullAt(t time.Time) {
	if a == nil {
		return
	}
	a.NextPullAt.Store(t)
}

// GetNextPullAt returns the next automatic TRaSH pull time, if one is armed.
func (a *App) GetNextPullAt() time.Time {
	if a == nil {
		return time.Time{}
	}
	v := a.NextPullAt.Load()
	if t, ok := v.(time.Time); ok {
		return t
	}
	return time.Time{}
}

// SetNextSyncAt records the next periodic auto-sync time for /api/trash/status.
func (a *App) SetNextSyncAt(t time.Time) {
	if a == nil {
		return
	}
	a.NextSyncAt.Store(t)
}

// GetNextSyncAt returns the next periodic auto-sync time, if a schedule is armed.
func (a *App) GetNextSyncAt() time.Time {
	if a == nil {
		return time.Time{}
	}
	v := a.NextSyncAt.Load()
	if t, ok := v.(time.Time); ok {
		return t
	}
	return time.Time{}
}

// ParsePullInterval parses fixed-duration pull intervals such as "1h" or "30m".
// "0" and "specific" return 0; the scheduler handles wall-clock schedules separately.
// Empty values keep the historical 24h default.
func ParsePullInterval(s string) time.Duration {
	s = strings.TrimSpace(s)
	if s == "" {
		return 24 * time.Hour
	}
	if s == "0" || s == "specific" {
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

// ParsePullScheduleClock parses the persisted HH:MM schedule clock.
// It is shared by API validation and scheduler math so they accept the same format.
func ParsePullScheduleClock(s string) (int, int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(s[:2])
	if err != nil {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(s[3:])
	if err != nil {
		return 0, 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// nextPullTimeAt computes the next wall-clock fire time in now's location.
// It always returns a time after now; exact equality rolls to the next period.
// Invalid or empty schedules return the zero time.
func nextPullTimeAt(sched PullSchedule, now time.Time) time.Time {
	hour, minute, ok := ParsePullScheduleClock(sched.Time)
	if !ok {
		return time.Time{}
	}

	switch sched.Mode {
	case "daily":
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	case "weekly":
		if sched.DayOfWeek < 0 || sched.DayOfWeek > 6 {
			return time.Time{}
		}
		daysUntil := (sched.DayOfWeek - int(now.Weekday()) + 7) % 7
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()).AddDate(0, 0, daysUntil)
		if !next.After(now) {
			next = next.AddDate(0, 0, 7)
		}
		return next
	case "monthly":
		if sched.DayOfMonth < 1 || sched.DayOfMonth > 28 {
			return time.Time{}
		}
		next := time.Date(now.Year(), now.Month(), sched.DayOfMonth, hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 1, 0)
		}
		return next
	default:
		return time.Time{}
	}
}

// NextPullTime returns the next wall-clock fire time using the process local timezone.
func NextPullTime(sched PullSchedule) time.Time {
	return nextPullTimeAt(sched, time.Now())
}

// nextSyncTimeAt mirrors nextPullTimeAt but reads from a SyncSchedule. The
// scheduling logic is identical (daily/weekly/monthly + HH:MM in process
// local TZ); kept separate so the PullSchedule code Cole shipped stays
// untouched and the two schedules can evolve independently if needed.
func nextSyncTimeAt(sched SyncSchedule, now time.Time) time.Time {
	if !sched.Enabled {
		return time.Time{}
	}
	hour, minute, ok := ParsePullScheduleClock(sched.Time)
	if !ok {
		return time.Time{}
	}
	switch sched.Mode {
	case "daily":
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	case "weekly":
		if sched.DayOfWeek < 0 || sched.DayOfWeek > 6 {
			return time.Time{}
		}
		daysUntil := (sched.DayOfWeek - int(now.Weekday()) + 7) % 7
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()).AddDate(0, 0, daysUntil)
		if !next.After(now) {
			next = next.AddDate(0, 0, 7)
		}
		return next
	case "monthly":
		if sched.DayOfMonth < 1 || sched.DayOfMonth > 28 {
			return time.Time{}
		}
		next := time.Date(now.Year(), now.Month(), sched.DayOfMonth, hour, minute, 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 1, 0)
		}
		return next
	default:
		return time.Time{}
	}
}

// NextSyncTime returns the next wall-clock fire time for the auto-sync
// schedule using the process local timezone. Returns zero time when the
// schedule is disabled or invalid.
func NextSyncTime(sched SyncSchedule) time.Time {
	return nextSyncTimeAt(sched, time.Now())
}
