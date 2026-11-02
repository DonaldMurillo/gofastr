package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `gofastr help` and `gofastr help <command>` used to answer
// "Unknown command: help" with exit 1, though `theme help` and every
// generated app CLI take the spelling.
func TestHelpCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"help"}, "gofastr <command> [arguments]"},
		{[]string{"help", "help"}, "gofastr <command> [arguments]"},
		{[]string{"help", "dev"}, "Usage: gofastr dev"},
		{[]string{"help", "m"}, "Usage: gofastr migrate"},
		{[]string{"help", "version"}, "gofastr <command> [arguments]"},
	} {
		code := -1
		out := clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() { dispatch(tc.args) })
		})
		if code != -1 {
			t.Errorf("%q exited %d, want a normal return", tc.args, code)
		}
		if !strings.Contains(out, tc.want) || strings.Contains(out, "Unknown command") {
			t.Errorf("%q printed no %q:\n%s", tc.args, tc.want, out)
		}
	}
}

func TestHelpUnknownCommandIsUnknown(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { dispatch([]string{"help", "devv"}) })
	})
	if code != 1 || !strings.Contains(out, "Unknown command: devv") || !strings.Contains(out, "gofastr dev?") {
		t.Fatalf("help devv: exit %d, want the unknown-command answer with a suggestion:\n%s", code, out)
	}
}
