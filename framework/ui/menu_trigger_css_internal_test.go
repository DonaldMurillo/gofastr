package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// TestMenuTriggerCSSHidesClosedPanel: the trigger-element path's root
// is a div, so menuCSS's root-keyed `:not([open])` hide rule can never
// match it — the closed panel is hidden through the summary-less
// <details data-fui-menu> child instead, for the same
// author-display:grid-defeats-the-UA-sheet reason as the summary path
// (role engines prune every menuitem while details.open stays false).
func TestMenuTriggerCSSHidesClosedPanel(t *testing.T) {
	css := menuCSS(style.Theme{})
	if !strings.Contains(css, `[data-fui-comp="ui-menu"] > details[data-fui-menu]:not([open]) .ui-menu__panel { display: none; }`) {
		t.Fatal("menuCSS must hide the trigger path's closed panels through the details child: the root div never carries [open], and the author display:grid on .ui-menu__panel would defeat the UA sheet's closed-details hiding")
	}
}

// TestMenuTriggerCSSWrapperIsBoxless: the presentation wrapper must
// generate no box (display: contents), so the caller's element is laid
// out as a direct child of the root and host CSS written against the
// element itself (header button.rounded-full) keeps working.
func TestMenuTriggerCSSWrapperIsBoxless(t *testing.T) {
	css := menuCSS(style.Theme{})
	if !strings.Contains(css, `[data-fui-comp="ui-menu"] > [data-fui-menu-trigger] { display: contents; }`) {
		t.Fatal("menuCSS must keep the trigger wrapper box-less (display: contents): a wrapper box would sit between the host's layout and the caller's element")
	}
}
