package negdur_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/negdur"
)

func TestNegDur(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), negdur.Analyzer, "nd", "ndfix")
}
