package arr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTestConnectionHTMLPage covers a URL that reaches something other than
// the Arr API (reverse proxy page, login page, web UI without the URL base):
// the error must say so instead of "invalid character '<'".
func TestTestConnectionHTMLPage(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"html content type", "text/html; charset=utf-8", "<!DOCTYPE html><html></html>"},
		{"html body without content type", "", "\n  <html><body>Login</body></html>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := NewArrClient(srv.URL, "key", "", "", srv.Client()).TestConnection()
			if !errors.Is(err, ErrHTMLResponse) {
				t.Fatalf("err = %v, want ErrHTMLResponse", err)
			}
		})
	}
}

func TestDoRequestKeepsJSONAndErrorBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/missing" {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("<html>Not found</html>"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"appName":"Sonarr","version":"4.0.0"}`))
	}))
	defer srv.Close()
	c := NewArrClient(srv.URL, "key", "", "", srv.Client())

	status, err := c.TestConnection()
	if err != nil || status.AppName != "Sonarr" {
		t.Fatalf("TestConnection = %+v, %v; want Sonarr, nil", status, err)
	}
	// A JSON body labelled text/html by a proxy is still the API.
	jsonAsHTML := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`{"appName":"Radarr","version":"6.0.0"}`))
	}))
	defer jsonAsHTML.Close()
	if st, err := NewArrClient(jsonAsHTML.URL, "key", "", "", jsonAsHTML.Client()).TestConnection(); err != nil || st.AppName != "Radarr" {
		t.Fatalf("TestConnection(JSON labelled text/html) = %+v, %v; want Radarr, nil", st, err)
	}
	// An empty success body labelled text/html (some proxies do this on
	// PUT/DELETE) is not a web page.
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
	}))
	defer emptySrv.Close()
	if _, code, err := NewArrClient(emptySrv.URL, "key", "", "", emptySrv.Client()).DoRequest("DELETE", "/customformat/1", nil); err != nil || code != http.StatusOK {
		t.Fatalf("DoRequest(empty text/html) = %d, %v; want 200, nil", code, err)
	}
	// Non-2xx responses keep returning the body so callers can report the
	// HTTP status as before.
	data, code, err := c.DoRequest("GET", "/missing", nil)
	if err != nil || code != http.StatusNotFound || len(data) == 0 {
		t.Fatalf("DoRequest(404) = %q, %d, %v; want body, 404, nil", data, code, err)
	}
}
