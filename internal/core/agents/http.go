package agents

import (
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
