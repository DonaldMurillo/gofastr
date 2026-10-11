package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// A chip dropped into a flex column (a Card header, a Stack) stretched
// to the column's full width, because a flex item stretches unless its
// width is definite. fit-content keeps it chip-sized in a row or a
// column alike.
func TestChipsKeepTheirWidth(t *testing.T) {
	thm := style.DefaultTheme()
	for _, name := range []string{"ui-tag", "ui-badge"} {
		var css string
		for _, e := range registry.All() {
			if e.Name == name {
				css = e.CSSFor(thm)
			}
		}
		root := `[data-cui-comp="` + name + `"] {`
		i := strings.Index(css, root)
		if i < 0 {
			t.Fatalf("%s: no root rule", name)
		}
		rule := css[i : i+strings.Index(css[i:], "}")]
		if !strings.Contains(rule, "inline-size: fit-content") {
			t.Errorf("%s root rule has no inline-size: fit-content, so it stretches in a flex column:\n%s", name, rule)
		}
	}
}
