package optzero_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/optzero"
)

func TestOptZero(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), optzero.Analyzer, "oz")
}
