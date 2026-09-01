package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestMenuCSSHidesClosedPanel(t *testing.T) {
	css := menuCSS(style.Theme{})
	if !strings.Contains(css, `[data-fui-comp="ui-menu"]:not([open]) .ui-menu__panel { display: none; }`) {
		t.Fatal("menuCSS must hide closed panels explicitly: the author display:grid on .ui-menu__panel defeats the UA sheet's closed-details hiding, and role engines then prune the whole menu while details.open stays false")
	}
}
