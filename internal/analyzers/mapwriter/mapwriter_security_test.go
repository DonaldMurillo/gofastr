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

// TestGateFlagsSinksWithoutSelectorSyntax pins that the diagnostic does
// not depend on the sink appearing as a selector call. Binding the sink
// to a variable first (method value or package function) or dot-importing
// fmt leaves the call's Fun an *ast.Ident, which the selector-only match
// never inspects — the ordered write ships undiagnosed.
func TestGateFlagsSinksWithoutSelectorSyntax(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mapwriter.Analyzer, "sinkmatch")
}

// TestGateFlagsHiddenMapOrderRangeSources pins that recognizing a
// map-ordered range cannot stop at the direct `range m` /
// `range maps.Keys(m)` spellings. The iterator bound to a variable and
// slices.Collect(maps.Keys(m)) — collect without sort — still walk the
// map, but reach the range statement as an intermediate expression the
// source check does not resolve.
func TestGateFlagsHiddenMapOrderRangeSources(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mapwriter.Analyzer, "iterhide")
}

// TestGuardExemptionResolvesIdentifiers pins that the `if len(m) == 1`
// exemption binds the guarded and ranged expressions as the same
// VARIABLE. The check compares their printed source text, so a shadowing
// declaration inside the guard keeps the text identical while the ranged
// map is a different, unbounded one.
func TestGuardExemptionResolvesIdentifiers(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mapwriter.Analyzer, "guardmatch")
}
