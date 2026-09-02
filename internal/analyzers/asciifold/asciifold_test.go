package asciifold_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/asciifold"
)

func TestASCIIFold(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), asciifold.Analyzer, "af", "afalt")
}
