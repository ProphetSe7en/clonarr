package api

import (
	"bytes"
	"clonarr/internal/core"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func instanceJSON(t *testing.T, body any) *bytes.Reader {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	return bytes.NewReader(data)
}

func decodeInstanceResponse(t *testing.T, w *httptest.ResponseRecorder) core.Instance {
	t.Helper()
	var inst core.Instance
	if err := json.NewDecoder(w.Result().Body).Decode(&inst); err != nil {
		t.Fatalf("decode instance response: %v", err)
	}
	return inst
}

func TestHandleCreateInstanceTrimsAndPersists(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPost, "/api/instances", instanceJSON(t, map[string]string{
		"name":   " Radarr HD ",
		"type":   " radarr ",
		"url":    " http://arr.local:7878 ",
		"apiKey": " secret-key ",
	}))
	w := httptest.NewRecorder()

	server.handleCreateInstance(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	created := decodeInstanceResponse(t, w)
	stored, ok := app.Config.GetInstance(created.ID)
	if !ok {
		t.Fatalf("created instance %q not found in config", created.ID)
	}
	if stored.Name != "Radarr HD" || stored.Type != "radarr" || stored.URL != "http://arr.local:7878" || stored.APIKey != "secret-key" {
		t.Fatalf("stored instance = %#v, want trimmed fields", stored)
	}
}

func TestHandleCreateInstanceRejectsWhitespaceRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{
			name: "name",
			body: map[string]string{"name": "   ", "type": "radarr", "url": "http://arr.local:7878", "apiKey": "secret-key"},
		},
		{
			name: "url",
			body: map[string]string{"name": "Radarr HD", "type": "radarr", "url": "   ", "apiKey": "secret-key"},
		},
		{
			name: "api key",
			body: map[string]string{"name": "Radarr HD", "type": "radarr", "url": "http://arr.local:7878", "apiKey": "   "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestApp(t)
			server := &Server{Core: app}
			req := httptest.NewRequest(http.MethodPost, "/api/instances", instanceJSON(t, tc.body))
			w := httptest.NewRecorder()

			server.handleCreateInstance(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleUpdateInstanceTrimsAndPreservesWhitespaceAPIKey(t *testing.T) {
	app := setupTestApp(t)
	existing, err := app.Config.AddInstance(core.Instance{
		Name:   "Old",
		Type:   "radarr",
		URL:    "http://old.local:7878",
		APIKey: "saved-key",
	})
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPut, "/api/instances/"+existing.ID, instanceJSON(t, map[string]string{
		"name":   " Sonarr 4K ",
		"type":   " sonarr ",
		"url":    " http://new.local:8989 ",
		"apiKey": "   ",
	}))
	req.SetPathValue("id", existing.ID)
	w := httptest.NewRecorder()

	server.handleUpdateInstance(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	stored, ok := app.Config.GetInstance(existing.ID)
	if !ok {
		t.Fatalf("updated instance %q not found in config", existing.ID)
	}
	if stored.Name != "Sonarr 4K" || stored.Type != "sonarr" || stored.URL != "http://new.local:8989" || stored.APIKey != "saved-key" {
		t.Fatalf("stored instance = %#v, want trimmed fields with preserved API key", stored)
	}
}

func TestHandleUpdateInstanceRejectsWhitespaceRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{
			name: "name",
			body: map[string]string{"name": "   ", "type": "radarr", "url": "http://arr.local:7878", "apiKey": "secret-key"},
		},
		{
			name: "url",
			body: map[string]string{"name": "Radarr HD", "type": "radarr", "url": "   ", "apiKey": "secret-key"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestApp(t)
			existing, err := app.Config.AddInstance(core.Instance{
				Name:   "Old",
				Type:   "radarr",
				URL:    "http://old.local:7878",
				APIKey: "saved-key",
			})
			if err != nil {
				t.Fatalf("seed instance: %v", err)
			}
			server := &Server{Core: app}
			req := httptest.NewRequest(http.MethodPut, "/api/instances/"+existing.ID, instanceJSON(t, tc.body))
			req.SetPathValue("id", existing.ID)
			w := httptest.NewRecorder()

			server.handleUpdateInstance(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleTestConnectionRejectsWhitespaceRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{
			name: "url",
			body: map[string]string{"url": "   ", "apiKey": "secret-key"},
		},
		{
			name: "api key",
			body: map[string]string{"url": "http://arr.local:7878", "apiKey": "   "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupTestApp(t)
			server := &Server{Core: app}
			req := httptest.NewRequest(http.MethodPost, "/api/test-connection", instanceJSON(t, tc.body))
			w := httptest.NewRecorder()

			server.handleTestConnection(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestIsBlockedHost(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantBlocked bool
	}{
		{"loopback v4", "http://127.0.0.1/", true},
		{"loopback v6", "http://[::1]/", true},
		{"aws metadata", "http://169.254.169.254/", true},
		{"link-local v4", "http://169.254.1.1/", true},
		{"unspecified", "http://0.0.0.0/", true},
		{"v4-mapped loopback", "http://[::ffff:127.0.0.1]/", true},
		{"public v4", "http://1.1.1.1/", false},
		{"public v6", "http://[2606:4700:4700::1111]/", false},
		{"rfc1918", "http://10.0.0.5/", false},
		{"tailscale cgnat", "http://100.99.136.67/", false},
		{"empty", "", true},
		{"parse error", "http://%zz/", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, reason := isBlockedHost(tc.url)
			if blocked != tc.wantBlocked {
				t.Errorf("isBlockedHost(%q) blocked=%v reason=%q, want blocked=%v",
					tc.url, blocked, reason, tc.wantBlocked)
			}
			if blocked && reason == "" {
				t.Errorf("isBlockedHost(%q) blocked but returned empty reason", tc.url)
			}
			if !blocked && reason != "" {
				t.Errorf("isBlockedHost(%q) not blocked but returned reason %q", tc.url, reason)
			}
		})
	}
}
