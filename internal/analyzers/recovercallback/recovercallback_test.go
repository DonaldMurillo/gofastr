package recovercallback_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/recovercallback"
)

// The module flag exists for this run only: analysistest's loader
// reports an empty module path, so the fixture module (example.app,
// standing in for github.com/DonaldMurillo/gofastr) is passed
// explicitly. go vet reads the real module from the pass.
func TestRecoverCallback(t *testing.T) {
	if err := recovercallback.Analyzer.Flags.Set("module", "example.app"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), recovercallback.Analyzer,
		"peerish", "toolgate", "broker", "watcher",
		"ifacedisp", "fieldlocal", "nestedrec", "tickerloop", "helperguard",
		"leaderish", "bridgeish", "lifecycleish", "plumbingish",
		"example.app/fanoutish", "example.org/depish")
}
