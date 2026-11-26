package nilmapwrite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/nilmapwrite"
)

func TestNilMapWrite(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nilmapwrite.Analyzer, "a")
}
