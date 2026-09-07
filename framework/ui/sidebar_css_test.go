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

func TestAutoHideVariantShipsRevealCSS(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	if !strings.Contains(css, `.ui-sidebar--auto-hide .ui-sidebar__hamburger`) {
		t.Fatal("auto-hide variant must hide the hamburger at >= md like persistent/collapsible")
	}
	// The reveal ships in the component stylesheet (one styling
	// surface: hosts write zero CSS), and it must key on BOTH :hover
	// and :focus-within. A hover-only reveal is an accessibility
	// defect: every link in the rail is unreachable by keyboard.
	if !strings.Contains(css, `.ui-sidebar--auto-hide:hover .ui-sidebar__inline`) {
		t.Fatal("auto-hide must ship a :hover reveal rule in the component stylesheet")
	}
	if !strings.Contains(css, `.ui-sidebar--auto-hide:focus-within .ui-sidebar__inline`) {
		t.Fatal("auto-hide must ship a :focus-within reveal rule — hover-only hides every link from keyboard users")
	}
}

// TestCalloutHiddenAttributeWins: the callout rule sets display:grid,
// which outranks the UA's [hidden] { display: none }. Without an
// explicit [hidden] win a server-rendered, initially hidden notice
// (the rtc-call example's) shows as an empty warning bar on every
// page load.
func TestCalloutHiddenAttributeWins(t *testing.T) {
	css := calloutCSS(style.Theme{})
	start := strings.Index(css, `[data-fui-comp="ui-callout"][hidden]`)
	if start == -1 {
		t.Fatal(`no [data-fui-comp="ui-callout"][hidden] rule found: a hidden callout renders`)
	}
	block := css[start:]
	if end := strings.Index(block, "}"); end != -1 {
		block = block[:end]
	}
	if !strings.Contains(block, "display: none") {
		t.Fatalf("callout[hidden] rule must set display:none:\n%s", block)
	}
}

// The collapsed rail and the auto-hide rest state hide the title and
// footer; Prepend is chrome of the same kind and must hide with them,
// or a section <select> would poke out of a 64px rail (#405). The
// selector alone is not the guard: the rule it belongs to has to
// declare display:none, so a malformed or emptied rule fails here.
func TestSidebarPrependHidesWithTitleAndFooter(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	for _, state := range []string{
		`[data-collapsed="true"] .ui-sidebar__prepend,`,
		`.ui-sidebar--auto-hide:not(:hover):not(:focus-within) .ui-sidebar__prepend,`,
	} {
		start := strings.Index(css, state)
		if start == -1 {
			t.Errorf("sidebar CSS must hide .ui-sidebar__prepend in state %q", state)
			continue
		}
		block := css[start:]
		if end := strings.Index(block, "}"); end != -1 {
			block = block[:end]
		}
		if !strings.Contains(block, "display: none") {
			t.Errorf("the rule hiding .ui-sidebar__prepend in state %q must set display:none:\n%s", state, block)
		}
	}
}
