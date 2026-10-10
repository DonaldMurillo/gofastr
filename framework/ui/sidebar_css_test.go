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
	start := strings.Index(css, `[data-collapsed="true"] .fui-sidebar__label`)
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
	start := strings.Index(css, `.fui-sidebar__sublist[hidden]`)
	if start == -1 {
		t.Fatal("no .fui-sidebar__sublist[hidden] rule found — closed groups render their links")
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
	// The hiding keys on the hamburger's own variant class so a
	// relocated trigger (SidebarDrawerTrigger in a page header) hides
	// at >= md too, not only the copy inside the sidebar root.
	if !strings.Contains(css, `.fui-sidebar__hamburger--auto-hide`) {
		t.Fatal("auto-hide variant must hide the hamburger at >= md like persistent/collapsible")
	}
	// The reveal ships in the component stylesheet (one styling
	// surface: hosts write zero CSS), and it must key on BOTH :hover
	// and :focus-within. A hover-only reveal is an accessibility
	// defect: every link in the rail is unreachable by keyboard.
	if !strings.Contains(css, `.fui-sidebar--auto-hide:hover .fui-sidebar__inline`) {
		t.Fatal("auto-hide must ship a :hover reveal rule in the component stylesheet")
	}
	sel := `.fui-sidebar--auto-hide:focus-within .fui-sidebar__inline`
	start := strings.Index(css, sel)
	if start == -1 {
		t.Fatal("auto-hide must ship a :focus-within reveal rule — hover-only hides every link from keyboard users")
	}
	// Selector text is not the rule. The reveal shipped once with its
	// selector list ending in a comma and no opening brace; the browser
	// dropped it and the rail never widened while this test stayed
	// green. The selector must be followed by " {" and a block that
	// restores the persistent column width.
	block := css[start+len(sel):]
	if !strings.HasPrefix(block, " {") {
		t.Fatalf("reveal selector list must open its declaration block, got: %.40q", block)
	}
	if end := strings.Index(block, "}"); end != -1 {
		block = block[:end]
	}
	if !strings.Contains(block, "width: var(--ui-sidebar-width, 220px)") {
		t.Fatalf("reveal rule must restore the 220px column:\n%s", block)
	}
}

// TestCalloutHiddenAttributeWins: the callout rule sets display:grid,
// which outranks the UA's [hidden] { display: none }. Without an
// explicit [hidden] win a server-rendered, initially hidden notice
// (the rtc-call example's) shows as an empty warning bar on every
// page load.
func TestCalloutHiddenAttributeWins(t *testing.T) {
	css := calloutCSS(style.Theme{})
	start := strings.Index(css, `[data-cui-comp="ui-callout"][hidden]`)
	if start == -1 {
		t.Fatal(`no [data-cui-comp="ui-callout"][hidden] rule found: a hidden callout renders`)
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
// declare display:none, so a malformed or emptied rule fails here. The
// collapsed rail spares a Prepend that is only a SidebarBrand
// (TestCollapsedRailKeepsBrandTile draws both).
func TestSidebarPrependHidesWithTitleAndFooter(t *testing.T) {
	css := sidebarCSS(style.Theme{})
	for _, state := range []string{
		`[data-collapsed="true"] .fui-sidebar__prepend:not(:has(> [data-cui-comp="ui-sidebar-brand"]:only-child)),`,
		`.fui-sidebar--auto-hide:not(:hover):not(:focus-within) .fui-sidebar__prepend,`,
	} {
		start := strings.Index(css, state)
		if start == -1 {
			t.Errorf("sidebar CSS must hide .fui-sidebar__prepend in state %q", state)
			continue
		}
		block := css[start:]
		if end := strings.Index(block, "}"); end != -1 {
			block = block[:end]
		}
		if !strings.Contains(block, "display: none") {
			t.Errorf("the rule hiding .fui-sidebar__prepend in state %q must set display:none:\n%s", state, block)
		}
	}
}

// A <button> takes the UA's font (Arial) rather than the page's unless
// its rule sets font: inherit, as ui-button's does. The drawer's close
// button and the theme toggle's buttons rendered in Arial beside text
// set in the app's font.
func TestDrawerCloseAndToggleInheritFont(t *testing.T) {
	for name, tc := range map[string]struct{ css, selector string }{
		"drawer close": {sidebarCSS(style.Theme{}), ".fui-sidebar__drawer-close {"},
		"theme toggle": {themeToggleCSS(style.Theme{}), `:where(button)[data-cui-comp="ui-theme-toggle"] {`},
	} {
		start := strings.Index(tc.css, tc.selector)
		if start == -1 {
			t.Fatalf("%s: no %s rule found", name, tc.selector)
		}
		block := tc.css[start:]
		if end := strings.Index(block, "}"); end != -1 {
			block = block[:end]
		}
		if !strings.Contains(block, "font: inherit;") {
			t.Errorf("%s button rule must set font: inherit:\n%s", name, block)
		}
	}
}

// RaisedCurrent draws the current page's link as a raised pill: the
// surface, a hairline ring and a small shadow, not a grey fill.
func TestSidebarRaisedCurrent(t *testing.T) {
	items := []SidebarItem{{Label: "Home", Href: "/"}}
	if out := string(sidebarComponent{cfg: SidebarConfig{RaisedCurrent: true, Variant: SidebarPersistent, Items: items}}.Render()); !strings.Contains(out, "fui-sidebar--raised-current") {
		t.Errorf("no raised-current modifier:\n%s", out)
	}
	if out := string(sidebarComponent{cfg: SidebarConfig{Variant: SidebarPersistent, Items: items}}.Render()); strings.Contains(out, "fui-sidebar--raised-current") {
		t.Error("the modifier rides a sidebar that did not ask for it")
	}
	want := `[data-cui-comp="ui-sidebar"].fui-sidebar--raised-current .fui-sidebar__link[aria-current="page"] {
  background: var(--color-surface);
  box-shadow: var(--shadow-xs), 0 0 0 var(--stroke-thin, 1px) var(--color-border);`
	if css := sidebarCSS(style.Theme{}); !strings.Contains(css, want) {
		t.Errorf("the raised current link rule is missing:\n%s", css)
	}
}
