// vettool bundles the repo's custom go/analysis analyzers for
// `go vet -vettool`. This lane is for TYPE-AWARE invariants only: the
// pattern-shaped rules (bespoke CSS, hard navigation, bespoke
// EventSource, inline style/script) already live in the contracts
// pipeline (framework/contracts, `gofastr verify`) with its exemption
// system — don't duplicate them here.
//
// Two kinds of analyzer ship here. The gofastr* ones encode invariants
// this codebase learned the hard way, each traceable to fixes that had to
// be made more than once. The rest are stock x/tools passes that `go vet`
// does not enable by default; x/tools is already a direct dependency, so
// they cost no new module and satisfy the Go-team-only tooling rule.
//
// Run via `make analyze`, the pre-commit hook, and CI's vet step:
//
//	go build -o dist/vettool ./cmd/vettool
//	go vet -vettool=dist/vettool ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"
	"golang.org/x/tools/go/analysis/passes/atomicalign"
	"golang.org/x/tools/go/analysis/passes/deepequalerrors"
	"golang.org/x/tools/go/analysis/passes/hostport"
	"golang.org/x/tools/go/analysis/passes/nilness"
	"golang.org/x/tools/go/analysis/passes/reflectvaluecompare"
	"golang.org/x/tools/go/analysis/passes/scannererr"
	"golang.org/x/tools/go/analysis/passes/sortslice"
	"golang.org/x/tools/go/analysis/passes/sqlrowserr"
	"golang.org/x/tools/go/analysis/passes/unusedwrite"
	"golang.org/x/tools/go/analysis/passes/waitgroup"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/errleak"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/fieldtypeswitch"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/hygiene"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/mapwriter"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/unboundedbody"
)

func main() {
	multichecker.Main(
		// Repo invariants, each born from a bug that recurred.
		mapwriter.Analyzer,
		unboundedbody.Analyzer,
		errleak.Analyzer,
		fieldtypeswitch.Analyzer,

		// Checks that currently find nothing. They cost no cleanup and
		// hold classes this repo already drove to zero. A hit here means
		// something regressed, not that the rule is noisy.
		hygiene.EmptyErrBranchAnalyzer,
		hygiene.ClientTimeoutAnalyzer,

		// Stock x/tools passes outside the default `go vet` set. x/tools
		// is already a direct dependency, so these cost no new module.
		//
		// nilness, sqlrowserr and scannererr arrived with findings rather
		// than clean, and clearing them was the point: a partial-read bug
		// in the pgvector chunk purge, a vacuous assertion that could
		// never fail, an MCP client that stranded every in-flight call on
		// a dead transport, and a `gofastr test` tally printed from
		// truncated output. The rest were clean on arrival and are
		// regression insurance.
		//
		// Deliberately NOT enabled: shadow and fieldalignment, measured
		// at 1816 findings between them — a gate nobody can keep green
		// teaches people to ignore the gate.
		atomicalign.Analyzer,
		deepequalerrors.Analyzer,
		hostport.Analyzer,
		nilness.Analyzer,
		reflectvaluecompare.Analyzer,
		scannererr.Analyzer,
		sortslice.Analyzer,
		sqlrowserr.Analyzer,
		unusedwrite.Analyzer,
		waitgroup.Analyzer,
	)
}
