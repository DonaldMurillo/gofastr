package credfetch_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/credfetch"
)

func TestCredFetch(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), credfetch.Analyzer, "a", "b", "c", "d")
}
