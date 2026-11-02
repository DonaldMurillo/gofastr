package gates

import (
	"bytes"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// The a11y gate's report names each finding's file, a path from the
// project, and `gofastr build` and `gofastr dev` write it to the
// terminal (dev through an io.MultiWriter on os.Stdout, which the raw
// print guard in package cli cannot see). A directory name carrying
// escapes reaches the terminal inert.
func TestA11yGateReportCarriesNoLiveEscapes(t *testing.T) {
	const dir = "app\x1b]52;c;cHduZWQ=\x07\x1b[2J"
	root := clitest.WriteA11yTree(t, map[string]string{
		dir + "/screen.go": clitest.BadScreen,
	})
	var stdout, stderr bytes.Buffer
	if BuildA11yGateTo(root, &stdout, &stderr) {
		t.Fatal("setup broken: the gate passed a screen with a finding")
	}
	out := stdout.String() + stderr.String()
	if !bytes.Contains([]byte(out), []byte("cHduZWQ=")) {
		t.Fatalf("setup broken: the report does not name the file:\n%q", out)
	}
	for i, r := range out {
		if r != '\n' && r != '\t' && textsafe.IsUnsafe(r) {
			t.Fatalf("SECURITY: [gate-report-escape] the a11y gate printed %U at byte %d live:\n%q", r, i, out)
		}
	}
}
