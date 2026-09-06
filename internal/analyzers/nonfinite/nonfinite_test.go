package nonfinite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/nonfinite"
)

func TestNonFinite(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nonfinite.Analyzer, "a")
}
