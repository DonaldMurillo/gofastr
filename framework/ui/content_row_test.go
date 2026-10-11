package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderRow(t *testing.T, cfg ContentRowConfig, main render.HTML) string {
	t.Helper()
	nav, err := component.SafeRenderCtx(context.Background(), Sidebar(SidebarConfig{
		NavLabel: "Projects",
		Items:    []SidebarItem{{Label: "Project", Href: "/p"}},
	}))
	if err != nil {
		t.Fatalf("sidebar render: %v", err)
	}
	cfg.Sidebar = nav
	return string(ContentRow(cfg, main))
}

// The start column is the nav landmark AROUND the sidebar
// (NavLabel, default "Sidebar"): headless.Sidebar's own <nav> names
// only the links list, so the column landmark is what keeps the
// sidebar's title and footer inside a landmark (axe region — the
// retired page shell wrapped the column the same way).
func TestContentRowNavColumnIsTheSidebarLandmark(t *testing.T) {
	out := renderRow(t, ContentRowConfig{}, render.Text("main"))
	if !strings.Contains(out, `aria-label="Sidebar" class="fui-content-row__nav"`) {
		t.Errorf("start column should be the labelled nav landmark:\n%s", out)
	}
	if got := strings.Count(out, `<nav aria-label=`); got != 2 {
		t.Errorf("two nav landmarks should render (the column around the sidebar's links list), got %d:\n%s", got, out)
	}
	out = renderRow(t, ContentRowConfig{NavLabel: "Projects nav"}, render.Text("main"))
	if !strings.Contains(out, `aria-label="Projects nav" class="fui-content-row__nav"`) {
		t.Errorf("NavLabel should name the column landmark:\n%s", out)
	}
}

// The end column is an <aside> labelled by AsideLabel (default
// "Context"), and an absent Aside renders no landmark at all.
func TestContentRowAsideLandmark(t *testing.T) {
	out := renderRow(t, ContentRowConfig{Aside: render.Text("activity")}, render.Text("main"))
	if !strings.Contains(out, `<aside aria-label="Context" class="fui-content-row__aside"`) {
		t.Errorf("aside should render with the default label and the row's class:\n%s", out)
	}
	out = renderRow(t, ContentRowConfig{Aside: render.Text("x"), AsideLabel: "Activity"}, render.Text("main"))
	if !strings.Contains(out, `<aside aria-label="Activity" class="fui-content-row__aside"`) {
		t.Errorf("AsideLabel should name the landmark:\n%s", out)
	}
	out = renderRow(t, ContentRowConfig{}, render.Text("main"))
	if strings.Contains(out, "<aside") {
		t.Errorf("no Aside config, no aside element:\n%s", out)
	}
}

// The workspace wrapper (toolbar row above main, beside the nav) exists
// only when a Toolbar was given; otherwise main is a direct child the
// row's own stylesheet addresses.
func TestContentRowWorkspaceOnlyWithToolbar(t *testing.T) {
	out := renderRow(t, ContentRowConfig{}, render.Text("main"))
	if strings.Contains(out, "fui-content-row__workspace") {
		t.Errorf("no toolbar, no workspace wrapper:\n%s", out)
	}
	out = renderRow(t, ContentRowConfig{Toolbar: render.Text("crumbs")}, render.Text("main"))
	if !strings.Contains(out, `<div class="fui-content-row__workspace">`) {
		t.Errorf("toolbar should render inside the workspace wrapper:\n%s", out)
	}
	if !strings.Contains(out, `class="fui-content-row__toolbar"`) {
		t.Errorf("toolbar row missing its own band:\n%s", out)
	}
}

// The toolbar row sits outside main and the nav column, so it is a
// labelled region of its own: controls placed there (a palette trigger,
// a theme toggle) stay inside a landmark (axe region).
func TestContentRowToolbarIsARegion(t *testing.T) {
	out := renderRow(t, ContentRowConfig{Toolbar: render.Text("crumbs")}, render.Text("main"))
	if !regexp.MustCompile(`<section [^>]*aria-label="Toolbar"[^>]*class="fui-content-row__toolbar"`).MatchString(out) {
		t.Errorf("toolbar should be a section labelled Toolbar:\n%s", out)
	}
	out = renderRow(t, ContentRowConfig{Toolbar: render.Text("crumbs"), ToolbarLabel: "Workspace tools"}, render.Text("main"))
	if !strings.Contains(out, `aria-label="Workspace tools"`) {
		t.Errorf("ToolbarLabel should name the region:\n%s", out)
	}
}

// The frame modifiers ride the root's class list, and an unknown
// breakpoint is loud at render (the Sidebar contract).
// Class appends to the root's class list, after the component's own
// classes. The landmark rework dropped the append once and the field
// went silently inert.
func TestContentRowClassAppendsToRoot(t *testing.T) {
	out := string(ContentRow(ContentRowConfig{Class: "lab-row"}, render.Text("main")))
	if !strings.Contains(out, `class="fui-content-row lab-row"`) {
		t.Errorf("Class should append to the root's class list:\n%s", out)
	}
}

func TestContentRowModifiersAndBreakpointValidation(t *testing.T) {
	out := renderRow(t, ContentRowConfig{Viewport: true, PhoneNavFlush: true}, render.Text("main"))
	for _, want := range []string{"fui-content-row--has-nav", "fui-content-row--viewport", "fui-content-row--phone-nav-flush"} {
		if !strings.Contains(out, want) {
			t.Errorf("root class list should carry %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fui-content-row--stack-below-lg") {
		t.Errorf("default breakpoint must not carry the lg marker:\n%s", out)
	}
	out = renderRow(t, ContentRowConfig{Breakpoint: StackBelowLG}, render.Text("main"))
	if !strings.Contains(out, "fui-content-row--stack-below-lg") {
		t.Errorf("StackBelowLG should mark the root:\n%s", out)
	}
	defer func() { _ = recover() }()
	renderRow(t, ContentRowConfig{Breakpoint: "xl"}, render.Text("main"))
	t.Error("an unknown StackBreakpoint should panic at render")
}

// Sticky and Viewport are two scroll models: a row asking for both is
// refused at render, not drawn with one silently winning.
func TestContentRowStickyRefusesViewport(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ContentRow drew a row with both Sticky and Viewport")
		}
	}()
	ContentRow(ContentRowConfig{Sticky: true, Viewport: true}, render.Text("main"))
}
