package desktop

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"testing"
)

func withBuiltLaunchArgs(t *testing.T, args []string, scheme string) {
	t.Helper()
	oldArgs, oldScheme, oldLaunch := builtLaunchArgs, builtDeepLinkScheme, builtLaunchScheme
	t.Cleanup(func() { builtLaunchArgs, builtDeepLinkScheme, builtLaunchScheme = oldArgs, oldScheme, oldLaunch })
	builtLaunchScheme = ""
	builtLaunchArgs = ""
	if args != nil {
		b, _ := json.Marshal(args)
		builtLaunchArgs = base64.RawURLEncoding.EncodeToString(b)
	}
	builtDeepLinkScheme = scheme
}

func TestLaunchArgs(t *testing.T) {
	withBuiltLaunchArgs(t, []string{"desktop", "--profile=work"}, "notes")
	cases := []struct {
		in, want []string
	}{
		{nil, []string{"desktop", "--profile=work"}},
		{[]string{"-psn_0_12345"}, []string{"desktop", "--profile=work"}},
		{[]string{"notes://n/1"}, []string{"desktop", "--profile=work", "notes://n/1"}},
		{[]string{"serve", "--addr=:8080"}, []string{"serve", "--addr=:8080"}},
		{[]string{"notes://n/1", "serve"}, []string{"notes://n/1", "serve"}},
		{[]string{"other://x"}, []string{"other://x"}},
	}
	for _, tc := range cases {
		if got := LaunchArgs(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("LaunchArgs(%q) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestLaunchArgsWithoutBuild(t *testing.T) {
	withBuiltLaunchArgs(t, nil, "")
	if got := LaunchArgs(nil); got != nil {
		t.Fatalf("LaunchArgs(nil) with nothing baked = %q; want nil", got)
	}
	for _, stamp := range []string{"!!not base64", base64.RawURLEncoding.EncodeToString([]byte("{not json"))} {
		builtLaunchArgs = stamp
		if got := LaunchArgs(nil); got != nil {
			t.Fatalf("stamp %q: a malformed stamp must bake in nothing, got %q", stamp, got)
		}
	}
}
