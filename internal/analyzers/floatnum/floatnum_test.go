package floatnum_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/floatnum"
)

func TestFloatNum(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(floatnum.Analyzer), "a")
}
