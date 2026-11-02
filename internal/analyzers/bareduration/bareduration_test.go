package bareduration_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/bareduration"
)

func TestBareDuration(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), bareduration.Analyzer, "bd")
}
