package errleak_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/errleak"
)

func TestErrLeak(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), errleak.Analyzer, "b")
}
