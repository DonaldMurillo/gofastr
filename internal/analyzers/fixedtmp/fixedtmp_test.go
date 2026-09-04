package fixedtmp_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/fixedtmp"
)

func TestFixedTmpFiresOnPredictableTempPaths(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fixedtmp.Analyzer, "a")
}
