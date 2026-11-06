package appgen

import (
	"os"
	"slices"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
)

// ReportGenerateFailure prints a generator failure using the requested
// output format without exiting, so watch mode can continue after a failure.
func ReportGenerateFailure(args []string, err error) {
	failGenerateArgs(args, err)
}

func failGenerateArgs(args []string, err error, hints ...string) {
	failGenerate(slices.Contains(args, "--json"), err, hints...)
}

func failGenerate(jsonMode bool, err error, hints ...string) {
	if jsonMode {
		printGeneratedErrorsJSON(err)
		cli.Fprintf(os.Stderr, "  ✗ %v\n", err)
		for _, hint := range hints {
			cli.Fprintln(os.Stderr, hint)
		}
		return
	}
	cli.Fail("%v", err)
	for _, hint := range hints {
		cli.Info("%s", hint)
	}
}
