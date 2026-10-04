package scan

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func selectorNote(sel ...string) *upgrade.Note {
	return &upgrade.Note{Find: upgrade.Find{CSS: upgrade.CSSMatch{Selectors: sel}}}
}

// A selector entry matches a prelude holding that compound in order,
// whatever whitespace surrounds its combinators, inside a selector list
// or a nested rule.
func TestCSSSelectorCombinator(t *testing.T) {
	css := `.cui-pos-center > .cui-slot {
	outline: none;
}
.a, .cui-pos-center>.cui-slot.x {
	color: red;
}
.board {
	.cui-pos-center   >   .cui-slot { color: blue; }
}
`
	n := selectorNote(".cui-pos-center > .cui-slot")
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	why := "css .cui-pos-center > .cui-slot"
	wantHits(t, res, n,
		hitAt(css, ".cui-pos-center > .cui-slot {", "static/app.style.css", why),
		hitAt(css, ".cui-pos-center>.cui-slot.x", "static/app.style.css", why),
		hitAt(css, ".cui-pos-center   >", "static/app.style.css", why),
	)
}

// The class alone, a longer class, another combinator, and the same
// text inside a declaration value are not the selector.
func TestCSSSelectorSilent(t *testing.T) {
	css := `.cui-pos-center > .cui-panel { color: red; }
.cui-pos-center > .cui-slot-bare { color: red; }
.cui-pos-center .cui-slot { color: red; }
.x::before { content: ".cui-pos-center>.cui-slot"; }
.y { color: red; }
`
	n := selectorNote(".cui-pos-center > .cui-slot")
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n)
}
