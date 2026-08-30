package mapwriter_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/mapwriter"
)

// TestGateFlagsAllMapWriteSpellings pins the completeness side of the
// nondeterminism gate: whatever spelling a range-over-map write uses — the
// real fmt package behind an import alias, or ranging maps.Keys/maps.Values
// directly, which still iterates in map order — the diagnostic must fire.
// The want-comments in testdata/src/sec define the contract; each case there
// is a shape the current name-based sink matching and map-only range-source
// whitelist let through.
func TestGateFlagsAllMapWriteSpellings(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mapwriter.Analyzer, "sec")
}
