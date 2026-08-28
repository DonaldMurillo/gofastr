package mapwriter_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/mapwriter"
)

func TestMapWriter(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mapwriter.Analyzer, "a")
}
