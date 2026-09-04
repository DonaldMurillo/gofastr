package worldreadable_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/worldreadable"
)

func TestWorldReadableFiresOnStateSites(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), worldreadable.Analyzer, "a", "b")
}
