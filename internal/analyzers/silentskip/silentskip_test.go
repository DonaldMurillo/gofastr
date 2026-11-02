package silentskip_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/silentskip"
)

func TestSilentSkip(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), silentskip.Analyzer, "a")
}
