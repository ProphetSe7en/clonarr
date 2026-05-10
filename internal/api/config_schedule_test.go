package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		DebugLog:     core.NewDebugLogger(tempDir),
		ActivityLog:  core.NewActivityLogger(tempDir),
		PullUpdateCh: make(chan string, 1),
	}
	return &Server{Core: app}, app
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
