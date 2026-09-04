package allow_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardeddecode"
)

// TestGuardDropsMarkedLinesOnly runs a real analyzer through Guard: the
// fixture has an unmarked site (reported), a trailing marker with a
// reason (dropped), a stand-alone marker above a site (dropped), a bare
// marker with no reason (still reported), and a marker naming another
// analyzer (still reported).
func TestGuardDropsMarkedLinesOnly(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(discardeddecode.Analyzer), "a")
}
