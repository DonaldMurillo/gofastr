package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestSidebarRendersInlineAndHamburger(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{
		Title: "App",
		Items: []ui.SidebarItem{
			{Label: "Dashboard", Href: "/"},
			{Label: "Customers", Href: "/customers"},
		},
		CurrentPath: "/customers",
	})
	out := string(c.Render())
	for _, want := range []string{
		`data-fui-comp="ui-sidebar"`,
		`ui-sidebar--persistent`,
		`data-fui-open="ui-sidebar-drawer"`,
		`aria-label="Open navigation"`,
		`<h2 class="ui-sidebar__title">App</h2>`,
		`href="/customers"`,
		`aria-current="page"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sidebar html missing %q\n--\n%s", want, out)
		}
	}
}

func TestSidebarNestedItemsUseDisclosure(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{
		Items: []ui.SidebarItem{
			{Label: "Settings", Children: []ui.SidebarItem{
				{Label: "Profile", Href: "/settings/profile"},
				{Label: "Team", Href: "/settings/team"},
			}},
		},
		CurrentPath: "/settings/profile",
	})
	out := string(c.Render())
	for _, want := range []string{
		`<details class="ui-sidebar__group" data-fui-disclosure data-fui-disclosure-persist open>`,
		`>Settings</span></summary>`,
		`href="/settings/profile"`,
		`aria-current="page"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("nested sidebar missing %q\n--\n%s", want, out)
		}
	}
}

func TestSidebarBodyExposesSharedContent(t *testing.T) {
	cfg := ui.SidebarConfig{
		NavLabel: "Workspace",
		Items:    []ui.SidebarItem{{Label: "Home", Href: "/"}},
	}
	body := string(ui.SidebarBody(cfg))
	if !strings.Contains(body, `aria-label="Workspace"`) {
		t.Errorf("SidebarBody should use the configured landmark label: %s", body)
	}
	if !strings.Contains(body, `class="ui-sidebar__nav"`) {
		t.Errorf("SidebarBody should render the nav: %s", body)
	}
	if strings.Contains(body, "data-fui-open") {
		t.Errorf("SidebarBody should NOT render the hamburger: %s", body)
	}
}

func TestSidebarSuppressDrawerTrigger(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{
		Items:                 []ui.SidebarItem{{Label: "x", Href: "/"}},
		SuppressDrawerTrigger: true,
	})
	out := string(c.Render())
	if strings.Contains(out, `data-fui-open=`) {
		t.Errorf("SuppressDrawerTrigger should hide hamburger: %s", out)
	}
}

func TestSidebarCollapsibleEmitsPersistedToggleContract(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{
		Variant:            ui.SidebarCollapsible,
		DrawerName:         "workspace-nav",
		CollapseStorageKey: "app.sidebar.collapsed",
		Items:              []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	})
	out := string(c.Render())
	for _, want := range []string{
		`ui-sidebar--collapsible`,
		`data-fui-sidebar-storage="app.sidebar.collapsed"`,
		`data-fui-sidebar-collapse`,
		`aria-controls="workspace-nav-inline"`,
		`aria-expanded="true"`,
		`aria-label="Collapse navigation"`,
		`ui-sidebar__icon--fallback`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("collapsible sidebar missing %q\n--\n%s", want, out)
		}
	}
}

// TestSidebarCollapsibleAutoMatchesLegacyBytes pins the Auto (zero
// value) contract: the collapse owner fields must not change a byte
// of today's output. A HEAD-vs-branch render diff proved it for the
// full component; this pins it forever.
func TestSidebarCollapsibleAutoMatchesLegacyBytes(t *testing.T) {
	out := string(ui.Sidebar(ui.SidebarConfig{
		Variant:    ui.SidebarCollapsible,
		DrawerName: "workspace-nav",
		Items:      []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	}).Render())
	// Root: storage attribute present, no data-collapsed.
	if !strings.Contains(out, `data-fui-sidebar data-fui-sidebar-storage="gofastr.sidebar.workspace-nav.collapsed"`) {
		t.Errorf("Auto mode must emit the storage key and nothing else on the root:\n%s", out)
	}
	if strings.Contains(out, "data-collapsed") {
		t.Errorf("Auto mode must not emit data-collapsed (runtime owns the state):\n%s", out)
	}
	// Button: exact legacy bytes, including the hardcoded label pair.
	want := `<button type="button" class="ui-sidebar__collapse" data-fui-sidebar-collapse ` +
		`aria-controls="workspace-nav-inline" aria-expanded="true" aria-label="Collapse navigation">` +
		`<span aria-hidden="true">‹</span></button>`
	if !strings.Contains(out, want) {
		t.Errorf("Auto mode button must match the legacy bytes exactly:\nwant %s\ngot  %s", want, out)
	}
	if strings.Contains(out, "data-fui-sidebar-collapse-label") || strings.Contains(out, "data-fui-sidebar-expand-label") {
		t.Errorf("default labels must not emit label override attributes:\n%s", out)
	}
}

