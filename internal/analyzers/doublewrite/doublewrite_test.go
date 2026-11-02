package doublewrite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/doublewrite"
)

func TestDoubleWrite(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(doublewrite.Analyzer), "lib", "a")
}
