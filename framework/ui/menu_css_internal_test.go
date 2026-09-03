package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestMenuCSSHidesClosedPanel(t *testing.T) {
	css := menuCSS(style.Theme{})
	if !strings.Contains(css, `details[data-fui-comp="ui-menu"]:not([open]) .ui-menu__panel { display: none; }`) {
		t.Fatal("menuCSS must hide closed panels explicitly through a details-typed root: the author display:grid on .ui-menu__panel defeats the UA sheet's closed-details hiding, and the TriggerElement path's root is a div that never carries [open], so an untyped root-keyed rule would hide the panel unconditionally (#386)")
	}
}
