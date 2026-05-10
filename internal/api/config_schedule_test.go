package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setupConfigScheduleServer(t *testing.T, cfg *core.Config) (*Server, *core.App) {
	t.Helper()
	tempDir := t.TempDir()
	store := core.NewConfigStore(tempDir)
	if cfg == nil {
		cfg = core.DefaultConfig()
	}
	if err := store.Set(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	app := &core.App{
		Config:       store,
		Trash:        core.NewTrashStore(tempDir),
		DebugLog:     core.NewDebugLogger(tempDir),
		ActivityLog:  core.NewActivityLogger(tempDir),
		PullUpdateCh: make(chan string, 1),
	}
	return &Server{Core: app}, app
}

func TestServerTimeInfoAt(t *testing.T) {
	utc := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	info := serverTimeInfoAt(utc, false)
	if info.ServerTimeZone != "UTC" {
		t.Fatalf("ServerTimeZone = %q, want UTC", info.ServerTimeZone)
	}
	if label := serverTimeZoneLabel(utc); label != "UTC" {
		t.Fatalf("serverTimeZoneLabel = %q, want UTC", label)
	}
	if info.ServerTimeZoneOffset != 0 {
		t.Fatalf("ServerTimeZoneOffset = %d, want 0", info.ServerTimeZoneOffset)
	}
	if info.ServerTimeZoneConfigured {
		t.Fatalf("ServerTimeZoneConfigured = true, want false")
	}
	if info.ServerNow != "2026-05-10T12:00:00Z" {
		t.Fatalf("ServerNow = %q, want RFC3339 UTC time", info.ServerNow)
	}

	loc := time.FixedZone("CDT", -5*60*60)
	local := time.Date(2026, time.May, 10, 9, 30, 0, 0, loc)
	info = serverTimeInfoAt(local, true)
	if info.ServerTimeZone != "CDT" {
		t.Fatalf("ServerTimeZone = %q, want CDT", info.ServerTimeZone)
	}
	if info.ServerTimeZoneOffset != -5*60*60 {
		t.Fatalf("ServerTimeZoneOffset = %d, want -18000", info.ServerTimeZoneOffset)
	}
	if !info.ServerTimeZoneConfigured {
		t.Fatalf("ServerTimeZoneConfigured = false, want true")
	}
	if info.ServerNow != "2026-05-10T09:30:00-05:00" {
		t.Fatalf("ServerNow = %q, want RFC3339 CDT time", info.ServerNow)
	}
}

func TestTrashStatusIncludesServerTiming(t *testing.T) {
	server, app := setupConfigScheduleServer(t, core.DefaultConfig())
	next := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	app.SetNextPullAt(next)

	req := httptest.NewRequest(http.MethodGet, "/api/trash/status", nil)
	w := httptest.NewRecorder()
	server.handleTrashStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var st core.TrashStatus
	if err := json.NewDecoder(w.Body).Decode(&st); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if st.ServerNow == "" {
		t.Fatalf("ServerNow missing")
	}
	if st.NextPull == "" {
		t.Fatalf("NextPull missing")
	}
	if st.NextPullClock != "12:00" {
		t.Fatalf("NextPullClock = %q, want 12:00", st.NextPullClock)
	}
}

func putConfigSchedule(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.handleUpdateConfig(w, req)
	return w
}

func TestUpdateConfigSpecificPullScheduleValid(t *testing.T) {
	server, app := setupConfigScheduleServer(t, core.DefaultConfig())

	w := putConfigSchedule(t, server, `{"pullInterval":"specific","pullSchedule":{"mode":"daily","time":"03:00","dayOfWeek":0,"dayOfMonth":1}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	cfg := app.Config.Get()
	if cfg.PullInterval != "specific" {
		t.Fatalf("PullInterval = %q, want specific", cfg.PullInterval)
	}
	if cfg.PullSchedule == nil || cfg.PullSchedule.Mode != "daily" || cfg.PullSchedule.Time != "03:00" {
		t.Fatalf("PullSchedule = %+v, want daily 03:00", cfg.PullSchedule)
	}
}

func TestUpdateConfigSpecificRequiresSchedule(t *testing.T) {
	server, _ := setupConfigScheduleServer(t, core.DefaultConfig())

	w := putConfigSchedule(t, server, `{"pullInterval":"specific"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestUpdateConfigSpecificUsesExistingSchedule(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.PullSchedule = &core.PullSchedule{Mode: "weekly", Time: "04:00", DayOfWeek: 0, DayOfMonth: 1}
	server, app := setupConfigScheduleServer(t, cfg)

	w := putConfigSchedule(t, server, `{"pullInterval":"specific"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := app.Config.Get().PullSchedule; got == nil || got.DayOfWeek != 0 {
		t.Fatalf("PullSchedule = %+v, want existing Sunday schedule", got)
	}
}

func TestUpdateConfigScheduleWithoutIntervalWhenSpecific(t *testing.T) {
	cfg := core.DefaultConfig()
	cfg.PullInterval = "specific"
	cfg.PullSchedule = &core.PullSchedule{Mode: "daily", Time: "03:00", DayOfWeek: 0, DayOfMonth: 1}
	server, app := setupConfigScheduleServer(t, cfg)

	w := putConfigSchedule(t, server, `{"pullSchedule":{"mode":"weekly","time":"04:00","dayOfWeek":0,"dayOfMonth":1}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	got := app.Config.Get().PullSchedule
	if got == nil || got.Mode != "weekly" || got.DayOfWeek != 0 {
		t.Fatalf("PullSchedule = %+v, want weekly Sunday", got)
	}
}

func TestUpdateConfigScheduleIgnoredWhenIntervalIsNotSpecific(t *testing.T) {
	server, app := setupConfigScheduleServer(t, core.DefaultConfig())

	w := putConfigSchedule(t, server, `{"pullSchedule":{"mode":"weekly","time":"25:00","dayOfWeek":7,"dayOfMonth":31}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := app.Config.Get().PullSchedule; got != nil {
		t.Fatalf("PullSchedule = %+v, want nil because fixed-interval schedule update is ignored", got)
	}
}

func TestUpdateConfigSpecificPullScheduleInvalid(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid time",
			body: `{"pullInterval":"specific","pullSchedule":{"mode":"daily","time":"25:00","dayOfWeek":0,"dayOfMonth":1}}`,
		},
		{
			name: "invalid weekday",
			body: `{"pullInterval":"specific","pullSchedule":{"mode":"weekly","time":"03:00","dayOfWeek":7,"dayOfMonth":1}}`,
		},
		{
			name: "invalid month day zero",
			body: `{"pullInterval":"specific","pullSchedule":{"mode":"monthly","time":"03:00","dayOfWeek":0,"dayOfMonth":0}}`,
		},
		{
			name: "invalid month day above max",
			body: `{"pullInterval":"specific","pullSchedule":{"mode":"monthly","time":"03:00","dayOfWeek":0,"dayOfMonth":31}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := setupConfigScheduleServer(t, core.DefaultConfig())
			w := putConfigSchedule(t, server, tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestPullScheduleSundayRoundTripsInConfigJSON(t *testing.T) {
	server, _ := setupConfigScheduleServer(t, core.DefaultConfig())

	w := putConfigSchedule(t, server, `{"pullInterval":"specific","pullSchedule":{"mode":"weekly","time":"03:00","dayOfWeek":0,"dayOfMonth":1}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	getW := httptest.NewRecorder()
	server.handleGetConfig(getW, req)
	res := getW.Result()
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	var sched map[string]json.RawMessage
	if err := json.Unmarshal(root["pullSchedule"], &sched); err != nil {
		t.Fatalf("decode pullSchedule: %v", err)
	}
	v, ok := sched["dayOfWeek"]
	if !ok {
		t.Fatalf("dayOfWeek missing from pullSchedule JSON: %s", raw)
	}
	if string(v) != "0" {
		t.Fatalf("dayOfWeek = %s, want 0", v)
	}
}
