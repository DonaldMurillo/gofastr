package resolvedial_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/resolvedial"
)

func TestResolveDial(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(resolvedial.Analyzer), "a")
}
