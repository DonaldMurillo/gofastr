package reqparamlimit_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/reqparamlimit"
)

func TestReqParamLimit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), reqparamlimit.Analyzer, "a", "n")
}
