package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ActivityLogger writes timestamped UI/activity events to a separate
// file from debug.log. Splits out user navigation, login events, and
// other context-only signals so debug.log stays focused on operations
// that explain bugs.
//
// Mirrors DebugLogger's lock + silent-fail semantics so disk problems
// can't cascade into request handling. Reuses the same enable bit: if
// DebugLogging is on in config, both files are written.
type ActivityLogger struct {
	mu       sync.Mutex
	enabled  bool
	filePath string
	maxSize  int64
}

// NewActivityLogger opens (or prepares to open) /config/activity.log.
// File is created lazily on first write.
func NewActivityLogger(configDir string) *ActivityLogger {
	return &ActivityLogger{
		filePath: filepath.Join(configDir, "activity.log"),
		maxSize:  1 << 20, // 1 MB
	}
}

// SetEnabled flips logging on or off.
func (l *ActivityLogger) SetEnabled(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = on
}

// Enabled returns the current enable state.
func (l *ActivityLogger) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// FilePath returns the path to the activity log file.
func (l *ActivityLogger) FilePath() string {
	return l.filePath
}

// Logf writes a formatted line. Category is included in the prefix so
// downstream consumers can filter the same way they do today's
// debug.log. Safe under concurrent calls; failures swallowed.
func (l *ActivityLogger) Logf(category, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, category, msg)
	l.writeAndRotate(line)
}

// writeAndRotate appends and rotates at maxSize. Same pattern as
// DebugLogger so behaviour is predictable across both files.
func (l *ActivityLogger) writeAndRotate(line string) {
	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	f.WriteString(line)
	fi, err := f.Stat()
	f.Close()
	if err != nil {
		return
	}
	if fi.Size() > l.maxSize {
		os.Rename(l.filePath, l.filePath+".1")
	}
}
