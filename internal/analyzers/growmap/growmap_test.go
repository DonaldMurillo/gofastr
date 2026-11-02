package growmap_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/growmap"
)

func TestGrowMap(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(growmap.Analyzer), "gm", "gmfixed")
}
