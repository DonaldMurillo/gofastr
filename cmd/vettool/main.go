// vettool bundles the repo's custom go/analysis analyzers for
// `go vet -vettool`. This lane is for TYPE-AWARE invariants only: the
// pattern-shaped rules (bespoke CSS, hard navigation, bespoke
// EventSource, inline style/script) already live in the contracts
// pipeline (framework/contracts, `gofastr verify`) with its exemption
// system — don't duplicate them here.
//
// Run via `make analyze`, the pre-commit hook, and CI's vet step:
//
//	go build -o dist/vettool ./cmd/vettool
//	go vet -vettool=dist/vettool ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/mapwriter"
)

func main() {
	multichecker.Main(
		mapwriter.Analyzer,
	)
}
