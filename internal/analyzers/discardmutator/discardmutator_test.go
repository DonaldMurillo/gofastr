package discardmutator_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardmutator"
)

func TestDiscardmutator(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardmutator.Analyzer, "a", "n")
}
