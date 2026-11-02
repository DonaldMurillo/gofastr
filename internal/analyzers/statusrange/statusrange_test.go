package statusrange_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/statusrange"
)

func TestStatusRange(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(statusrange.Analyzer), "lib", "a")
}
