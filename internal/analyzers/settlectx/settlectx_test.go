package settlectx_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/settlectx"
)

func TestSettleCtx(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), settlectx.Analyzer, "a", "lib")
}
