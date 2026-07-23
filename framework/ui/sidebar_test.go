package ui_test

import (
	"strings"
	"testing"

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
		`<details class="ui-sidebar__group" data-fui-disclosure open>`,
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
