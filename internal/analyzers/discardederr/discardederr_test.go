package discardederr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardederr"
)

func TestDiscardedErr(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardederr.Analyzer, "a", "b", "c")
}
