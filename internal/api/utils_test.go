package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// maskKey / isMasked
// =============================================================================

func TestMaskKey_ShortKey(t *testing.T) {
	// Keys ≤ 8 chars should be fully masked
	cases := []string{"", "a", "abcd", "12345678"}
	for _, key := range cases {
		got := maskKey(key)
		if got != maskSentinel {
			t.Errorf("maskKey(%q) = %q, want %q", key, got, maskSentinel)
		}
	}
}

func TestMaskKey_LongKey(t *testing.T) {
	// Keys > 8 chars: first 4 + asterisks + last 4
	key := "abcdefghijklmnop" // 16 chars
	got := maskKey(key)
	// Should be: abcd********mnop (4 + 8 asterisks + 4 = 16)
	if !strings.HasPrefix(got, "abcd") {
		t.Errorf("expected prefix 'abcd', got %q", got)
	}
	if !strings.HasSuffix(got, "mnop") {
		t.Errorf("expected suffix 'mnop', got %q", got)
	}
	mid := got[4 : len(got)-4]
	for _, c := range mid {
		if c != '*' {
			t.Errorf("expected all asterisks in middle, got %q in %q", string(c), got)
		}
	}
	if len(got) != len(key) {
		t.Errorf("masked key length %d != original length %d", len(got), len(key))
	}
}

func TestMaskKey_ExactlyNineChars(t *testing.T) {
	// Boundary: 9 chars → 4 + 1 asterisk + 4
	key := "123456789"
	got := maskKey(key)
	want := "1234*6789"
	if got != want {
		t.Errorf("maskKey(%q) = %q, want %q", key, got, want)
	}
}

func TestIsMasked_Empty(t *testing.T) {
	if isMasked("") {
		t.Error("empty string should NOT be detected as masked (allow clearing keys)")
	}
}

func TestIsMasked_Sentinel(t *testing.T) {
	if !isMasked(maskSentinel) {
		t.Errorf("%q should be detected as masked", maskSentinel)
	}
}

func TestIsMasked_MaskedOutput(t *testing.T) {
	// Round-trip: maskKey output should be detected as masked
	keys := []string{"123456789", "abcdefghijklmnop", "0123456789abcdef0123456789abcdef"}
	for _, key := range keys {
		masked := maskKey(key)
		if !isMasked(masked) {
			t.Errorf("isMasked(maskKey(%q)) = false for %q", key, masked)
		}
	}
}

func TestIsMasked_RealKeys(t *testing.T) {
	// Actual API keys should NOT be detected as masked
	keys := []string{
		"abc123def456",
		"0123456789abcdef",
		"my-api-key-here",
	}
	for _, key := range keys {
		if isMasked(key) {
			t.Errorf("real key %q should not be detected as masked", key)
		}
	}
}

func TestIsMasked_ShortNonMasked(t *testing.T) {
	// Short strings that aren't the sentinel
	if isMasked("short") {
		t.Error("'short' should not be detected as masked")
	}
	if isMasked("12345678") {
		t.Error("8-char string should not be detected as masked")
	}
}

func TestMaskKey_IsMasked_Roundtrip(t *testing.T) {
	// Ensure that for any key, maskKey → isMasked is always true
	keys := []string{"a", "ab", "1234", "12345678", "123456789", "abcdefghijklmnopqrstuvwxyz"}
	for _, key := range keys {
		masked := maskKey(key)
		if !isMasked(masked) {
			t.Errorf("roundtrip failed: maskKey(%q)=%q, isMasked=false", key, masked)
		}
	}
}

// =============================================================================
// stringify
// =============================================================================

func TestStringify_Nil(t *testing.T) {
	if got := stringify(nil); got != "" {
		t.Errorf("stringify(nil) = %q, want empty", got)
	}
}

func TestStringify_String(t *testing.T) {
	if got := stringify("hello"); got != "hello" {
		t.Errorf("stringify(\"hello\") = %q", got)
	}
}

