package rsakeysize_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/rsakeysize"
)

func TestRSAKeySize(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), rsakeysize.Analyzer, "a")
}
