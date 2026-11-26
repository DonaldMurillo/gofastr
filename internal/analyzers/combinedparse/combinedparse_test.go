package combinedparse_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/combinedparse"
)

func TestCombinedParse(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), combinedparse.Analyzer, "a")
}
