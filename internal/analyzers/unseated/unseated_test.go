package unseated_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/unseated"
)

func TestUnseated(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unseated.Analyzer, "us")
}
