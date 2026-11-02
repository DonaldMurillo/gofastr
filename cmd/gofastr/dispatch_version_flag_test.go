package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --version and -v are global flags: they print the version only in the
// command's place. After a command they belong to that command. The
// dispatcher used to take them anywhere in argv, so `gofastr test -v`
// printed the version and exited 0 without running a test (a green CI
// step that tested nothing), and `gofastr verify -v`, verify's own
// verbose flag, never reached verify.
func TestVersionFlagIsGlobalOnlyInTheCommandSlot(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"--version"}, true},
		{[]string{"-v"}, true},
		{[]string{"--version", "dev"}, true},
		{[]string{"test", "-v"}, false},
		{[]string{"verify", "-v"}, false},
		{[]string{"dev", "--addr", ":0", "-v"}, false},
		{[]string{"desktop", "build", "--version", "1.2.3"}, false},
		{[]string{"docs", "--version"}, false},
	} {
		if got := isGlobalVersionFlag(tc.args); got != tc.want {
			t.Errorf("isGlobalVersionFlag(%q) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

// End to end through dispatch: `dev -v` reaches dev's flag parser, which
// refuses the unknown flag, instead of printing the version and exiting 0.
func TestDispatchDevDashVIsDevsFlag(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"dev", "-v"}) })
	})
	if strings.Contains(out, "commit:") {
		t.Fatalf("dev -v printed the version:\n%s", out)
	}
	if code == 0 || code == -1 || !strings.Contains(out, "unknown flag -v") {
		t.Fatalf("dev -v: exit %d, want dev's unknown-flag refusal:\n%s", code, out)
	}
}
