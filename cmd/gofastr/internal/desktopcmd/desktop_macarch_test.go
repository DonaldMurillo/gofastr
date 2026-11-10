package desktopcmd

import (
	"slices"
	"strings"
	"testing"
)

// The macOS build once set GOARCH=arm64 whatever the caller asked for,
// and an Intel Mac got a bundle that could not launch there (2026-10-27
// G8). The shell now builds for both Mac architectures (#604): no GOARCH
// means a universal bundle, arm64 or amd64 means that one slice, and
// anything else is refused by name.
func TestMacDesktopArchs(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want []string
	}{
		{"", []string{"arm64", "amd64"}},
		{"arm64", []string{"arm64"}},
		{"amd64", []string{"amd64"}},
		{"386", nil},
		{"universal", nil},
	} {
		got, err := macDesktopArchs(tc.env)
		if !slices.Equal(got, tc.want) {
			t.Errorf("GOARCH=%q: archs=%v, want %v", tc.env, got, tc.want)
		}
		if (err != nil) != (tc.want == nil) {
			t.Errorf("GOARCH=%q: err=%v, want error %t", tc.env, err, tc.want == nil)
		}
		if err != nil && !strings.Contains(err.Error(), "GOARCH="+tc.env) {
			t.Errorf("GOARCH=%q: error %q does not name the refused arch", tc.env, err)
		}
	}
}
