package laxenvelope_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/laxenvelope"
)

func TestLaxEnvelope(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), laxenvelope.Analyzer, "lx")
}
