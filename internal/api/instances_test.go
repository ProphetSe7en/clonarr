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

func TestHandleUpdateInstanceRejectsExternalAuthWithoutPassword(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPut, "/api/instances/"+existing.ID, instanceJSON(t, map[string]any{
		"name":         "Radarr HD",
		"type":         "radarr",
		"url":          "http://arr.local:7878",
		"apiKey":       "saved-key",
		"externalAuth": true,
		"username":     "user",
		"password":     "",
	}))
	req.SetPathValue("id", existing.ID)
	w := httptest.NewRecorder()

	server.handleUpdateInstance(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateInstanceAllowsExternalAuthWithStoredPassword(t *testing.T) {
	app := setupTestApp(t)
	existing, err := app.Config.AddInstance(core.Instance{
		Name:         "Old",
		Type:         "radarr",
		URL:          "http://old.local:7878",
		APIKey:       "saved-key",
		ExternalAuth: true,
		Username:     "user",
		Password:     "saved-pass",
	})
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPut, "/api/instances/"+existing.ID, instanceJSON(t, map[string]any{
		"name":         "Radarr HD",
		"type":         "radarr",
		"url":          "http://arr.local:7878",
		"apiKey":       "saved-key",
		"externalAuth": true,
		"username":     "user",
		"password":     "",
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
	if stored.Password != "saved-pass" {
		t.Fatalf("stored password = %q, want preserved %q", stored.Password, "saved-pass")
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

// TestHandleTestConnectionUsesStoredSecretsForEditedURL covers testing an
// edited instance: the form sends the new URL with a blank or masked API key
// and password, and the test must reach the NEW URL with the SAVED secrets.
func TestHandleTestConnectionUsesStoredSecretsForEditedURL(t *testing.T) {
	cases := []struct {
		name     string
		apiKey   string
		password string
	}{
		{"blank secrets", "", ""},
		{"masked secrets", maskKey("saved-api-key-1234"), maskKey("saved-password-5678")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotKey, gotUser, gotPass string
			arrServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotKey = r.Header.Get("X-Api-Key")
				gotUser, gotPass, _ = r.BasicAuth()
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"appName":"Radarr","version":"6.0.0"}`))
			}))
			defer arrServer.Close()

			app := setupTestApp(t)
			app.HTTPClient = arrServer.Client()
			existing, err := app.Config.AddInstance(core.Instance{
				Name:         "Radarr",
				Type:         "radarr",
				URL:          "http://old-address.invalid:7878",
				APIKey:       "saved-api-key-1234",
				ExternalAuth: true,
				Username:     "saved-user",
				Password:     "saved-password-5678",
			})
			if err != nil {
				t.Fatalf("seed instance: %v", err)
			}
			server := &Server{Core: app}
			req := httptest.NewRequest(http.MethodPost, "/api/test-connection", instanceJSON(t, map[string]any{
				"instanceId":   existing.ID,
				"url":          arrServer.URL,
				"apiKey":       tc.apiKey,
				"externalAuth": true,
				"username":     "new-user",
				"password":     tc.password,
			}))
			w := httptest.NewRecorder()

			server.handleTestConnection(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, http.StatusOK, w.Body.String())
			}
			var resp map[string]any
			if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp["connected"] != true {
				t.Fatalf("connected = %v, want true (error %v)", resp["connected"], resp["error"])
			}
			if gotKey != "saved-api-key-1234" {
				t.Errorf("X-Api-Key = %q, want the saved key", gotKey)
			}
			if gotUser != "new-user" || gotPass != "saved-password-5678" {
				t.Errorf("basic auth = %q/%q, want the form username with the saved password", gotUser, gotPass)
			}
		})
	}
}

func TestHandleTestConnectionUnknownInstanceStillRequiresAPIKey(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPost, "/api/test-connection", instanceJSON(t, map[string]string{
		"instanceId": "does-not-exist",
		"url":        "http://arr.local:7878",
		"apiKey":     "",
	}))
	w := httptest.NewRecorder()

	server.handleTestConnection(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateInstanceRejectsMetadataAddress(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	req := httptest.NewRequest(http.MethodPost, "/api/instances", instanceJSON(t, map[string]string{
		"name":   "Radarr",
		"type":   "radarr",
		"url":    "169.254.169.254",
		"apiKey": "secret-key",
	}))
	w := httptest.NewRecorder()

	server.handleCreateInstance(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateInstanceMetadataAddress(t *testing.T) {
	app := setupTestApp(t)
	server := &Server{Core: app}
	update := func(id, url string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/instances/"+id, instanceJSON(t, map[string]string{
			"name": "Renamed",
			"type": "radarr",
			"url":  url,
		}))
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		server.handleUpdateInstance(w, req)
		return w.Code
	}

	normal, err := app.Config.AddInstance(core.Instance{Name: "Radarr", Type: "radarr", URL: "http://10.0.0.5:7878", APIKey: "saved-key"})
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	if code := update(normal.ID, "http://169.254.169.254/"); code != http.StatusBadRequest {
		t.Fatalf("changing URL to a metadata address: status = %d, want %d", code, http.StatusBadRequest)
	}

	// An instance saved with a metadata URL (seeded straight into the config,
	// as if saved before the check existed) can still be edited without
	// changing the URL: the check only runs when the URL changes.
	legacy, err := app.Config.AddInstance(core.Instance{Name: "Legacy", Type: "radarr", URL: "http://169.254.169.254/", APIKey: "saved-key"})
	if err != nil {
		t.Fatalf("seed legacy instance: %v", err)
	}
	if code := update(legacy.ID, "http://169.254.169.254/"); code != http.StatusOK {
		t.Fatalf("saving with the unchanged URL: status = %d, want %d", code, http.StatusOK)
	}
}

func TestIsBlockedHost(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantBlocked bool
	}{
		{"loopback v4", "http://127.0.0.1/", false},
		{"loopback v6", "http://[::1]/", false},
		{"localhost name", "http://localhost:7878/", false},
		{"v4-mapped loopback", "http://[::ffff:127.0.0.1]/", false},
		{"no scheme", "radarr.invalid:7878", false},
		{"aws metadata", "http://169.254.169.254/", true},
		{"metadata without scheme", "169.254.169.254", true},
		{"v4-mapped metadata", "http://[::ffff:169.254.169.254]/", true},
		{"ecs task metadata", "http://169.254.170.2/", true},
		{"aws ipv6 metadata", "http://[fd00:ec2::254]/", true},
		{"other link-local v4", "http://169.254.1.1/", false},
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
