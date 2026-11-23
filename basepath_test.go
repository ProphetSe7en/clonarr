package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithBasePath(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("inner:" + r.URL.Path))
	})
	h := withBasePath("/clonarr", inner)
	cases := []struct {
		path     string
		code     int
		location string
		body     string
	}{
		{path: "/", code: http.StatusFound, location: "/clonarr/"},
		{path: "/clonarr", code: http.StatusMovedPermanently, location: "/clonarr/"},
		{path: "/api/health", code: http.StatusOK, body: "inner:/api/health"},
		{path: "/clonarr/api/health", code: http.StatusOK, body: "inner:/api/health"},
		{path: "/clonarr/", code: http.StatusOK, body: "inner:/"},
		{path: "/api/config", code: http.StatusNotFound},
		{path: "/login", code: http.StatusNotFound},
		{path: "/clonarrx/", code: http.StatusNotFound},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.path, nil))
		if rec.Code != c.code {
			t.Errorf("%s: code %d, want %d", c.path, rec.Code, c.code)
		}
		if c.location != "" && rec.Header().Get("Location") != c.location {
			t.Errorf("%s: Location %q, want %q", c.path, rec.Header().Get("Location"), c.location)
		}
		if c.body != "" && rec.Body.String() != c.body {
			t.Errorf("%s: body %q, want %q", c.path, rec.Body.String(), c.body)
		}
	}
}
