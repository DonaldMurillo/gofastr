package desktopui_test

import (
	"context"
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout returns a core-ui layout named "desktop": the same Layout
// every screen registers with, so SPA navigation, layer keys, and the
// one-<main> rule all hold.
func TestLayoutIsCoreUILayout(t *testing.T) {
	l := desktopui.Layout(sidebarComp{})
	if l == nil {
		t.Fatal("Layout returned nil")
	}
	if l.Name != "desktop" || desktopui.LayoutName != "desktop" {
		t.Errorf("layout name = %q, LayoutName = %q, want desktop", l.Name, desktopui.LayoutName)
	}
	var _ *appui.Layout = l
}

// Wrapping a screen through the layout emits the frame: the sidebar
// <nav> landmark around the sidebar component, then the content
// <main>, both inside .desktopui-frame.
func TestLayoutRendersFrame(t *testing.T) {
	out := string(desktopui.Layout(sidebarComp{}).WrapCtx(context.Background(), plain("<p>content</p>")))
	for _, w := range []string{
		"layout-desktop",
		`data-fui-layout="desktop"`,
		`class="desktopui-frame"`,
		`aria-label="Sidebar"`,
		"desktopui-frame__sidebar",
		`desktopui-sourcelist">nav`,
		`id="main-content"`,
		"<p>content</p>",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("desktop layout missing %q:\n%s", w, out)
		}
	}
	if strings.Index(out, "desktopui-frame__sidebar") > strings.Index(out, "<main") {
		t.Errorf("sidebar must precede main:\n%s", out)
	}
	if strings.Contains(out, "style=") {
		t.Errorf("layout must not emit an inline style:\n%s", out)
	}
}

// A nil sidebar renders the content column alone: no empty nav
// landmark.
func TestLayoutNilSidebarHasNoNav(t *testing.T) {
	out := string(desktopui.Layout(nil).WrapCtx(context.Background(), plain("<p>x</p>")))
	if strings.Contains(out, "<nav") {
		t.Errorf("nil sidebar emitted a nav:\n%s", out)
	}
	if strings.Count(out, "<main") != 1 {
		t.Errorf("want one <main>:\n%s", out)
	}
}

// The layout stylesheet places the zones: transparent html/body so
// the native material shows through, the sidebar at the declared
// width with the measured traffic-light zone reserved at its top, and
// an opaque content column.
func TestLayoutCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	for _, w := range []string{
		"html:has(.desktopui-frame), body:has(.desktopui-frame) { background-color: transparent; }",
		"--desktop-sidebar-width: 220px;",
		"--desktop-sidebar-top-inset: 52px;",
		"flex: 0 0 var(--desktop-sidebar-width",
		"padding-top: var(--desktop-sidebar-top-inset",
		"background: var(--desktop-sidebar-surface, transparent)",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("layout CSS missing %q:\n%s", w, css)
		}
	}
	if got := cssProperty(t, css, ".desktopui-frame > .layout-content", "background-color"); !strings.HasPrefix(got, "var(--color-background") {
		t.Errorf("content column background = %q, want the theme background", got)
	}
}

// The layout sheet is LoadAlways: the transparent-html rule must be in
// the first-paint bundle, not fetched after hydration, or the window
// flashes an opaque page before the link lands.
func TestLayoutStyleIsEager(t *testing.T) {
	e, ok := lookup(t, "desktopui-layout")
	if !ok {
		t.Fatal("desktopui-layout not registered")
	}
	if e.Load != registry.LoadAlways {
		t.Errorf("desktopui-layout load mode = %d, want LoadAlways(%d)", e.Load, registry.LoadAlways)
	}
}

// SidebarTopInset is measured, not sourced: the Notes capture
// (light-active-notes.png) puts the first sidebar row's box top 51.5
// pt below the window's top edge, with the traffic lights ending at
// 32.5 pt; the layout rounds the reservation to 52.
func TestSidebarTopInsetMeasured(t *testing.T) {
	if desktopui.SidebarTopInset != 52 {
		t.Errorf("SidebarTopInset = %d, want 52", desktopui.SidebarTopInset)
	}
	if desktopui.DefaultSidebarWidth != 220 {
		t.Errorf("DefaultSidebarWidth = %d, want 220", desktopui.DefaultSidebarWidth)
	}
}

// In dark mode the sidebar surface must differ from the content
// surface: the capture comparison (dark-active-focus.png against
// dark-active-notes.png) showed both columns rendering the same
// near-black with no divider, because the dark vibrancy sidebar
// material lands on the same #1E1E1E as the content background. The
// dark re-declaration carries a tint; the light default stays
// transparent.
func TestDarkSidebarSurfaceDiffersFromContent(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	decl := cssProperty(t, css, ":root[data-color-scheme=\"dark\"] .desktopui-frame", "--desktop-sidebar-surface")
	if decl == "" || decl == "transparent" {
		t.Fatalf("dark block does not re-declare a distinct --desktop-sidebar-surface (got %q):\n%s", decl, css)
	}
	content := cssProperty(t, css, ".desktopui-frame > main", "background-color")
	if decl == content {
		t.Fatalf("dark sidebar surface equals the content surface %q:\n%s", content, css)
	}
	// The OS-preference path re-declares the same tint, the same shape
	// the theme's token emitter uses (the data-color-scheme attribute
	// or the media query, whichever fires first).
	if !strings.Contains(css, "@media (prefers-color-scheme: dark)") {
		t.Errorf("layout CSS lacks the prefers-color-scheme dark block:\n%s", css)
	}
}

