package testcmd

import (
	"fmt"
)

func printTestUsage() {
	fmt.Println(`Usage: gofastr test [--run=<regex>] [--bench=<regex>] [--race] [--cover] [--short] [--timeout=<d>]

Runs the project's tests (go test ./...), colorizing the output.
GOFASTR_TEST=1 is set for the child process.

--run=<regex>     run only tests matching the regex
--bench=<regex>   also run matching benchmarks
--race            enable the race detector
--cover           report coverage
--short           set -short
--timeout=<d>     per-package timeout (e.g. --timeout=10m)`)
}
