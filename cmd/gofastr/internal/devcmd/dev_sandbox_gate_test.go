package devcmd

import (
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --no-a11y skips the accessibility lint only: a .ui.go that breaks the
// hydration sandbox still fails the rebuild.
func TestDevSandboxGateIgnoresNoA11y(t *testing.T) {
	root := clitest.WriteA11yTree(t, map[string]string{
		"app/screen.ui.go": "package app\n\nfunc render() { go func() {}() }\n",
	})
	var (
		mu  sync.Mutex
		cmd *exec.Cmd
	)
	out := &holdOutput{}
	var ok bool
	clitest.CovT_capOutput(t, func() { ok = buildGatesAndBinary(root, ".", nil, &mu, &cmd, true, out, nil) })
	if ok {
		t.Fatal("a .ui.go sandbox violation passed the rebuild under --no-a11y")
	}
	if !strings.Contains(out.String(), "screen.ui.go") {
		t.Errorf("the sandbox finding did not reach the hold output:\n%s", out.String())
	}
}
