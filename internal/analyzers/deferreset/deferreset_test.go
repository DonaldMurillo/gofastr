package deferreset_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/deferreset"
)

func TestDeferReset(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), deferreset.Analyzer, "a")
}
