package errleak_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/errleak"
)

// TestErrLeak runs every fixture package. The later ones pin misses:
//   - variants: the 2026-11-02 guard bash. The HTTP arm read only
//     err.Error(), so the error formatted into the body
//     (fmt.Sprintf("query failed: %v", err), fmt.Sprint(err)) went to the
//     client on a 5xx with the rule silent.
//   - decode: 1a4d9e2a. encoding/json's own text, naming the server's Go
//     types, sent to a client at any status.
//   - rewrap: d01462e8a. A typed error rebuilt from its own text loses its
//     code.
//   - selfcall: the review's recursion crash. A func var re-bound to a
//     literal that calls the var itself re-entered litReturns with no
//     busy guard and stack-overflowed, taking go vet down with it.
func TestErrLeak(t *testing.T) {
	for _, pkg := range []string{"b", "variants", "decode", "rewrap", "selfcall"} {
		t.Run(pkg, func(t *testing.T) {
			analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, pkg)
		})
	}
}
