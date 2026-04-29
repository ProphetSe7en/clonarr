package agents

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// mockPoster is a configurable HTTP client stub for provider tests.
// It records the last request URL and returns canned responses.
type mockPoster struct {
	statusCode int    // HTTP status to return
	err        error  // if non-nil, Post returns this error
	lastURL    string // captures the URL of the most recent Post call
}

func (m *mockPoster) Post(url, contentType string, body io.Reader) (*http.Response, error) {
	m.lastURL = url
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func (m *mockPoster) Do(req *http.Request) (*http.Response, error) {
	m.lastURL = req.URL.String()
	if m.err != nil {
		return nil, m.err
	}
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

// newOKPoster returns a mock that always responds with 200 OK.
func newOKPoster() *mockPoster {
	return &mockPoster{statusCode: 200}
}

// newErrorPoster returns a mock that always returns a network error.
func newErrorPoster(msg string) *mockPoster {
	return &mockPoster{err: fmt.Errorf("%s", msg)}
}

// newStatusPoster returns a mock that responds with a fixed status code.
func newStatusPoster(code int) *mockPoster {
	return &mockPoster{statusCode: code}
}

// testRuntime returns an agents.Runtime wired to the given mocks.
// Pass nil for a client to simulate a missing (unconfigured) client.
func testRuntime(notify, safe *mockPoster) Runtime {
	r := Runtime{Version: "test"}
	if notify != nil {
		r.NotifyClient = notify
	}
	if safe != nil {
		r.SafeClient = safe
	}
	return r
}
