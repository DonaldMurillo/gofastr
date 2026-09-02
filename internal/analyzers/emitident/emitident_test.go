package emitident_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/emitident"
)

func TestEmitIdent(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), emitident.Analyzer, "ei", "eifix")
}
