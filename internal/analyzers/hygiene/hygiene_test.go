package hygiene_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/hygiene"
)

func TestEmptyErrBranch(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hygiene.EmptyErrBranchAnalyzer, "errb")
}

func TestClientTimeout(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hygiene.ClientTimeoutAnalyzer, "clientp", "clientctx")
}
