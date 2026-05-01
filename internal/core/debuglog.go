package core

import (
	"crypto/rand"
	"encoding/hex"
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

// Operation source values. The vocabulary distinguishes user-initiated
// actions (manual-*) from background-triggered ones (auto-*) so a bug
// report's "I clicked Sync" can be filtered out of overnight noise and
// vice versa.
const (
	SourceManualTrashRule     = "manual-trash-rule"
	SourceManualBuilder       = "manual-builder"
	SourceManualRollback      = "manual-rollback"
	SourceManualCreate        = "manual-create"
	SourceManualEdit          = "manual-edit"
	SourceManualDelete        = "manual-delete"
	SourceManualImportInst    = "manual-import-instance"
	SourceManualImportJSON    = "manual-import-json"
	SourceManualPause         = "manual-pause"
	SourceManualResume        = "manual-resume"
	SourceManualPull          = "manual-pull"
	SourceManualInstanceAdd   = "manual-instance-add"
	SourceManualInstanceEdit  = "manual-instance-edit"
	SourceManualNotifEdit     = "manual-notification-edit"
	SourceAutoSync            = "auto-sync"
	SourceAutoPullStartup     = "auto-pull-startup"
	SourceAutoPullInterval    = "auto-pull-interval"
)

// Operation types. Each maps to a class of user-visible action.
const (
	OpSync    = "SYNC"
	OpAutoSync = "AUTOSYNC"
	OpBuilder = "BUILDER"
	OpCF      = "CF"
	OpRule    = "RULE"
	OpTrash   = "TRASH"
	OpConfig  = "CONFIG"
)

// DebugLogger writes timestamped debug messages to a log file with rotation.
type DebugLogger struct {
	mu       sync.Mutex
	enabled  bool
	filePath string
	maxSize  int64
}

func NewDebugLogger(configDir string) *DebugLogger {
	return &DebugLogger{
		filePath: filepath.Join(configDir, "debug.log"),
		maxSize:  1 << 20, // 1 MB
	}
}

// SetEnabled enables or disables debug logging.
func (l *DebugLogger) SetEnabled(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = on
}

// Enabled returns whether debug logging is active.
func (l *DebugLogger) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Log writes a single debug log line if logging is enabled.
func (l *DebugLogger) Log(category, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, category, message)
	l.writeAndRotate(line)
}

