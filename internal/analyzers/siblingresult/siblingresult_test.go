package siblingresult_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/siblingresult"
)

// TestSiblingResult runs the unwrapped analyzer (allow.Guard is what
// drops the stepUp fixture's marked site in the vettool).
func TestSiblingResult(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), siblingresult.Analyzer, "a", "example.com/lib")
}
