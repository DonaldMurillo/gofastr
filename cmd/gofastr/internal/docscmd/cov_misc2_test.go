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

func TestRunDocsGrepShortFlag(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunDocs([]string{"-g", "architecture"}) })
	if out == "" {
		t.Fatal("expected grep output")
	}
}
