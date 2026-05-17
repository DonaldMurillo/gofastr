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
		Items: []ui.SidebarItem{{Label: "Home", Href: "/"}},
	}
	body := string(ui.SidebarBody(cfg))
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
