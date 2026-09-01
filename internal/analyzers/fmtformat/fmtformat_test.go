package fmtformat_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/fmtformat"
)

func TestFmtFormat(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fmtformat.Analyzer, "a", "n")
}