func TestSidebarServerCollapsedRendersRailState(t *testing.T) {
	out := string(ui.Sidebar(ui.SidebarConfig{
		Variant:    ui.SidebarCollapsible,
		DrawerName: "workspace-nav",
		Collapse:   ui.SidebarCollapseCollapsed,
		Items:      []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	}).Render())
	if !strings.Contains(out, `data-collapsed="true"`) {
		t.Errorf("server-collapsed sidebar must render data-collapsed=\"true\" on the root:\n%s", out)
	}
	if strings.Contains(out, "data-fui-sidebar-storage") {
		t.Errorf("server-owned collapse state must suppress the localStorage key (no read, no write):\n%s", out)
	}
	want := `aria-expanded="false" aria-label="Expand navigation"`
	if !strings.Contains(out, want) {
		t.Errorf("server-collapsed button must carry the expand label + aria-expanded=false:\nwant %q\ngot  %s", want, out)
	}
}

func TestSidebarServerExpandedRendersColumnState(t *testing.T) {
	out := string(ui.Sidebar(ui.SidebarConfig{
		Variant:    ui.SidebarCollapsible,
		DrawerName: "workspace-nav",
		Collapse:   ui.SidebarCollapseExpanded,
		Items:      []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	}).Render())
	if !strings.Contains(out, `data-collapsed="false"`) {
		t.Errorf("server-expanded sidebar must render data-collapsed=\"false\":\n%s", out)
	}
	if strings.Contains(out, "data-fui-sidebar-storage") {
		t.Errorf("server-owned collapse state must suppress the localStorage key:\n%s", out)
	}
	if !strings.Contains(out, `aria-expanded="true" aria-label="Collapse navigation"`) {
		t.Errorf("server-expanded button must carry the collapse label + aria-expanded=true:\n%s", out)
	}
}

func TestSidebarCollapseLabelsConfigurableBothStates(t *testing.T) {
	base := ui.SidebarConfig{
		Variant:       ui.SidebarCollapsible,
		DrawerName:    "workspace-nav",
		CollapseLabel: "Collapse sidebar",
		ExpandLabel:   "Expand sidebar",
		Items:         []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	}
	expanded := string(ui.Sidebar(base).Render())
	if !strings.Contains(expanded, `aria-expanded="true" aria-label="Collapse sidebar"`) {
		t.Errorf("expanded sidebar must use the configured collapse label:\n%s", expanded)
	}
	base2 := base
	base2.Collapse = ui.SidebarCollapseCollapsed
	collapsed := string(ui.Sidebar(base2).Render())
	if !strings.Contains(collapsed, `aria-expanded="false" aria-label="Expand sidebar"`) {
		t.Errorf("collapsed sidebar must use the configured expand label:\n%s", collapsed)
	}
	// The runtime needs both labels to keep client-side toggles on the
	// host's wording; they ride on the button itself.
	for _, want := range []string{
		`data-fui-sidebar-collapse-label="Collapse sidebar"`,
		`data-fui-sidebar-expand-label="Expand sidebar"`,
	} {
		if !strings.Contains(collapsed, want) || !strings.Contains(expanded, want) {
			t.Errorf("custom labels must be emitted as button data attributes in both states, missing %q", want)
		}
	}
}

func TestSidebarGroupButtonDialectContract(t *testing.T) {
	out := string(ui.Sidebar(ui.SidebarConfig{
		DrawerName:  "workspace-nav",
		GroupMarkup: ui.SidebarGroupButton,
		Items: []ui.SidebarItem{
			{Label: "Settings", Children: []ui.SidebarItem{
				{Label: "Profile", Href: "/settings/profile"},
			}},
			{Label: "Inactive", Children: []ui.SidebarItem{
				{Label: "Other", Href: "/other"},
			}},
		},
		CurrentPath: "/settings/profile",
	}).Render())
	// Active group: expanded button + visible container.
	if !strings.Contains(out, `<button type="button" class="ui-sidebar__link ui-sidebar__group-toggle" data-fui-sidebar-group-toggle aria-expanded="true" aria-controls="workspace-nav-inline-g1">`) {
		t.Errorf("active group must render an expanded toggle button naming its container:\n%s", out)
	}
	if !strings.Contains(out, `<ul class="ui-sidebar__sublist" id="workspace-nav-inline-g1">`) {
		t.Errorf("open group container must not carry hidden:\n%s", out)
	}
	// Inactive group: collapsed button + hidden container.
	if !strings.Contains(out, `aria-expanded="false" aria-controls="workspace-nav-inline-g2">`) {
		t.Errorf("inactive group must render a collapsed toggle button:\n%s", out)
	}
	if !strings.Contains(out, `<ul class="ui-sidebar__sublist" id="workspace-nav-inline-g2" hidden>`) {
		t.Errorf("closed group container must carry the hidden attribute:\n%s", out)
	}
	if strings.Contains(out, "<details") || strings.Contains(out, "<summary") {
		t.Errorf("button dialect must not emit details/summary markup:\n%s", out)
	}
}

