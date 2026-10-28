package wrapwriter_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/wrapwriter"
)

func TestWrapWriter(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(wrapwriter.Analyzer), "a")
}
