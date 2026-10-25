package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestDispatchRoutesNonBlocking exercises every dispatch case that does
// not block on a serve loop. Each runs with args that terminate quickly
// (help/usage/unknown-subcommand paths), so we only assert dispatch
// routed without crashing the suite.
func TestDispatchRoutesNonBlocking(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)

	clitest.CovT_capStdout(t, func() { dispatch([]string{"theme"}) })
	clitest.CovT_capStdout(t, func() { dispatch([]string{"new", "-h"}) })
	clitest.CovT_capStdout(t, func() { dispatch([]string{"docs", "--help"}) })
	clitest.CovT_capStdout(t, func() { dispatch([]string{"semantic", "help"}) })

	// These dispatch to functions whose no-/bad-arg path, or a
	// removed-feature path (`generate entity`), calls osExit.
	for _, args := range [][]string{
		{"gen", "entity", "Foo", "name:string"},
		{"migrate", "bogus"},
		{"agents"},
		{"audit"},
	} {
		args := args
		_ = clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { dispatch(args) })
		})
	}
}
