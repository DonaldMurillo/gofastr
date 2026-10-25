package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestHelpIncludesHarnessAndAgents verifies both harness and agents appear in --help.
func TestHelpIncludesHarnessAndAgents(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { dispatch([]string{"--help"}) })
	if !strings.Contains(out, "harness") {
		t.Fatalf("--help output missing 'harness'; got:\n%s", out)
	}
	if !strings.Contains(out, "agents") {
		t.Fatalf("--help output missing 'agents'; got:\n%s", out)
	}
}
