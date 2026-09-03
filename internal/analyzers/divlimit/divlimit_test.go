package divlimit_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/divlimit"
)

func TestDivLimit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), divlimit.Analyzer, "a", "b", "c", "d")
}
