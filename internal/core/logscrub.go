package core

import (
	"regexp"
	"strings"
)

// flattenForLog collapses newlines, tabs, and runs of whitespace into
// single spaces. Used to keep multi-line error bodies on a single
// debug.log line so an op-tagged trace stays greppable. Without this,
// an Arr 400 response with structured JSON and embedded \n splits the
// trace across multiple physical log lines that aren't tagged with
// the operation ID, breaking grep-by-id retrieval.
func flattenForLog(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return whitespaceRun.ReplaceAllString(s, " ")
}

var whitespaceRun = regexp.MustCompile(`  +`)

// redactSecrets walks a string and replaces well-known secret-bearing
// patterns with [REDACTED]. Used before any HTTP error body, exception
// trace, or other potentially-arbitrary text is written to debug.log.
//
// The patterns target the secret formats that actually flow through
// clonarr — Radarr/Sonarr API keys, common HTTP auth headers, JWTs,
// and query/form parameters with `apikey`-style names. The list is
// intentionally conservative: better to leave a few non-secret strings
// untouched than to blanket-mask anything that looks vaguely random.
//
// Add a new pattern here when a secret format we use shows up in logs.
func redactSecrets(s string) string {
	for _, p := range secretPatterns {
		s = p.regex.ReplaceAllString(s, p.replacement)
	}
	return s
}

type secretPattern struct {
	regex       *regexp.Regexp
	replacement string
}

// secretPatterns are evaluated in order — earlier patterns win when
// regions overlap. Most specific patterns first (Bearer tokens have
// a fixed prefix and tend to surround hex blobs that the generic
// hex matcher would also catch — scoping them first preserves the
// helpful "Authorization: Bearer [REDACTED]" framing).
var secretPatterns = []secretPattern{
	// Authorization: Bearer <token>  /  Authorization: Token <token>.
	// Token character class deliberately excludes comma/semicolon/whitespace
	// so subsequent fields in a multi-header line aren't swallowed.
	{
		regex:       regexp.MustCompile(`(?i)(authorization\s*:\s*)(bearer|token)\s+[A-Za-z0-9._\-]+`),
		replacement: "${1}${2} [REDACTED]",
	},
	// Bare "Bearer <token>" or "Token <token>" anywhere
	{
		regex:       regexp.MustCompile(`(?i)\b(bearer|token)\s+[A-Za-z0-9._\-]{16,}\b`),
		replacement: "${1} [REDACTED]",
	},
	// JWT — three base64url segments separated by dots, starting with eyJ
	{
		regex:       regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`),
		replacement: "[REDACTED]",
	},
	// X-Api-Key / X-API-Key headers
	{
		regex:       regexp.MustCompile(`(?i)(x-api-key\s*:\s*)\S+`),
		replacement: "${1}[REDACTED]",
	},
	// Query/form params with secret-bearing names: apikey, api_key,
	// access_token, secret. Values masked but param name preserved
	// so the trace still reads as "?apikey=[REDACTED]&page=2".
	{
		regex:       regexp.MustCompile(`(?i)((?:apikey|api_key|access_token|secret)\s*[=:]\s*)([^&\s"']+)`),
		replacement: "${1}[REDACTED]",
	},
	// Radarr/Sonarr API keys are 32 lowercase hex chars. Match 32+ to
	// catch other key formats too. Word-boundary anchored so hex inside
	// longer non-hex strings is left alone.
	{
		regex:       regexp.MustCompile(`\b[0-9a-f]{32,}\b`),
		replacement: "[REDACTED]",
	},
}
