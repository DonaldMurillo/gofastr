package testgap_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/testgap"
)

func TestTestGap(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), testgap.Analyzer, "a", "n")
}
