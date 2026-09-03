package recovercallback_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/recovercallback"
)

func TestRecoverCallback(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), recovercallback.Analyzer,
		"peerish", "toolgate", "broker", "watcher",
		"ifacedisp", "fieldlocal", "nestedrec", "tickerloop", "helperguard")
}
