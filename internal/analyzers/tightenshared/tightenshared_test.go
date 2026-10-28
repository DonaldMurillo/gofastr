package tightenshared_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/tightenshared"
)

func TestTightenShared(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), tightenshared.Analyzer, "a")
}
