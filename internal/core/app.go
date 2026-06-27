package core

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clonarr/internal/core/calculator"
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
	Sandbox        *SandboxStore
	CalcSessions   *calculator.Store // Scoring Generator sessions
	DebugLog       *DebugLogger
	ActivityLog    *ActivityLogger
	Version        string
	DevFeatures    bool         // set from CLONARR_DEV_FEATURES env at startup; gates contributor-only UI (TRaSH schema fields, Recyclarr import/export)
	HTTPClient     *http.Client // shared HTTP client for Arr/Prowlarr API calls
	NotifyClient   *http.Client // shared HTTP client for Discord/Gotify notifications
	SafeClient     *http.Client // shared HTTP client with SSRF blocklist (Pushover, Discord)
	PullUpdateCh   chan string  // wake the scheduler; payload is ignored so config stays authoritative
	ApplyUpdateCh  chan struct{} // wake the delayed-apply scheduler when ProfileSync apply schedule changes
	ShutdownCh     <-chan struct{} // closed on graceful shutdown; long-running waits (retry sleeps, etc.) should select on this to exit early
	NextPullAt     atomic.Value // time.Time; zero means no automatic pull is armed
	NextApplyAt    atomic.Value // time.Time; zero means no delayed-apply schedule is armed
	lastDelayedApplyLog time.Time // throttle for the "changes pending" log; written only by the single-goroutine delayed-apply scheduler
	CleanupEvents  []CleanupEvent
	CleanupMu      sync.Mutex
	AutoSyncEvents []AutoSyncEvent
	AutoSyncMu     sync.Mutex
	// ProfileSyncRunner runs the unified Profile Sync subsystem — detection
	// (TRaSH ls-remote) and apply (Pull-and-sync when Mode=auto). Wired by
	// main.go at startup; nil during tests that don't need it.
	ProfileSyncRunner *ProfileSyncRunner

	// DriftRunner detects Arr-side drift by fetching each rule's current
	// Arr profile and diffing against BuildArrProfile target. Wired by
	// main.go at startup; nil during tests that don't need it.
	DriftRunner *DriftRunner

	// AfterPullCallback runs after a successful TRaSH pull (commit advanced
	// or not), before AutoSyncAfterPull. Lets main.go register server-level
	// helpers like api.Server.AutoSyncQualitySizes — which lives in the api
	// package and is not callable from core — without core depending on api.
	// Fires on BOTH the data-only Pull (RunPullOnly) and the scheduled
	// Pull-and-sync (RunPullAndSync). Nil-safe.
	AfterPullCallback func()

	// AfterSyncCallback runs only on the scheduled Pull-and-sync path
	// (RunPullAndSync), NOT on the data-only Pull (RunPullOnly). Used for
	// instance-level auto-sync that writes to Arr (naming auto-sync), so the
	// manual Pull button stays data-only. Nil-safe.
	AfterSyncCallback func()
}

// IsShuttingDown returns true once ShutdownCh has been closed (graceful
// shutdown in progress). Non-blocking — callers can use this to attribute
// context-canceled errors correctly (shutdown vs request timeout).
func (a *App) IsShuttingDown() bool {
	if a == nil || a.ShutdownCh == nil {
		return false
	}
	select {
	case <-a.ShutdownCh:
		return true
	default:
		return false
	}
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

// SetNextApplyAt records the next delayed-apply time (Profile Sync "Wait
// before applying" mode) so the UI can render a countdown.
func (a *App) SetNextApplyAt(t time.Time) {
	if a == nil {
		return
	}
	a.NextApplyAt.Store(t)
}

// GetNextApplyAt returns the next delayed-apply time, if a schedule is armed.
func (a *App) GetNextApplyAt() time.Time {
	if a == nil {
		return time.Time{}
	}
	v := a.NextApplyAt.Load()
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

