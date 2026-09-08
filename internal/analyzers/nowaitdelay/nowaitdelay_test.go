package nowaitdelay_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/nowaitdelay"
)

func TestNoWaitDelay(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nowaitdelay.Analyzer, "a")
}
