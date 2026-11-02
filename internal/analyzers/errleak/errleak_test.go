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

// TestErrLeakDecodeText pins the 1a4d9e2a arm: encoding/json's own text,
// naming the server's Go types, sent to a client at any status.
func TestErrLeakDecodeText(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, "decode")
}

// TestErrLeakRewrap pins the d01462e8a arm: a typed error rebuilt from
// its own text loses its code.
func TestErrLeakRewrap(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, "rewrap")
}