func TestSidebarGroupDefaultDialectUnchanged(t *testing.T) {
	// Zero value GroupMarkup keeps the <details> dialect; no button
	// markers, no generated ids.
	out := string(ui.Sidebar(ui.SidebarConfig{
		DrawerName: "workspace-nav",
		Items: []ui.SidebarItem{
			{Label: "Settings", Children: []ui.SidebarItem{{Label: "Profile", Href: "/p"}}},
		},
	}).Render())
	if !strings.Contains(out, `<details class="ui-sidebar__group" data-fui-disclosure data-fui-disclosure-persist>`) {
		t.Errorf("default group markup must stay <details data-fui-disclosure-persist>:\n%s", out)
	}
	if strings.Contains(out, "data-fui-sidebar-group-toggle") || strings.Contains(out, "aria-controls=") {
		t.Errorf("default dialect must not emit button markers or group ids:\n%s", out)
	}
}

func TestSidebarGroupButtonDialectIdsDisambiguated(t *testing.T) {
	// The inline sidebar and the drawer body render the same groups on
	// one page; their aria-controls targets must not collide.
	cfg := ui.SidebarConfig{
		DrawerName:  "workspace-nav",
		GroupMarkup: ui.SidebarGroupButton,
		Items:       []ui.SidebarItem{{Label: "G", Children: []ui.SidebarItem{{Label: "C", Href: "/c"}}}},
	}
	inline := string(ui.Sidebar(cfg).Render())
	body := string(ui.SidebarBody(cfg))
	if !strings.Contains(inline, `aria-controls="workspace-nav-inline-g1"`) {
		t.Errorf("inline groups must use the -inline prefix:\n%s", inline)
	}
	if !strings.Contains(body, `aria-controls="workspace-nav-body-g1"`) {
		t.Errorf("SidebarBody groups must use the -body prefix:\n%s", body)
	}
}

func TestSidebarAutoHideVariantClassHook(t *testing.T) {
	out := string(ui.Sidebar(ui.SidebarConfig{
		Variant: ui.SidebarAutoHide,
		Items:   []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	}).Render())
	if !strings.Contains(out, `class="ui-sidebar ui-sidebar--auto-hide" data-fui-sidebar`) {
		t.Errorf("auto-hide variant must emit its variant class as the host CSS hook:\n%s", out)
	}
	if strings.Contains(out, "data-fui-sidebar-collapse") {
		t.Errorf("auto-hide must not grow a collapse button (no JS, class hook only):\n%s", out)
	}
}

func TestSidebarBadCollapseAndGroupMarkupPanic(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		fn   func()
	}{
		{
			name: "collapse",
			msg:  "Sidebar unknown Collapse",
			fn: func() {
				_ = ui.Sidebar(ui.SidebarConfig{Variant: ui.SidebarCollapsible, Collapse: ui.SidebarCollapse("collpased")}).Render()
			},
		},
		{
			name: "group markup",
			msg:  "Sidebar unknown GroupMarkup",
			fn: func() {
				_ = ui.SidebarBody(ui.SidebarConfig{GroupMarkup: ui.SidebarGroupMarkup("div")})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic %q, got none", tc.msg)
				}
				if !strings.Contains(fmt.Sprint(r), tc.msg) {
					t.Fatalf("panic %q does not contain %q", r, tc.msg)
				}
			}()
			tc.fn()
		})
	}
}

