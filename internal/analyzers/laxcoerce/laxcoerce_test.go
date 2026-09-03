package laxcoerce_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/laxcoerce"
)

func TestLaxCoerce(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), laxcoerce.Analyzer, "a", "b", "c", "d")
}
