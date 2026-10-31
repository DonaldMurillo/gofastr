package testcmd

import (
	"fmt"
)

func printTestUsage() {
	fmt.Println(`Usage: gofastr test [flags] [packages]

Runs the project's tests (go test, ./... when no packages are named),
colorizing the output. GOFASTR_TEST=1 is set for the child process.
A flag takes its value as --run=X or --run X; an unknown flag is an
error.

--run=<regex>     run only tests matching the regex
--bench=<regex>   also run matching benchmarks
--race            enable the race detector
--cover           report coverage
--short           set -short
--timeout=<d>     per-package timeout (e.g. --timeout=10m)`)
}
