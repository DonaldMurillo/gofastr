package allow_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardeddecode"
)

// Pins: a suppression marker must be the first thing in its comment —
// start-anchored — so prose or documentation merely MENTIONING a marker
// never disables an analyzer. Found 2026-09-06 adversarial round 5,
// fixed 2026-09-07 by anchoring the marker regex in allow.go the way
// suppress.go:35 anchors the contracts directives.
// The hole: allow.go's marker regex was unanchored
// (`//gofastr:allow\(...\)(.*)$`), and collect's FindStringSubmatch
// matched the marker mid-prose; the next-line rule then spread the
// suppression onto the following trigger line — verified live with the
// built vettool, a file containing `// The
// //gofastr:allow(worldreadable) marker is documented in allow.go.`
// above an os.WriteFile(..., 0666) suppressed worldreadable (only the
// named analyzer; adjacent fixedtmp still fired). Trailing prose
// counted as the "reason". Its anchored twin suppress.go:35 never had
// the hole.
//
// Fix applied: the marker regex is now anchored to the start of the
// comment token, exactly the suppress.go grammar.

func TestAllowProseMentionKept(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(discardeddecode.Analyzer), "prose")
}
