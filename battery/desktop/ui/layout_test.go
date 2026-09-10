package desktopui_test

import (
	"strings"
	"testing"

	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout returns the core-ui layout named "desktop": the same Layout
// every screen registers with, so SPA navigation, layer keys, and the
// one-<main> rule all hold. The host chains WithSidebar.
func TestLayoutIsCoreUILayout(t *testing.T) {
	l := desktopui.Layout()
	if l == nil {
		t.Fatal("Layout returned nil")
	}
	if l.Name != "desktop" || desktopui.LayoutName != "desktop" {
		t.Errorf("layout name = %q, LayoutName = %q, want desktop", l.Name, desktopui.LayoutName)
	}
	var _ *appui.Layout = l
}

// Wrapping a screen through the layout emits the three regions: the
// sidebar <nav> (from WithSidebar), the content <main>, and the
// .layout-body row that carries both. The desktop sheet keys off
// .layout-desktop on the wrapper.
func TestLayoutRendersThreeRegions(t *testing.T) {
	l := desktopui.Layout().WithSidebar(sidebarComp{})
	out := string(l.Wrap(plain("<p>content</p>")))
	for _, w := range []string{
		"layout-desktop",
		`data-fui-layout="desktop"`,
		`aria-label="Sidebar"`,
		`id="main-content"`,
		`class="layout-body"`,
		"<p>content</p>",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("desktop layout missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "style=") {
		t.Errorf("layout must not emit an inline style:\n%s", out)
	}
}

// The layout stylesheet places the three zones: transparent html/body
// so the native material shows through, the sidebar nav at the
// declared width with the measured traffic-light zone reserved at its
// top, and an opaque content column.
func TestLayoutCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	for _, w := range []string{
		"html:has(.layout-desktop), body:has(.layout-desktop) { background-color: transparent; }",
		"--desktop-sidebar-width: 220px;",
		"--desktop-sidebar-top-inset: 52px;",
		"flex-basis: var(--desktop-sidebar-width",
		"padding-top: var(--desktop-sidebar-top-inset",
		"background: var(--desktop-sidebar-surface, transparent)",
		"background-color: var(--color-background",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("layout CSS missing %q:\n%s", w, css)
		}
	}
	if strings.Contains(css, "traffic-inset") {
		t.Errorf("layout CSS still carries the traffic-inset knob:\n%s", css)
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
	decl := cssProperty(t, css, ":root[data-color-scheme=\"dark\"] .layout-desktop", "--desktop-sidebar-surface")
	if decl == "" || decl == "transparent" {
		t.Fatalf("dark block does not re-declare a distinct --desktop-sidebar-surface (got %q):\n%s", decl, css)
	}
	content := cssProperty(t, css, ".layout-desktop .layout-body > main", "background-color")
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
