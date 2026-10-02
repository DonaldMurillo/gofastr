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
	css := `.fui-pos-center > .fui-slot {
	outline: none;
}
.a, .fui-pos-center>.fui-slot.x {
	color: red;
}
.board {
	.fui-pos-center   >   .fui-slot { color: blue; }
}
`
	n := selectorNote(".fui-pos-center > .fui-slot")
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	why := "css .fui-pos-center > .fui-slot"
	wantHits(t, res, n,
		hitAt(css, ".fui-pos-center > .fui-slot {", "static/app.style.css", why),
		hitAt(css, ".fui-pos-center>.fui-slot.x", "static/app.style.css", why),
		hitAt(css, ".fui-pos-center   >", "static/app.style.css", why),
	)
}

// The class alone, a longer class, another combinator, and the same
// text inside a declaration value are not the selector.
func TestCSSSelectorSilent(t *testing.T) {
	css := `.fui-pos-center > .fui-panel { color: red; }
.fui-pos-center > .fui-slot-bare { color: red; }
.fui-pos-center .fui-slot { color: red; }
.x::before { content: ".fui-pos-center>.fui-slot"; }
.y { color: red; }
`
	n := selectorNote(".fui-pos-center > .fui-slot")
	res := mustRun(t, cssWorkspace(t, map[string]string{"static/app.style.css": css}), n)
	wantHits(t, res, n)
}
