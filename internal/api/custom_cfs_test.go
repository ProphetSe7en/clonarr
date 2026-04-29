package api

import "testing"

// TestEnsureCustomPrefix verifies the "!"-prefix helper covers the cases the
// create/update/import handlers depend on:
//   - leading "!" preserved
//   - leading whitespace trimmed before the check (so " PCOK" -> "!PCOK")
//   - already-correct names round-trip unchanged
func TestEnsureCustomPrefix(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"PCOK", "!PCOK"},
		{"!PCOK", "!PCOK"},
		{"  PCOK", "!PCOK"},        // leading whitespace
		{"PCOK  ", "!PCOK"},        // trailing whitespace also trimmed
		{"  !PCOK  ", "!PCOK"},     // both sides + already prefixed
		{"!", "!"},                 // pathological — caller validates emptiness
		{"!!Foo", "!!Foo"},         // already prefixed (with extra !), no double-bang
		{"My Format", "!My Format"},
	}
	for _, c := range cases {
		got := ensureCustomPrefix(c.in)
		if got != c.want {
			t.Errorf("ensureCustomPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
