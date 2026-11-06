package listheader_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/listheader"
)

func TestListHeader(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(listheader.Analyzer), "a")
}

func TestReviewWrappedHeaderValues(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(listheader.Analyzer), "review")
}
