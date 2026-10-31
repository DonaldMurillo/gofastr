package unseated_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/unseated"
)

func TestUnseated(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unseated.Analyzer, "us")
}

func TestUnrechecked(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unseated.RecheckAnalyzer, "rc")
}

func TestNoWriteDeadline(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unseated.WriteDeadlineAnalyzer, "wd")
}
