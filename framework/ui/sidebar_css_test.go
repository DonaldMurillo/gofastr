package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCollapsedSidebarLabelsStayAccessible(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	// The collapsed rail must hide labels with the visually-hidden clip
	// pattern, never display:none. Focusable links would lose their
	// accessible names (WCAG 4.1.2).
	start := strings.Index(css, `[data-collapsed="true"] .ui-sidebar__label`)
	if start == -1 {
		t.Fatal("no collapsed-state label rule found")
	}
	block := css[start:]
	if end := strings.Index(block, "}"); end != -1 {
		block = block[:end]
	}
	if strings.Contains(block, "display: none") {
		t.Fatalf("collapsed label rule uses display:none:\n%s", block)
	}
	if !strings.Contains(block, "clip: rect(0, 0, 0, 0)") {
		t.Fatalf("collapsed label rule should use the clip pattern:\n%s", block)
	}
}

func TestGroupSublistHiddenAttributeWins(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	// The sublist rule sets display:grid, which overrides the UA's
	// [hidden] { display: none } (author rule, same-or-higher
	// specificity). Without an explicit [hidden] win, a closed
	// button-dialect group keeps its links visible.
	start := strings.Index(css, `.ui-sidebar__sublist[hidden]`)
	if start == -1 {
		t.Fatal("no .ui-sidebar__sublist[hidden] rule found — closed groups render their links")
	}
	block := css[start:]
	if end := strings.Index(block, "}"); end != -1 {
		block = block[:end]
	}
	if !strings.Contains(block, "display: none") {
		t.Fatalf("sublist[hidden] rule must set display:none:\n%s", block)
	}
}

func TestAutoHideVariantCSSHook(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	// Class hook only: at >= md the hamburger hides like the other
	// inline variants; the reveal styling itself is host CSS keyed on
	// the variant class, deliberately absent here.
	if !strings.Contains(css, `.ui-sidebar--auto-hide .ui-sidebar__hamburger`) {
		t.Fatal("auto-hide variant must hide the hamburger at >= md like persistent/collapsible")
	}
	if strings.Contains(css, "ui-sidebar--auto-hide:hover") {
		t.Fatal("auto-hide must ship no hover-reveal CSS; the hover behaviour is host CSS against the hook")
	}
}
