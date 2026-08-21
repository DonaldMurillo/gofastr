package app

import (
	"strings"
	"testing"
)

// A WithSidebar + WithHeader shell renders <header role="banner"> as a direct
// child of the layout--has-sidebar wrapper, but LayoutBaseCSS only styles the
// CONTAINED layout's header. Without a rule for the sidebar shell the banner
// has no height, no background, and no bottom border: ui.SiteHeader is
// block-size: 100%, which resolves against a parent with no height, so the
// bar collapses to text height over the bare page background. (#187)
func TestSidebarShellHeaderHasBannerChrome(t *testing.T) {
	css := LayoutBaseCSS()
	if !strings.Contains(css, ".layout--has-sidebar > header") {
		t.Fatal("LayoutBaseCSS has no .layout--has-sidebar > header rule — a WithSidebar+WithHeader banner collapses to text height")
	}
	if !strings.Contains(css, "var(--ui-layout-header-height") {
		t.Fatal("sidebar header height must come from the --ui-layout-header-height token so hosts can override it without overriding the rule")
	}
}

// .layout-body is min-height: 100vh, so adding a banner makes every page
// taller than the viewport by exactly the header height, every screen
// scrolls with nothing in the overflow. The body under a sidebar-shell
// header must subtract the header height. (#187)
func TestSidebarHeaderOffsetsBodyMinHeight(t *testing.T) {
	css := LayoutBaseCSS()
	if !strings.Contains(css, ".layout--has-sidebar > header + .layout-body") ||
		!strings.Contains(css, "calc(100vh - var(--ui-layout-header-height") {
		t.Fatal("LayoutBaseCSS does not offset .layout-body min-height under the sidebar-shell header — every page scrolls by the header height")
	}
}
