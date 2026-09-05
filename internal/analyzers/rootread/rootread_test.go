package rootread_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/rootread"
)

func TestRootRead(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), rootread.Analyzer, "a", "b")
}
