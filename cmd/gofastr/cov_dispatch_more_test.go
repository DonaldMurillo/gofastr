package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestDispatchRoutesNonBlocking pins that each command reaches its own
// subcommand: the exit code and a line only that subcommand prints.
func TestDispatchRoutesNonBlocking(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"theme"}, -1, "Usage: gofastr theme <subcommand>"},
		{[]string{"new", "-h"}, -1, "Usage: gofastr new <resource> <name>"},
		{[]string{"docs", "--help"}, -1, "gofastr docs: browse framework docs"},
		{[]string{"semantic", "help"}, -1, "gofastr semantic: semantic search"},
		{[]string{"gen", "entity", "Foo", "name:string"}, 1, "Usage: gofastr generate entity <name>"},
		{[]string{"migrate", "bogus"}, 1, `Unknown migrate subcommand: "bogus"`},
		{[]string{"agents"}, 1, "Usage: gofastr agents [init|sync|skill]"},
		{[]string{"audit"}, 2, "Usage: gofastr audit <subcommand>"},
	} {
		code := -1
		out := clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() { dispatch(tc.args) })
		})
		if code != tc.code || !strings.Contains(out, tc.want) {
			t.Errorf("%q: exit %d (want %d), output lacks %q:\n%s", tc.args, code, tc.code, tc.want, out)
		}
	}
}
