package discardeddecode_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardeddecode"
)

func TestDiscardedDecodeFiresOnParseDrops(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardeddecode.Analyzer, "a")
}
