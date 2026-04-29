package agents

import (
	"fmt"
	"io"
	"net/http"
)

// drainAndClose discards the response body and closes it, ensuring the
// underlying TCP connection is returned to the HTTP client's connection pool
// for reuse. Without draining, Go's http.Transport cannot recycle the
// connection, causing a buildup of TIME_WAIT sockets during notification bursts.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// httpError builds an error from an HTTP response with a non-2xx status.
// It reads up to 512 bytes of the response body to include the server's error
// message (e.g. Discord rate-limit details, Pushover validation errors).
// The body is fully drained after reading so the connection can be reused.
func httpError(provider string, resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	// Drain any remaining body bytes beyond our 512-byte read.
	io.Copy(io.Discard, resp.Body)

	if len(snippet) > 0 {
		return fmt.Errorf("%s returned %d: %s", provider, resp.StatusCode, string(snippet))
	}
	return fmt.Errorf("%s returned %d", provider, resp.StatusCode)
}
