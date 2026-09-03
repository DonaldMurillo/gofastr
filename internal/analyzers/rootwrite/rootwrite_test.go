package rootwrite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/rootwrite"
)

func TestRootWrite(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), rootwrite.Analyzer, "a", "b", "c", "d")
}
