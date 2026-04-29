package api

import (
	"clonarr/internal/core"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Fallback for Radarr/Sonarr's parse API returning empty ReleaseGroup when the
// trailing group token is numeric only (e.g. "…Atmos-126811"). Narrow enough to
// leave alphanumeric groups (handled correctly by Arr) and codec tokens alone:
// matches only a trailing dash followed by pure digits at end of string.
var numericReleaseGroupRE = regexp.MustCompile(`-(\d+)$`)

// writeJSON encodes v as JSON and writes it to w.
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	// No-store prevents shared browser caches + reverse-proxy caches from
	// retaining /api/* responses. Even with masking, a 4+4 API-key reveal
	// or config blob shouldn't live on a kiosk browser after logout.
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("writeJSON: encode error: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// decodeJSON reads and decodes JSON from the request body into T, enforcing a size limit.
// Returns the decoded value and true on success, or writes an error response and returns false.
func decodeJSON[T any](w http.ResponseWriter, r *http.Request, maxBytes int64) (T, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		writeError(w, 400, "Invalid JSON")
		return v, false
	}
	return v, true
}

// requireInstance looks up an instance by the "id" path parameter.
// Returns the instance and true on success, or writes an error response and returns false.
func (s *Server) requireInstance(w http.ResponseWriter, r *http.Request) (core.Instance, bool) {
	id := r.PathValue("id")
	inst, ok := s.Core.Config.GetInstance(id)
	if !ok {
		writeError(w, 404, "Instance not found")
	}
	return inst, ok
}

// --- Helpers ---

const maskSentinel = "********"

func maskKey(key string) string {
	if len(key) <= 8 {
		return maskSentinel
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

// isMasked detects if a key was produced by maskKey.
func isMasked(key string) bool {
	if key == maskSentinel {
		return true
	}
	// maskKey produces: 4 chars + N asterisks + 4 chars (len >= 9)
	if len(key) < 9 {
		return false
	}
	mid := key[4 : len(key)-4]
	for _, c := range mid {
		if c != '*' {
			return false
		}
	}
	return len(mid) > 0
}

// stringify converts an arbitrary value to its string representation.
func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
