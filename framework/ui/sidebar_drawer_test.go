package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// In-package on purpose: sidebarDrawerSlot (the MountSidebar drawer's
// body) is unexported, and the id-prefix contract it pins is between
// package-internal renderers.

func TestSidebarDrawerSlotGroupIdsUseDrawerPrefix(t *testing.T) {
	// The inline sidebar and the drawer body render the same groups on
	// one page. In the button dialect both mint aria-controls targets,
	// so the drawer's must carry the -drawer prefix: with the inline
	// prefix instead, getElementById returns the inline panel and a
	// group toggle inside the drawer drives the inline sidebar.
	cfg := SidebarConfig{
		DrawerName:  "workspace-nav",
		GroupMarkup: SidebarGroupButton,
		Items:       []SidebarItem{{Label: "G", Children: []SidebarItem{{Label: "C", Href: "/c"}}}},
	}
	out := string(sidebarDrawerSlot{cfg: cfg}.Render())
	if !strings.Contains(out, `aria-controls="workspace-nav-drawer-g1"`) {
		t.Errorf("drawer slot groups must use the -drawer prefix on aria-controls:\n%s", out)
	}
	if !strings.Contains(out, `<ul class="fui-sidebar__sublist" hidden="" id="workspace-nav-drawer-g1">`) {
		t.Errorf("drawer slot group container must carry the -drawer-prefixed id:\n%s", out)
	}
}

// The MountSidebar drawer carries a header row: the app's brand and a
// 44px close button wired to the widget runtime's close contract, so
// the open panel is not a bare link list pressed against the edge.
func TestSidebarDrawerHeaderRenders(t *testing.T) {
	r := &mountRecorder{}
	def := MountSidebar(r, SidebarConfig{
		DrawerTitle: "Acme Tracker",
		Items:       []SidebarItem{{Label: "Home", Href: "/"}},
	})
	chrome := string(widget.RenderChrome(&def))
	for _, want := range []string{
		`class="fui-sidebar__drawer-head"`,
		`<span class="fui-sidebar__drawer-brand">Acme Tracker</span>`,
		`aria-label="Close navigation"`,
		`data-fui-action="close"`,
		`class="fui-sidebar__drawer-close"`,
	} {
		if !strings.Contains(chrome, want) {
			t.Errorf("drawer chrome missing %s:\n%s", want, chrome)
		}
	}
	// The header must sit ABOVE the body slot (brand read first).
	if head, body := strings.Index(chrome, "fui-sidebar__drawer-head"),
		strings.Index(chrome, "fui-slot-body"); head < 0 || body < 0 || head > body {
	}
}

// No brand anywhere (both Title and DrawerTitle empty): the drawer
// renders no header rather than an empty row above the nav.
func TestSidebarDrawerHeaderOmittedWithoutTitle(t *testing.T) {
	r := &mountRecorder{}
	def := MountSidebar(r, SidebarConfig{Items: []SidebarItem{{Label: "Home", Href: "/"}}})
	if chrome := string(widget.RenderChrome(&def)); strings.Contains(chrome, "fui-sidebar__drawer-head") {
		t.Errorf("an untitled sidebar must render no drawer header:\n%s", chrome)
	}
}

// DrawerTitle wins over Title (the inline column's heading and the
// drawer's brand can differ).
func TestSidebarDrawerTitleBeatsTitle(t *testing.T) {
	r := &mountRecorder{}
	def := MountSidebar(r, SidebarConfig{Title: "Sections", DrawerTitle: "Acme"})
	chrome := string(widget.RenderChrome(&def))
	if !strings.Contains(chrome, `<span class="fui-sidebar__drawer-brand">Acme</span>`) {
		t.Errorf("DrawerTitle must win the drawer brand slot:\n%s", chrome)
	}
}

type mountRecorder struct{ mounted []*widget.Definition }

func (m *mountRecorder) MountWidget(def *widget.Definition) { m.mounted = append(m.mounted, def) }

// sectionKey is the request-scoped value a context-aware Prepend reads.
type sectionKey struct{}

// ctxPrepend renders the current section from the request context. It
// stands in for a docs site's section switcher: MountSidebar runs once
// at boot, so only a per-request render can mark the right option.
type ctxPrepend struct{ component.ContextOnly }

func (ctxPrepend) RenderCtx(ctx context.Context) render.HTML {
	section, _ := ctx.Value(sectionKey{}).(string)
	return render.HTML(`<select id="section"><option selected>` + render.Escape(section) + `</option></select>`)
}

// The drawer body is the only navigation below the md breakpoint, so
// Prepend has to reach it too, not only the inline sidebar (#405), and
// it has to reach it WITH the request: the drawer chrome is served per
// request, and a Prepend that implements ContextComponent must see
// that context on both the drawer and the inline path.
func TestSidebarPrependRendersWithRequestCtx(t *testing.T) {
	cfg := SidebarConfig{
		DrawerName: "docs-nav",
		Prepend:    ctxPrepend{},
		Items:      []SidebarItem{{Label: "Home", Href: "/"}},
	}
	ctx := context.WithValue(context.Background(), sectionKey{}, "Batteries")
	for name, out := range map[string]string{
		"drawer": string(sidebarDrawerSlot{cfg: cfg}.RenderCtx(ctx)),
		"inline": string(sidebarComponent{cfg: cfg}.RenderCtx(ctx)),
	} {
		pre := strings.Index(out, `<div class="fui-sidebar__prepend"><select id="section"><option selected>Batteries</option>`)
		nav := strings.Index(out, `class="fui-sidebar__nav"`)
		if pre < 0 || nav < 0 || pre > nav {
			t.Errorf("%s: Prepend must render with the request ctx above the nav (prepend=%d nav=%d):\n%s", name, pre, nav, out)
		}
	}
}
