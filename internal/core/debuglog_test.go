package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readLog reads the entire contents of the debug log file as a string.
// Used by all tests in this file to assert on emitted log lines.
func readLog(t *testing.T, l *DebugLogger) string {
	t.Helper()
	data, err := os.ReadFile(l.FilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

// makeLogger returns a DebugLogger writing to a fresh temp dir, with
// logging enabled. Removes need to repeat enable/disable boilerplate.
func makeLogger(t *testing.T) *DebugLogger {
	t.Helper()
	tmp := t.TempDir()
	l := NewDebugLogger(tmp)
	l.SetEnabled(true)
	return l
}

func TestOperation_BeginEnd(t *testing.T) {
	l := makeLogger(t)
	op := l.BeginOp(OpSync, SourceManualTrashRule, `context: profile="X" → "Y"`)
	op.End("ok | 0 changes")

	contents := readLog(t, l)
	if !strings.Contains(contents, ">>> SYNC begin") {
		t.Errorf("expected begin marker in log, got:\n%s", contents)
	}
	if !strings.Contains(contents, "source=manual-trash-rule") {
		t.Errorf("expected source tag in begin marker, got:\n%s", contents)
	}
	if !strings.Contains(contents, "<<< SYNC end") {
		t.Errorf("expected end marker in log, got:\n%s", contents)
	}
	if !strings.Contains(contents, "result=ok | 0 changes") {
		t.Errorf("expected result string in end marker, got:\n%s", contents)
	}
	// Both markers must reference the same operation ID.
	beginID := extractOpID(contents, ">>> SYNC begin id=")
	endID := extractOpID(contents, "<<< SYNC end id=")
	if beginID == "" || beginID != endID {
		t.Errorf("begin/end IDs mismatch: begin=%q end=%q", beginID, endID)
	}
}

func TestOperation_Logf(t *testing.T) {
	l := makeLogger(t)
	op := l.BeginOp(OpSync, SourceManualTrashRule, "")
	op.Logf("plan diff: %d CFs to create", 3)
	op.Logf("HTTP: PUT /api/v3/qualityprofile/%d → 200 OK", 48)
	op.End("ok")

	contents := readLog(t, l)
	if !strings.Contains(contents, "plan diff: 3 CFs to create") {
		t.Errorf("expected formatted message in log, got:\n%s", contents)
	}
	if !strings.Contains(contents, "HTTP: PUT /api/v3/qualityprofile/48 → 200 OK") {
		t.Errorf("expected HTTP line in log, got:\n%s", contents)
	}
	// Every log line within an op must be tagged with its ID.
	id := extractOpID(contents, ">>> SYNC begin id=")
	if id == "" {
		t.Fatal("could not extract op ID")
	}
	for _, line := range strings.Split(contents, "\n") {
		if line == "" || strings.HasPrefix(strings.SplitN(line, " ", 3)[2], ">>>") || strings.HasPrefix(strings.SplitN(line, " ", 3)[2], "<<<") {
			continue
		}
		if !strings.Contains(line, "["+id+"]") {
			t.Errorf("non-marker line missing op-id tag: %q", line)
		}
	}
}

func TestOperation_NilSafe(t *testing.T) {
	// A nil *Operation is a valid no-op receiver — callers that didn't
	// open an operation can still use the same call shape without
	// crashing.
	var op *Operation
	op.Logf("should be no-op: %s", "fine")
	op.End("should be no-op")
	// No assertion needed; the test passes if no panic occurred.
}

func TestOperation_NestedOps(t *testing.T) {
	l := makeLogger(t)
	parent := l.BeginOp(OpAutoSync, SourceAutoPullInterval, "trigger=interval")
	sub := parent.Sub(OpSync, SourceAutoSync, `rule rule_aaa: "X" → "Y"`)
	sub.Logf("desired: Language=Original")
	sub.End("ok | 1 setting changed")
	parent.End("ok | 1 changed, 49 no-op")

	contents := readLog(t, l)
	parentID := extractOpID(contents, ">>> AUTOSYNC begin id=")
	subID := extractOpID(contents, ">>> SYNC begin id=")
	if parentID == "" || subID == "" {
		t.Fatalf("could not extract IDs:\n%s", contents)
	}
	if !strings.HasPrefix(subID, parentID+"-r") {
		t.Errorf("expected sub-id prefixed with parent id %q, got %q", parentID+"-r", subID)
	}
	if !strings.Contains(contents, "parent="+parentID) {
		t.Errorf("expected sub begin to reference parent, got:\n%s", contents)
	}
}

func TestOperation_DisabledLoggerWritesNothing(t *testing.T) {
	tmp := t.TempDir()
	l := NewDebugLogger(tmp)
	// Logging not enabled — operations should be no-ops.
	op := l.BeginOp(OpSync, SourceManualTrashRule, "should not log")
	op.Logf("nor this")
	op.End("nor this")

	logPath := filepath.Join(tmp, "debug.log")
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("expected no log file when logging disabled, but %s exists", logPath)
	}
}

func TestOperation_DoubleEndIsNoOp(t *testing.T) {
	l := makeLogger(t)
	op := l.BeginOp(OpSync, SourceManualTrashRule, "")
	op.End("ok")
	op.End("should be ignored")

	contents := readLog(t, l)
	endCount := strings.Count(contents, "<<< SYNC end")
	if endCount != 1 {
		t.Errorf("expected exactly 1 end marker after double End() call, got %d:\n%s", endCount, contents)
	}
}

// extractOpID returns the op_xxxx token following the given prefix in
// the log contents, or "" if the prefix isn't found. Used to assert
// begin/end IDs match without hardcoding generated values.
func extractOpID(contents, prefix string) string {
	idx := strings.Index(contents, prefix)
	if idx < 0 {
		return ""
	}
	rest := contents[idx+len(prefix):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		return rest
	}
	return rest[:end]
}
