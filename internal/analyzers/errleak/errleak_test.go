package errleak_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/errleak"
)

func TestErrLeak(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, "b")
}

// TestErrLeakFormattedErrorOnFiveHundred pins the 2026-11-02 guard bash
// miss: the HTTP arm read only err.Error(), so the error formatted into
// the body (fmt.Sprintf("query failed: %v", err), fmt.Sprint(err)) went
// to the client on a 5xx with the rule silent.
func TestErrLeakFormattedErrorOnFiveHundred(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, "variants")
}
