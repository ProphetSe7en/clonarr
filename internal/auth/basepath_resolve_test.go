package auth

import "testing"

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
