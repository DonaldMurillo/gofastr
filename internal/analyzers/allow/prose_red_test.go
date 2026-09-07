//go:build red

package allow_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardeddecode"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: a suppression directive must be the first thing in its comment
// (start-anchored), so prose or documentation merely MENTIONING a marker
// never disables an analyzer.
// Surfaces: internal/analyzers/allow/allow.go marker regex (line 34,
// unanchored `//gofastr:allow\(...\)(.*)$`) and collect (:74), whose
// FindStringSubmatch matches the marker mid-prose; the next-line rule
// (:83-85) then spreads the suppression onto the following trigger line.
// Finding: verified live with the built vettool — a dependency or
// CI-contributed file containing `// The //gofastr:allow(worldreadable)
// marker is documented in allow.go.` above an os.WriteFile(..., 0666)
// suppresses worldreadable (only the named analyzer; adjacent fixedtmp
// still fires). Trailing prose counts as the "reason". Its anchored twin
// framework/contracts/suppress.go:35 `^(?:/\*|//|#)\s*gofastr:allow...`
// does not have this hole.
// Fix direction: anchor the allow.go marker regex to the start of the
// comment token the same way suppress.go:35 does.

func TestAllowRedProseMentionKept(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(discardeddecode.Analyzer), "prose")
}
