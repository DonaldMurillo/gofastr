package docscmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestRunDocsNoArgsLists(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunDocs(nil) })
	if !strings.Contains(out, "framework docs") {
		t.Fatalf("docs list: %s", out)
	}
}

// `gofastr docs --grep=migrate` read the whole flag as a topic name and
// failed with `topic "--grep=migrate" not found`; the = spelling every
// other gofastr command takes searches like the space one.
func TestRunDocsGrepEqualsSpelling(t *testing.T) {
	for _, flag := range []string{"--grep=architecture", "-g=architecture"} {
		code := clitest.CovT_capExit(t, func() {
			out := clitest.CovT_capStdout(t, func() { RunDocs([]string{flag}) })
			if !strings.Contains(out, "for \"architecture\"") {
				t.Errorf("docs %s did not search: %s", flag, out)
			}
		})
		if code != -1 {
			t.Errorf("docs %s exited %d", flag, code)
		}
	}
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep="}) })
	})
	if code != 2 {
		t.Errorf("docs --grep= exited %d, want 2 (usage)", code)
	}
}

func TestRunDocsGrepShortFlag(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunDocs([]string{"-g", "architecture"}) })
	if out == "" {
		t.Fatal("expected grep output")
	}
}
