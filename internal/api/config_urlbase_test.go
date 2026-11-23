package api

import (
	"clonarr/internal/core"
	"net/http"
	"testing"
)

func TestUpdateConfigURLBase(t *testing.T) {
	cases := []struct {
		name   string
		locked bool
		saved  string
		body   string
		code   int
		want   string // stored urlBase afterwards
	}{
		{name: "saves normalized", body: `{"urlBase":" /clonarr/ "}`, code: http.StatusOK, want: "/clonarr"},
		{name: "root clears", saved: "/clonarr", body: `{"urlBase":"/"}`, code: http.StatusOK, want: ""},
		{name: "empty clears", saved: "/clonarr", body: `{"urlBase":""}`, code: http.StatusOK, want: ""},
		{name: "invalid rejected, saved kept", saved: "/clonarr", body: `{"urlBase":"nope"}`, code: http.StatusBadRequest, want: "/clonarr"},
		{name: "dot segment rejected", body: `{"urlBase":"/a/../b"}`, code: http.StatusBadRequest, want: ""},
		{name: "locked rejects change", locked: true, saved: "/clonarr", body: `{"urlBase":"/other"}`, code: http.StatusForbidden, want: "/clonarr"},
		{name: "locked accepts no-op", locked: true, saved: "/clonarr", body: `{"urlBase":"/clonarr/"}`, code: http.StatusOK, want: "/clonarr"},
		{name: "locked ignores unrelated fields", locked: true, saved: "/clonarr", body: `{"devMode":true}`, code: http.StatusOK, want: "/clonarr"},
		{name: "omitted leaves it alone", saved: "/clonarr", body: `{"devMode":true}`, code: http.StatusOK, want: "/clonarr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := core.DefaultConfig()
			cfg.URLBase = c.saved
			server, app := setupConfigScheduleServer(t, cfg)
			server.BasePathLocked = c.locked
			w := putConfigSchedule(t, server, c.body)
			if w.Code != c.code {
				t.Fatalf("status = %d, want %d: %s", w.Code, c.code, w.Body.String())
			}
			if got := app.Config.Get().URLBase; got != c.want {
				t.Fatalf("stored urlBase = %q, want %q", got, c.want)
			}
		})
	}
}
