package arr

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An Arr address that answers with a redirect used to turn every write into a
// read, because Go's default client reissues a redirected POST or PUT as a
// GET. Reported as a create failing with "cannot unmarshal array into Go value
// of type arr.ArrCF": the create had landed on the list endpoint.
//
// The update half was worse. It landed on the single-item endpoint, which
// answers with one format, so it parsed cleanly and was counted as written
// while the instance was never touched.
func TestRedirectIsRefusedRatherThanTurnedIntoARead(t *testing.T) {
	var sawOnReal []string
	real := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawOnReal = append(sawOnReal, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v3/customformat" {
			_ = json.NewEncoder(w).Encode([]ArrCF{{ID: 1, Name: "already here"}})
			return
		}
		_ = json.NewEncoder(w).Encode(ArrCF{ID: 7, Name: "already here"})
	}))
	defer real.Close()

	// The front door redirects, the way an http to https hop or a proxy adding
	// a URL base does.
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, real.URL+r.URL.Path, http.StatusFound)
	}))
	defer front.Close()

	c := NewArrClient(front.URL, "k", "", "", NewHTTPClient(10*time.Second))

	if _, err := c.CreateCustomFormat(&ArrCF{Name: "new one"}); !errors.Is(err, ErrRedirected) {
		t.Errorf("create: want ErrRedirected, got %v", err)
	}
	if _, err := c.UpdateCustomFormat(7, &ArrCF{Name: "new one"}); !errors.Is(err, ErrRedirected) {
		t.Errorf("update: want ErrRedirected, got %v", err)
	}
	// The point of the fix: nothing reached the instance at all, rather than
	// reaching it as a read that looks like a successful write.
	if len(sawOnReal) != 0 {
		t.Errorf("requests should not have been followed through: %v", sawOnReal)
	}
	// A read of the same address fails the same way, so the connection test
	// names the problem instead of the instance half working.
	if _, err := c.TestConnection(); !errors.Is(err, ErrRedirected) {
		t.Errorf("test connection: want ErrRedirected, got %v", err)
	}
}

// A plain address must keep working untouched.
func TestNoRedirectStillWrites(t *testing.T) {
	var method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Method
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ArrCF{ID: 42, Name: "new one"})
	}))
	defer srv.Close()

	c := NewArrClient(srv.URL, "k", "", "", NewHTTPClient(10*time.Second))
	got, err := c.CreateCustomFormat(&ArrCF{Name: "new one"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if got.ID != 42 {
		t.Errorf("id = %d, want 42", got.ID)
	}
}
