// vettool runs the analyzer suite (internal/analyzers/suite) as
// go vet -vettool; make analyze builds and runs it.
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/suite"
)

func main() {
	multichecker.Main(suite.Analyzers()...)
}
