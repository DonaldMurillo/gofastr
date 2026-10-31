package desktopcmd

import (
	"strings"
	"testing"
)

// The macOS build set GOARCH=arm64 whatever the caller asked for, and an
// Intel Mac got a bundle reported written that could not launch there
// (2026-10-27 G8). The shell is arm64-only, so the target stays arm64:
// another GOARCH is refused, and an Intel host is told.
func TestMacDesktopArchRefusesAndWarns(t *testing.T) {
	for _, tc := range []struct {
		env, host string
		wantErr   bool
		wantWarn  bool
	}{
		{"", "arm64", false, false},
		{"arm64", "arm64", false, false},
		{"", "amd64", false, true},
		{"arm64", "amd64", false, true},
		{"amd64", "arm64", true, false},
		{"386", "amd64", true, false},
	} {
		warn, err := macDesktopArch(tc.env, tc.host)
		if (err != nil) != tc.wantErr {
			t.Errorf("GOARCH=%q host=%s: err=%v, want error %t", tc.env, tc.host, err, tc.wantErr)
		}
		if (warn != "") != tc.wantWarn {
			t.Errorf("GOARCH=%q host=%s: warn=%q, want warning %t", tc.env, tc.host, warn, tc.wantWarn)
		}
		if err != nil && !strings.Contains(err.Error(), tc.env) {
			t.Errorf("GOARCH=%q: error %q does not name the refused arch", tc.env, err)
		}
	}
}