// cssProperty finds one rule's declaration. The layout sheet is
// hand-written CSS, so the test reads it with a line scan rather than
// a real parser; the sheet's shape (one declaration per line) is under
// this package's control.
func cssProperty(t *testing.T, css, selector, prop string) string {
	t.Helper()
	lines := strings.Split(css, "\n")
	inRule := false
	var depth int
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.Contains(ln, selector) && strings.HasSuffix(trimmed, "{") {
			inRule = true
			depth = 1
			continue
		}
		if !inRule {
			continue
		}
		depth += strings.Count(ln, "{") - strings.Count(ln, "}")
		if strings.HasPrefix(trimmed, prop+":") {
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.SplitN(trimmed, ":", 2)[1]), ";"))
		}
		if depth <= 0 {
			inRule = false
		}
	}
	return ""
}

// sidebarComp is a minimal sidebar component for the layout test.
type sidebarComp struct{}

func (sidebarComp) Render() render.HTML {
	return plain(`<div class="desktopui-sourcelist">nav</div>`)
}

// lookup returns a registered component entry by name.
func lookup(t *testing.T, name string) (*registry.Entry, bool) {
	t.Helper()
	return registry.Lookup(name)
}

// A narrow window keeps the sidebar beside the content: the web
// content row stacks the nav above the page under 48rem, which in the
// 480-point settings window put the whole source list on top of the
// form (the 2026-09-22 capture). The desktop frame has no narrow-width
// rule at all.
func TestLayoutHoldsRowWhenNarrow(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	if strings.Contains(css, "max-width") {
		t.Errorf("desktop frame carries a narrow-width rule:\n%s", css)
	}
	if got := cssProperty(t, css, ".desktopui-frame {", "display"); got != "flex" {
		t.Errorf("frame display = %q, want flex", got)
	}
}

// WindowLayout is the sidebar-less window: transparent page and
// content column, and the traffic-light zone reserved at the top.
func TestWindowLayoutCSS(t *testing.T) {
	l := desktopui.WindowLayout()
	if l.Name != desktopui.WindowLayoutName {
		t.Fatalf("WindowLayout name = %q, want %q", l.Name, desktopui.WindowLayoutName)
	}
	out := string(l.WrapCtx(context.Background(), plain("<p>x</p>")))
	if !strings.Contains(out, "desktopui-frame--window") || strings.Contains(out, "<nav") {
		t.Errorf("window layout markup wrong:\n%s", out)
	}
	css := componentCSS(t, "desktopui-layout")
	col := ".desktopui-frame--window > .layout-content"
	if got := cssProperty(t, css, col, "background-color"); got != "transparent" {
		t.Errorf("window layout content background = %q, want transparent", got)
	}
	if got := cssProperty(t, css, col, "padding"); !strings.HasPrefix(got, "52px var(--spacing-xl") {
		t.Errorf("window layout content padding = %q, want the 52px traffic-light zone over xl gutters", got)
	}
}

// A floating widget window is transparent and small: the page behind
// the screen's surface must paint nothing, and the frame must not
// force a viewport-tall body or a padded column. The first widget
// rendered a white slab with its button cut off; a screenshot caught
// it, no DOM assertion could.
func TestWidgetLayoutIsTransparentAndCompact(t *testing.T) {
	l := desktopui.WidgetLayout()
	if l.Name != desktopui.WidgetLayoutName {
		t.Fatalf("WidgetLayout name = %q, want %q", l.Name, desktopui.WidgetLayoutName)
	}
	out := string(l.WrapCtx(context.Background(), plain("<p>body</p>")))
	if strings.Count(out, "<main") != 1 {
		t.Fatalf("WidgetLayout must emit exactly one <main> landmark:\n%s", out)
	}
	for _, tag := range []string{"<header", "<footer", "<nav"} {
		if strings.Contains(out, tag) {
			t.Errorf("WidgetLayout emitted %s:\n%s", tag, out)
		}
	}
	css := componentCSS(t, "desktopui-layout")
	if got := cssProperty(t, css, ".desktopui-frame--widget {", "min-block-size"); got != "0" {
		t.Errorf("widget frame min-block-size = %q, want 0", got)
	}
	col := ".desktopui-frame--widget > .layout-content"
	if got := cssProperty(t, css, col, "background-color"); got != "transparent" {
		t.Errorf("widget content background = %q, want transparent", got)
	}
	if got := cssProperty(t, css, col, "padding"); got != "var(--desktop-widget-padding, 8px)" {
		t.Errorf("widget content padding = %q, want the widget padding knob", got)
	}
}

// The desktop frames tighten the framework controls' shared block
// padding; the web default (10px) stays everywhere else.
func TestLayoutsSetControlDensity(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	if decl := cssProperty(t, css, "html:has(.desktopui-frame) {", "--ui-control-padding-y"); decl != "4px" {
		t.Errorf("--ui-control-padding-y = %q, want 4px", decl)
	}
}
