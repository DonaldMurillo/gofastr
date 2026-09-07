package ui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// Every component stylesheet is a Go string literal nobody parses before
// the browser does, and the browser drops a malformed rule without a
// word. The sidebar's auto-hide reveal rule shipped with its selector
// list ending in a comma and no opening brace; the rail never widened
// and every selector-text test stayed green. This walks every registered
// stylesheet and rejects the two shapes that bug takes: a brace depth
// that goes negative or does not return to zero, and a declaration (`;`)
// sitting where a selector list belongs.
func TestRegisteredStylesheetsAreWellFormed(t *testing.T) {
	th := theme.Default()
	entries := registry.All()
	if len(entries) == 0 {
		t.Fatal("no registered stylesheets: the walk is vacuous")
	}
	for _, e := range entries {
		css := cssComment.ReplaceAllString(e.CSSFor(th), "")
		depth := 0
		selectorStart := 0
		for i, c := range css {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
				if depth < 0 {
					t.Errorf("%s: unbalanced '}' at byte %d (a rule before it has no opening brace):\n%s", e.Name, i, excerpt(css, i))
					depth = 0
				}
				selectorStart = i + 1
			case ';':
				if depth == 0 {
					sel := strings.TrimSpace(css[selectorStart:i])
					// @import/@charset/@namespace end in ';' at depth 0 and are
					// legitimate; a component sheet has no business shipping
					// them, but do not fail on the syntax itself.
					if !strings.HasPrefix(sel, "@") {
						t.Errorf("%s: declaration outside any rule block at byte %d (selector list missing its '{'):\n%s", e.Name, i, excerpt(css, i))
					}
					selectorStart = i + 1
				}
			}
		}
		if depth != 0 {
			t.Errorf("%s: brace depth ends at %d, expected 0", e.Name, depth)
		}
	}
}

func excerpt(css string, at int) string {
	lo, hi := at-160, at+40
	if lo < 0 {
		lo = 0
	}
	if hi > len(css) {
		hi = len(css)
	}
	return css[lo:hi]
}
