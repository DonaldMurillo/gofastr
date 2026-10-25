package minify

import "testing"

// The env contract behind Enabled: production wins by default, dev
// opts out, manual overrides trump env detection. decideEnabled is the
// uncached decision, so each row can set its own env.
func TestEnabledEnvTable(t *testing.T) {
	cases := []struct {
		env, dev, nomin, min string
		want                 bool
	}{
		{want: true},                              // nothing set: production default
		{dev: "1", want: false},                   // dev keeps sources raw
		{env: "production", dev: "1", want: true}, // a prod env beats a stray dev flag
		{env: "STAGING", want: true},
		{env: "development", dev: "true", want: false},
		{env: "production", nomin: "1", want: false}, // manual override wins
		{dev: "1", min: "1", want: true},
		{nomin: "1", min: "1", want: false}, // nominify checked first
	}
	for _, c := range cases {
		t.Setenv("GOFASTR_ENV", c.env)
		t.Setenv("GOFASTR_DEV", c.dev)
		t.Setenv("RUNTIME_NOMINIFY", c.nomin)
		t.Setenv("RUNTIME_MINIFY", c.min)
		if got := decideEnabled(); got != c.want {
			t.Errorf("env=%q dev=%q nominify=%q minify=%q: Enabled = %v, want %v", c.env, c.dev, c.nomin, c.min, got, c.want)
		}
	}
}