// Logf writes a formatted debug log line if logging is enabled.
func (l *DebugLogger) Logf(category, format string, args ...any) {
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

// FilePath returns the path to the current debug log file.
func (l *DebugLogger) FilePath() string {
	return l.filePath
}

// BeginOp opens a new Operation scoped to this logger. Callers should
// defer EndOp to ensure the closing marker is written even on early
// returns. opType is one of the Op* constants; source is one of the
// Source* constants. context is a free-form one-liner that summarises
// what's being operated on (profile name → instance, CF name, etc.) —
// it's logged on the begin marker.
//
// The returned Operation is safe to pass to nil-checking call sites:
// op.Logf and op.End on a nil receiver are no-ops, so code that wants
// to log only when an operation is active can use the same call shape
// regardless of whether one was started.
func (l *DebugLogger) BeginOp(opType, source, context string) *Operation {
	op := &Operation{
		ID:      newOpID(),
		Type:    opType,
		Source:  source,
		started: time.Now(),
		log:     l,
	}
	if l != nil && l.Enabled() {
		ctxPart := ""
		if context != "" {
			ctxPart = " " + context
		}
		l.writeLine(fmt.Sprintf(">>> %s begin id=%s source=%s%s", opType, op.ID, source, ctxPart))
	}
	return op
}

// Operation represents a single user-visible action. Lines emitted via
// (*Operation).Logf are tagged with the operation's ID so the full trace
// can be extracted with a single grep. Use BeginOp to create one and
// defer (*Operation).End() to close it.
type Operation struct {
	ID      string
	Type    string
	Source  string

	mu      sync.Mutex
	started time.Time
	parent  *Operation
	ended   bool
	log     *DebugLogger
}

// Logf writes a log line scoped to this operation. The line is auto-
// tagged with the operation ID so a single grep returns the full trace.
// Safe to call on a nil receiver — that's a no-op so callers can
// instrument code without forcing every entry point to construct an op.
func (op *Operation) Logf(format string, args ...any) {
	if op == nil || op.log == nil || !op.log.Enabled() {
		return
	}
	msg := fmt.Sprintf(format, args...)
	op.log.writeLine(fmt.Sprintf("[%s] %s", op.ID, msg))
}

// End writes the closing marker for the operation with the elapsed time
// and a result summary. Calling End twice or on a nil receiver is a
// no-op so callers can defer it unconditionally. Result is a free-form
// summary like "ok | 2 settings changed" or "error: profile not found".
func (op *Operation) End(result string) {
	if op == nil {
		return
	}
	op.mu.Lock()
	if op.ended {
		op.mu.Unlock()
		return
	}
	op.ended = true
	elapsed := time.Since(op.started)
	op.mu.Unlock()

	if op.log == nil || !op.log.Enabled() {
		return
	}
	op.log.writeLine(fmt.Sprintf("<<< %s end id=%s result=%s | %s", op.Type, op.ID, result, formatDuration(elapsed)))
}

// Sub creates a child operation linked to this one. Used for auto-sync
// ticks where each rule that produces work gets its own nested SYNC op
// while the parent tick keeps its summary line. The child's ID encodes
// the parent for grep-friendly extraction (op_xyz789-r02).
//
// A nil receiver returns a nil sub — that's still a usable Operation
// since (*Operation).Logf and End handle nil. This means call sites
// can blindly do `parent.Sub(...)` without nil-checking parent first.
func (op *Operation) Sub(opType, source, context string) *Operation {
	if op == nil {
		return nil
	}
	if op.log == nil {
		// Parent has no logger — sub inherits that, becomes a no-op.
		return &Operation{ID: newOpID(), Type: opType, Source: source, started: time.Now()}
	}
	subID := op.ID + "-r" + newSubSuffix()
	sub := &Operation{
		ID:      subID,
		Type:    opType,
		Source:  source,
		started: time.Now(),
		parent:  op,
		log:     op.log,
	}
	if op.log.Enabled() {
		ctxPart := ""
		if context != "" {
			ctxPart = " " + context
		}
		op.log.writeLine(fmt.Sprintf(">>> %s begin id=%s parent=%s source=%s%s", opType, sub.ID, op.ID, source, ctxPart))
	}
	return sub
}

// EndWithRecover is the same as End plus a deferred-panic safety net.
// Use it as `defer op.EndWithRecover(&endResult)` — if the calling
// function panics, the recover here logs the panic message in the
// op's end marker and re-panics so normal stack-unwinding continues.
// Without this, a panic would let End() run with whatever endResult
// was at the time defer was registered (typically "error: unknown"),
// which misrepresents what happened.
func (op *Operation) EndWithRecover(endResult *string) {
	if r := recover(); r != nil {
		if endResult != nil {
			*endResult = fmt.Sprintf("panic: %v", r)
		}
		op.End(deref(endResult, "panic: unknown"))
		panic(r)
	}
	op.End(deref(endResult, "error: unknown"))
}

// deref returns *s when s is non-nil, else fallback. Used by
// EndWithRecover so callers can pass a *string they update over the
// life of the function and we read its final value at defer time.
func deref(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}

// writeLine is the shared writer used by Logf and Operation. Holds the
// mutex, prepends timestamp, appends newline, and rotates if needed.
// Returns silently on disabled logger or write failure — debug logging
// must never break the app.
func (l *DebugLogger) writeLine(body string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	line := fmt.Sprintf("[%s] %s\n", ts, body)
	l.writeAndRotate(line)
}

// newOpID generates an 8-hex-char operation identifier. Short enough
// to read in logs; collision chance over a single log file is
// vanishingly small (~4B IDs before birthday-paradox 50% match).
func newOpID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return "op_" + hex.EncodeToString(b[:])
}

// newSubSuffix returns a 2-hex-char suffix for sub-operation IDs.
// Sub-IDs are scoped to a parent op's lifetime so the small space is
// fine — collisions within one parent are <1% even with hundreds of
// rules.
func newSubSuffix() string {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%02x", time.Now().UnixNano()&0xFF)
	}
	return hex.EncodeToString(b[:])
}

// formatDuration produces a human-friendly elapsed-time string for the
// end-of-operation summary line. Sub-second times use ms; longer times
// use seconds with one decimal.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// writeAndRotate appends a line to the log file and rotates if over maxSize.
// Must be called with dl.mu held.
func (l *DebugLogger) writeAndRotate(line string) {
	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return // silently fail — debug logging should never break the app
	}
	f.WriteString(line)
	fi, err := f.Stat()
	f.Close()
	if err != nil {
		return
	}
	if fi.Size() > l.maxSize {
		// Rotate: rename current to .1, start fresh
		os.Rename(l.filePath, l.filePath+".1")
	}
}

// overrideSummary formats sync overrides for logging.
func OverrideSummary(o *SyncOverrides) string {
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