func TestSidebarOffCanvasEmitsDrawerOnlyVariant(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{
		Variant:    ui.SidebarOffCanvas,
		DrawerName: "workspace-nav",
		Items:      []ui.SidebarItem{{Label: "Dashboard", Href: "/"}},
	})
	out := string(c.Render())
	for _, want := range []string{
		`ui-sidebar--off-canvas`,
		`data-fui-open="workspace-nav"`,
		`id="workspace-nav-inline"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("off-canvas sidebar missing %q\n--\n%s", want, out)
		}
	}
	if strings.Contains(out, `data-fui-sidebar-collapse`) {
		t.Errorf("off-canvas sidebar should not emit a collapse control: %s", out)
	}
}

func TestSidebarExtraAttrsOnRoot(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, variant := range map[string]ui.SidebarVariant{
		"persistent":  ui.SidebarPersistent,
		"collapsible": ui.SidebarCollapsible,
	} {
		out := string(ui.Sidebar(ui.SidebarConfig{
			Variant:    variant,
			DrawerName: "x",
			Items:      []ui.SidebarItem{{Label: "A", Href: "/a"}},
			ExtraAttrs: extra,
		}).Render())
		root := out[:strings.Index(out, ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

func TestSidebarExtraAttrsDataCollapsedDropped(t *testing.T) {
	// The component owns data-collapsed in server-owned mode; a caller
	// copy via ExtraAttrs would render the attribute twice (invalid
	// HTML, and browsers keep the first value, so the copy is inert).
	out := string(ui.Sidebar(ui.SidebarConfig{
		Variant:    ui.SidebarCollapsible,
		Collapse:   ui.SidebarCollapseCollapsed,
		DrawerName: "x",
		Items:      []ui.SidebarItem{{Label: "A", Href: "/a"}},
		ExtraAttrs: map[string]string{"data-collapsed": "false"},
	}).Render())
	root := out[:strings.Index(out, ">")+1]
	if strings.Count(root, "data-collapsed") != 1 || !strings.Contains(root, `data-collapsed="true"`) {
		t.Errorf("caller data-collapsed must be dropped in favour of the component's own value:\n%s", root)
	}
}

// SidebarBody must ship as one scoped root (style marker + nav + footer
// inside it) without a nested data-fui-sidebar root, and the full
// Sidebar shell must still own exactly one.
func TestSidebarBodySingleScopedRoot(t *testing.T) {
	out := string(ui.SidebarBody(ui.SidebarConfig{
		NavLabel: "Dashboard",
		Footer:   render.HTML(`<a href="/x">x</a>`),
		Items:    []ui.SidebarItem{{Label: "Home", Href: "/"}},
	}))
	if !strings.HasPrefix(out, `<div class="ui-sidebar ui-sidebar__body" data-fui-comp="ui-sidebar">`) {
		t.Fatalf("SidebarBody root missing scope: %.120s", out)
	}
	if strings.Count(out, "data-fui-sidebar\"") != 0 || strings.Count(out, "data-fui-sidebar ") != 0 {
		// no data-fui-sidebar attribute anywhere in the body root
		if strings.Contains(out, `data-fui-sidebar`) {
			t.Fatal("SidebarBody must not emit data-fui-sidebar (runtime would treat it as a sidebar root)")
		}
	}
	for _, want := range []string{`aria-label="Dashboard"`, `ui-sidebar__footer`, `>x</a>`} {
		if !strings.Contains(out, want) {
			t.Errorf("SidebarBody missing %q", want)
		}
	}
}

func TestSidebarShellStillSingleRoot(t *testing.T) {
	c := ui.Sidebar(ui.SidebarConfig{Items: []ui.SidebarItem{{Label: "Home", Href: "/"}}})
	out := string(component.RenderComponent(c))
	if got := strings.Count(out, `data-fui-sidebar`); got != 1 {
		t.Fatalf("full Sidebar must emit exactly one data-fui-sidebar root, got %d", got)
	}
	if strings.Contains(out, `ui-sidebar__body`) {
		t.Fatal("full Sidebar must not nest a SidebarBody root")
	}
}

func TestSidebarGroupOpenByDefault(t *testing.T) {
	out := string(ui.SidebarBody(ui.SidebarConfig{
		GroupMarkup: ui.SidebarGroupButton,
		Items: []ui.SidebarItem{
			{Label: "Pinned", Open: true, Children: []ui.SidebarItem{{Label: "Child", Href: "/c"}}},
			{Label: "Shut", Children: []ui.SidebarItem{{Label: "Other", Href: "/o"}}},
		},
	}))
	if !strings.Contains(out, `aria-expanded="true"`) {
		t.Fatal("Open:true group must render expanded")
	}
	// The second group stays closed: exactly one expanded toggle.
	if got := strings.Count(out, `aria-expanded="true"`); got != 1 {
		t.Fatalf("want exactly one expanded group, got %d", got)
	}
}
