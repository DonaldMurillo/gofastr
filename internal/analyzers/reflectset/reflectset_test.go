package reflectset_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/reflectset"
)

func TestReflectSet(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), reflectset.Analyzer, "a", "b", "c", "d")
}
