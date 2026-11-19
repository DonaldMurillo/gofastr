package gates

import (
	"io"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// lintGate is the build gates' shared skeleton: run the audit, report
// its error to stderr under the gate's label, print the formatted
// report when it has findings.
func lintGate[F any](root string, stdout, stderr io.Writer, label string, audit func(string) ([]F, error), format func([]F) string) bool {
	findings, err := audit(root)
	if err != nil {
		cli.Fprintf(stderr, "%s: %v\n", label, err)
		return false
	}
	if len(findings) == 0 {
		return true
	}
	// The report names project files: scrubbed, as every CLI print is.
	cli.Fprintf(stdout, "%s", format(findings))
	return false
}