func TestStringify_Float(t *testing.T) {
	cases := []struct {
		input float64
		want  string
	}{
		{42, "42"},
		{3.14, "3.14"},
		{0, "0"},
		{-1.5, "-1.5"},
	}
	for _, c := range cases {
		got := stringify(c.input)
		if got != c.want {
			t.Errorf("stringify(%v) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestStringify_Bool(t *testing.T) {
	if got := stringify(true); got != "true" {
		t.Errorf("stringify(true) = %q", got)
	}
	if got := stringify(false); got != "false" {
		t.Errorf("stringify(false) = %q", got)
	}
}

func TestStringify_Fallback(t *testing.T) {
	// Slice should JSON-marshal
	got := stringify([]int{1, 2, 3})
	if got != "[1,2,3]" {
		t.Errorf("stringify([]int{1,2,3}) = %q, want \"[1,2,3]\"", got)
	}
}

// =============================================================================
// writeJSON
// =============================================================================

func TestWriteJSON_Success(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"key": "value"})

	res := w.Result()
	defer res.Body.Close()

	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if res.StatusCode != 200 {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["key"] != "value" {
		t.Errorf("body[key] = %q, want 'value'", body["key"])
	}
}

func TestWriteJSON_NilSlice(t *testing.T) {
	// Ensure nil slices encode as null, not crash
	w := httptest.NewRecorder()
	var s []string
	writeJSON(w, s)

	res := w.Result()
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
}

// =============================================================================
// writeError
// =============================================================================

func TestWriteError_Status(t *testing.T) {
	cases := []struct {
		status int
		msg    string
	}{
		{400, "Bad Request"},
		{404, "Not Found"},
		{500, "Internal Server Error"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		writeError(w, c.status, c.msg)

		res := w.Result()
		defer res.Body.Close()

		if res.StatusCode != c.status {
			t.Errorf("writeError(%d): status = %d", c.status, res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}

		var body map[string]string
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["error"] != c.msg {
			t.Errorf("error = %q, want %q", body["error"], c.msg)
		}
	}
}

// =============================================================================
// decodeJSON
// =============================================================================

func TestDecodeJSON_Valid(t *testing.T) {
	body := strings.NewReader(`{"name":"test","value":42}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	type payload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}
	v, ok := decodeJSON[payload](w, r, 1<<20)
	if !ok {
		t.Fatal("decodeJSON returned false for valid JSON")
	}
	if v.Name != "test" || v.Value != 42 {
		t.Errorf("got %+v", v)
	}
}

func TestDecodeJSON_Invalid(t *testing.T) {
	body := strings.NewReader(`not json`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	_, ok := decodeJSON[payload](w, r, 1<<20)
	if ok {
		t.Fatal("decodeJSON should return false for invalid JSON")
	}
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDecodeJSON_TooLarge(t *testing.T) {
	// Body exceeds the 10-byte limit
	body := strings.NewReader(`{"name":"this is way too long for the limit"}`)
	r := httptest.NewRequest(http.MethodPost, "/", body)
	w := httptest.NewRecorder()

	type payload struct {
		Name string `json:"name"`
	}
	_, ok := decodeJSON[payload](w, r, 10)
	if ok {
		t.Fatal("decodeJSON should reject oversized body")
	}
	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// =============================================================================
// requireInstance
// =============================================================================

func setupTestAppWithInstance(t *testing.T) (*core.App, core.Instance) {
	t.Helper()
	tempDir := t.TempDir()

	inst := core.Instance{
		ID:     "test-inst-1",
		Name:   "Test Radarr",
		Type:   "radarr",
		URL:    "http://localhost:7878",
		APIKey: "testapikey123456",
	}

	config := core.NewConfigStore(tempDir)
	dummyCfg := core.Config{
		Instances: []core.Instance{inst},
	}
	cfgData, _ := json.MarshalIndent(dummyCfg, "", "  ")
	os.WriteFile(filepath.Join(tempDir, "clonarr.json"), cfgData, 0644)

	if err := config.Load(); err != nil {
		t.Fatalf("Failed to load test config: %v", err)
	}

	app := &core.App{
		Config:   config,
		DebugLog: core.NewDebugLogger(tempDir),
	}
	return app, inst
}

func TestRequireInstance_Found(t *testing.T) {
	app, inst := setupTestAppWithInstance(t)
	server := &Server{Core: app}

	// Go 1.22+ ServeMux populates PathValue from route patterns.
	// For direct handler calls, we set it manually via SetPathValue.
	r := httptest.NewRequest(http.MethodGet, "/api/instances/"+inst.ID, nil)
	r.SetPathValue("id", inst.ID)
	w := httptest.NewRecorder()

	got, ok := server.requireInstance(w, r)
	if !ok {
		t.Fatal("requireInstance returned false for existing instance")
	}
	if got.ID != inst.ID {
		t.Errorf("got ID %q, want %q", got.ID, inst.ID)
	}
	if got.Name != inst.Name {
		t.Errorf("got Name %q, want %q", got.Name, inst.Name)
	}
}

func TestRequireInstance_NotFound(t *testing.T) {
	app, _ := setupTestAppWithInstance(t)
	server := &Server{Core: app}

	r := httptest.NewRequest(http.MethodGet, "/api/instances/nonexistent", nil)
	r.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	_, ok := server.requireInstance(w, r)
	if ok {
		t.Fatal("requireInstance should return false for missing instance")
	}
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// =============================================================================
// numericReleaseGroupRE
// =============================================================================

func TestNumericReleaseGroupRE_Matches(t *testing.T) {
	cases := []struct {
		input string
		group string
	}{
		{"Some.Movie.2024.1080p.WEB-DL.DDP5.1.Atmos-126811", "126811"},
		{"Title-99999", "99999"},
		{"A-1", "1"},
	}
	for _, c := range cases {
		m := numericReleaseGroupRE.FindStringSubmatch(c.input)
		if m == nil {
			t.Errorf("expected match for %q", c.input)
			continue
		}
		if m[1] != c.group {
			t.Errorf("for %q: got group %q, want %q", c.input, m[1], c.group)
		}
	}
}

func TestNumericReleaseGroupRE_NoMatch(t *testing.T) {
	cases := []string{
		"Title-FGT",        // alphanumeric group — Arr handles this
		"Title-H264",       // codec token
		"Title-123abc",     // mixed, not pure digits
		"No-Dash-At-End.",  // trailing dot
		"Title",            // no dash at all
	}
	for _, input := range cases {
		if numericReleaseGroupRE.MatchString(input) {
			t.Errorf("should not match %q", input)
		}
	}
}
