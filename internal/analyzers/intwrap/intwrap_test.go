package intwrap_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/intwrap"
)

func TestIntWrap(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), intwrap.Analyzer, "a", "b", "c")
}
