package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Debug log categories
const (
	LogSync     = "SYNC"
	LogCompare  = "COMPARE"
	LogAutoSync = "AUTO-SYNC"
	LogTrash    = "TRASH"
	LogError    = "ERROR"
	LogUI       = "UI"
	LogConfig   = "CONFIG"
)

// debugLogger writes timestamped debug messages to a log file with rotation.
type debugLogger struct {
	mu       sync.Mutex
	enabled  bool
	filePath string
	maxSize  int64
}

func newDebugLogger(configDir string) *debugLogger {
	return &debugLogger{
		filePath: filepath.Join(configDir, "debug.log"),
		maxSize:  1 << 20, // 1 MB
	}
}

// SetEnabled enables or disables debug logging.
func (dl *debugLogger) SetEnabled(on bool) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	dl.enabled = on
}

// Enabled returns whether debug logging is active.
func (dl *debugLogger) Enabled() bool {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.enabled
}

// Log writes a single debug log line if logging is enabled.
func (dl *debugLogger) Log(category, message string) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if !dl.enabled {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, category, message)
	dl.writeAndRotate(line)
}

// Logf writes a formatted debug log line if logging is enabled.
func (dl *debugLogger) Logf(category, format string, args ...any) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if !dl.enabled {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, category, msg)
	dl.writeAndRotate(line)
}

// FilePath returns the path to the current debug log file.
func (dl *debugLogger) FilePath() string {
	return dl.filePath
}

// writeAndRotate appends a line to the log file and rotates if over maxSize.
// Must be called with dl.mu held.
func (dl *debugLogger) writeAndRotate(line string) {
	f, err := os.OpenFile(dl.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // silently fail — debug logging should never break the app
	}
	f.WriteString(line)
	fi, err := f.Stat()
	f.Close()
	if err != nil {
		return
	}
	if fi.Size() > dl.maxSize {
		// Rotate: rename current to .1, start fresh
		os.Rename(dl.filePath, dl.filePath+".1")
	}
}

// overrideSummary formats sync overrides for logging.
func overrideSummary(o *SyncOverrides) string {
	if o == nil {
		return "none"
	}
	parts := []string{}
	if o.Language != nil && *o.Language != "" {
		parts = append(parts, "language="+*o.Language)
	}
	if o.CutoffQuality != nil && *o.CutoffQuality != "" {
		parts = append(parts, "cutoff="+*o.CutoffQuality)
	}
	if o.MinFormatScore != nil {
		parts = append(parts, fmt.Sprintf("minScore=%d", *o.MinFormatScore))
	}
	if o.MinUpgradeFormatScore != nil {
		parts = append(parts, fmt.Sprintf("minUpgrade=%d", *o.MinUpgradeFormatScore))
	}
	if o.CutoffFormatScore != nil {
		parts = append(parts, fmt.Sprintf("cutoffScore=%d", *o.CutoffFormatScore))
	}
	if len(parts) == 0 {
		return "none"
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
