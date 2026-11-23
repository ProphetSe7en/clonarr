package auth

import (
	"strings"
	"testing"
)

func TestResolveBasePath(t *testing.T) {
	cases := []struct {
		name, env, cfg string
		want           string
		locked         bool
		cfgErr, err    bool
	}{
		{name: "nothing set", want: ""},
		{name: "config only", cfg: "/clonarr", want: "/clonarr"},
		{name: "config trailing slash", cfg: "/clonarr/", want: "/clonarr"},
		{name: "env root does not lock", env: "/", cfg: "/clonarr", want: "/clonarr"},
		{name: "env empty does not lock", env: "  ", cfg: "/clonarr", want: "/clonarr"},
		{name: "env wins and locks", env: "/proxy", cfg: "/clonarr", want: "/proxy", locked: true},
		{name: "invalid env is fatal", env: "clonarr", err: true},
		{name: "invalid config falls back to root", cfg: "../x", want: "", cfgErr: true},
		{name: "slash-only env is root, does not lock", env: "//", cfg: "/clonarr", want: "/clonarr"},
		{name: "slash-only config is root", cfg: "///", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, locked, cfgErr, err := ResolveBasePath(c.env, c.cfg)
			if (err != nil) != c.err || (cfgErr != nil) != c.cfgErr {
				t.Fatalf("err=%v cfgErr=%v", err, cfgErr)
			}
			if got != c.want || locked != c.locked {
				t.Fatalf("got (%q, %v), want (%q, %v)", got, locked, c.want, c.locked)
			}
		})
	}
}

func TestNormalizeBasePath(t *testing.T) {
	ok := map[string]string{
		"": "", "/": "", "//": "", "///": "", " // ": "",
		"/clonarr": "/clonarr", "/clonarr/": "/clonarr", "/clonarr//": "/clonarr",
		" /a/b ": "/a/b", "/0 /": "/0", "/clonarr / /": "/clonarr", "/my app": "/my app", "/.well": "/.well", "/a..b": "/a..b",
	}
	for in, want := range ok {
		got, err := NormalizeBasePath(in)
		if err != nil || got != want {
			t.Errorf("NormalizeBasePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"clonarr", "//a", "/a//b", "/a?x", "/a#x", "/a/./b", "/a/../b", "/..", "/.", "/a\x00", "/a\x7f"} {
		if got, err := NormalizeBasePath(in); err == nil {
			t.Errorf("NormalizeBasePath(%q) = %q, want error", in, got)
		}
	}
}

// FuzzNormalizeBasePath checks the invariants every caller relies on: it
// never panics, an accepted value is "" or a clean "/seg[/seg...]" path, and
// normalizing is idempotent.
func FuzzNormalizeBasePath(f *testing.F) {
	for _, s := range []string{"", "/", "//", "///", " // ", "/clonarr", "/clonarr/", "/a//b", "/a/../b", "/..", "clonarr", "/a?b", "/\x00", "/0 /", "/a\t/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out, err := NormalizeBasePath(in)
		if err != nil {
			return
		}
		if out != "" {
			if !strings.HasPrefix(out, "/") || strings.HasSuffix(out, "/") || strings.Contains(out, "//") || strings.ContainsAny(out, "?#") {
				t.Fatalf("NormalizeBasePath(%q) = %q: not a clean path", in, out)
			}
			for _, seg := range strings.Split(out[1:], "/") {
				if seg == "." || seg == ".." {
					t.Fatalf("NormalizeBasePath(%q) = %q: dot segment", in, out)
				}
			}
		}
		again, err := NormalizeBasePath(out)
		if err != nil || again != out {
			t.Fatalf("not idempotent: %q -> %q -> %q, %v", in, out, again, err)
		}
	})
}
