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
// declared width with the traffic-light inset reserved at its top, and
// an opaque content column.
func TestLayoutCSS(t *testing.T) {
	css := componentCSS(t, "desktopui-layout")
	for _, w := range []string{
		"html:has(.layout-desktop), body:has(.layout-desktop) { background-color: transparent; }",
		"--desktop-sidebar-width: 220px;",
		"--desktop-traffic-inset: 12px;",
		"flex-basis: var(--desktop-sidebar-width",
		"padding-top: var(--desktop-traffic-inset",
		"background: transparent",
		"background-color: var(--color-background",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("layout CSS missing %q:\n%s", w, css)
		}
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

// TrafficLightInset is the value the shell worker's contract field
// defaults to: Electron's hiddenInset margin is (12, 11) and its own
// comment says it does not match native apps; the desktop contract
// rounds to 12x12 so the page reservation and the native placement
// agree.
func TestTrafficLightInsetDefault(t *testing.T) {
	if desktopui.TrafficLightInset != 12 {
		t.Errorf("TrafficLightInset = %d, want 12", desktopui.TrafficLightInset)
	}
	if desktopui.DefaultSidebarWidth != 220 {
		t.Errorf("DefaultSidebarWidth = %d, want 220", desktopui.DefaultSidebarWidth)
	}
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
