// vettool runs GoFastr's analyzer suite (internal/analyzers/suite) for
// `go vet -vettool` over this repo. Run via `make analyze`, the
// pre-commit hook, and CI's vet step:
//
//	go build -o dist/vettool ./cmd/vettool
//	go vet -vettool=dist/vettool ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/suite"
)

func main() {
	multichecker.Main(suite.Analyzers()...)
}
