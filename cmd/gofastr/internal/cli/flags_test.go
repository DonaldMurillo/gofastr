package cli

import (
	"strings"
	"testing"
)

func TestParseFlagsBothForms(t *testing.T) {
	var addr, sign string
	var quiet bool
	seen, err := ParseFlags([]string{"--addr", ":9000", "--sign=-x", "--quiet"},
		map[string]*string{"--addr": &addr, "--sign": &sign}, map[string]*bool{"--quiet": &quiet})
	if err != nil || addr != ":9000" || sign != "-x" || !quiet || !seen["--addr"] || !seen["--quiet"] {
		t.Fatalf("addr %q sign %q quiet %v seen %v err %v", addr, sign, quiet, seen, err)
	}
	if _, err := ParseFlags([]string{"--sign", "-"}, map[string]*string{"--sign": &sign}, nil); err != nil || sign != "-" {
		t.Fatalf("lone dash: sign %q err %v, want the ad-hoc identity kept", sign, err)
	}
}

func TestParseFlagsRefusals(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--addr", "--quiet"}, "--addr needs a value"},
		{[]string{"--addr", "-p"}, "--addr needs a value"},
		{[]string{"--addr"}, "--addr needs a value"},
		{[]string{"--adr=:1"}, "unknown flag --adr"},
		{[]string{"app"}, `unexpected argument "app"`},
		{[]string{"-"}, `unexpected argument "-"`},
		{[]string{"--quiet=false"}, "--quiet takes no value"},
	}
	for _, tc := range cases {
		var addr string
		var quiet bool
		_, err := ParseFlags(tc.args, map[string]*string{"--addr": &addr}, map[string]*bool{"--quiet": &quiet})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ParseFlags(%q) = %v, want %q", tc.args, err, tc.want)
		}
		if quiet {
			t.Errorf("ParseFlags(%q) set --quiet", tc.args)
		}
	}
}
