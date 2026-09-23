package headless

import (
	"strings"
	"testing"
)

func renderSidebar(p SidebarProps) string { return string(Sidebar(p, nil)) }

func TestSidebarRendersShellNavGroupsAndLinks(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "Primary", Title: "Workspace", Items: []SidebarItem{
		{Label: "Home", Href: "/", Active: true},
		{Label: "Settings", Children: []SidebarItem{
			{Label: "Profile", Href: "/settings/profile"},
		}},
	}})
	for _, want := range []string{
		`<div data-hui-sidebar="" data-hui-sidebar-variant="persistent">`,
		// The nav landmark is the region inside the shell; the shell is
		// a div because a host layout wraps the sidebar in its own nav.
		`<nav aria-label="Primary"><ul>`,
		`<a aria-current="page" href="/"><span>Home</span></a>`,
		// Details-dialect groups are disclosures with a per-group
		// persist key, so the user's open sections survive navigation.
		`<details data-hui-disclosure="" data-hui-disclosure-persist="sidebar-inline-g1">`,
		`<summary><span>Settings</span></summary>`,
		`<a href="/settings/profile">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("sidebar missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, `data-hui-sidebar-group=""`) {
		t.Errorf("the details dialect no longer carries the group marker (the disclosure hooks own it):\n%s", h)
	}
}

func TestSidebarButtonDialectGroups(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", GroupMarkup: "button", Items: []SidebarItem{
		{Label: "One", Href: "/1"},
		{Label: "Two", Open: true, Children: []SidebarItem{{Label: "Two A", Href: "/2a"}}},
		{Label: "Three", Children: []SidebarItem{{Label: "Three A", Href: "/3a"}}},
	}})
	for _, want := range []string{
		`<button aria-controls="sidebar-inline-g1" aria-expanded="true" data-hui-sidebar-group-toggle="" type="button">`,
		`<ul id="sidebar-inline-g1">`,
		// A closed group's links are unreachable without script; the
		// hidden attribute says so in the SSR bytes.
		`<button aria-controls="sidebar-inline-g2" aria-expanded="false" data-hui-sidebar-group-toggle="" type="button">`,
		`<ul hidden="" id="sidebar-inline-g2">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("button dialect missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "<details") || strings.Contains(h, "<summary") {
		t.Errorf("the button dialect must not render details:\n%s", h)
	}
	// A caller's prefix derives the ids (the aria-controls targets must
	// not collide when the same navigation renders twice on a page).
	h2 := renderSidebar(SidebarProps{NavLabel: "N", GroupMarkup: "button",
		GroupIDPrefix: "host-body", Items: []SidebarItem{
			{Label: "Two", Children: []SidebarItem{{Label: "A", Href: "/a"}}},
		}})
	if !strings.Contains(h2, `aria-controls="host-body-g1"`) {
		t.Errorf("GroupIDPrefix must derive the group ids:\n%s", h2)
	}
}

func TestSidebarCollapseContract(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible", Collapse: "auto",
		CollapseStorageKey: "nav.side", Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	for _, want := range []string{
		`data-hui-sidebar-collapse="auto"`,
		`data-hui-sidebar-storage="nav.side"`,
		`data-hui-sidebar-toggle=""`,
		`aria-controls="sidebar-inline"`,
		`aria-expanded="true"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("collapsible sidebar missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "data-collapsed") {
		t.Errorf("auto mode must not ship data-collapsed (the module restores and owns it):\n%s", h)
	}
	// Server-owned: no storage attribute, the module never writes, and
	// the state ships in the SSR bytes with the state-matched name.
	server := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible",
		ServerCollapsed: new(true), CollapseLabel: "Collapse navigation",
		ExpandLabel: "Expand navigation",
		Items:       []SidebarItem{{Label: "One", Href: "/1"}}})
	for _, want := range []string{
		`data-collapsed="true"`,
		`aria-expanded="false"`,
		`aria-label="Expand navigation"`,
		`data-hui-sidebar-collapse-label="Collapse navigation"`,
		`data-hui-sidebar-expand-label="Expand navigation"`,
	} {
		if !strings.Contains(server, want) {
			t.Errorf("server-collapsed sidebar missing %q:\n%s", want, server)
		}
	}
	if strings.Contains(server, "data-hui-sidebar-storage") {
		t.Errorf("a sidebar with no CollapseStorageKey must not carry the storage hook:\n%s", server)
	}
	// The drawer trigger: a separate control from the collapse toggle,
	// opening the widget the caller mounts.
	drawer := renderSidebar(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer",
		DrawerLabel: "Open navigation", Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	if !strings.Contains(drawer, `aria-label="Open navigation" data-fui-open="nav-drawer"`) {
		t.Errorf("the drawer trigger must open the widget and carry its label:\n%s", drawer)
	}
	hidden := renderSidebar(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer",
		HideDrawerTrigger: true, Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	if strings.Contains(hidden, "data-fui-open") {
		t.Errorf("HideDrawerTrigger must suppress the trigger:\n%s", hidden)
	}
}

func TestSidebarRegionRendersNoShellHooks(t *testing.T) {
	h := string(SidebarRegion(SidebarProps{NavLabel: "Sections", Title: "Docs",
		GroupIDPrefix: "panel", Items: []SidebarItem{
			{Label: "One", Href: "/1"},
			{Label: "Two", Children: []SidebarItem{{Label: "A", Href: "/a"}}},
		}}, nil))
	for _, want := range []string{
		`<h2>Docs</h2>`,
		`<nav aria-label="Sections">`,
		`data-hui-disclosure-persist="panel-g1"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("region missing %q:\n%s", want, h)
		}
	}
	for _, banned := range []string{"data-hui-sidebar", "data-hui-sidebar-toggle", "data-fui-open"} {
		if strings.Contains(h, banned) {
			t.Errorf("the region must not carry the shell hook %q:\n%s", banned, h)
		}
	}
}

func TestSidebarRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    SidebarProps
	}{
		{"no nav label", SidebarProps{Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"whitespace nav label", SidebarProps{NavLabel: "  ", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"no items", SidebarProps{NavLabel: "N"}},
		{"labelless item", SidebarProps{NavLabel: "N", Items: []SidebarItem{{Href: "/a"}}}},
		{"whitespace item label", SidebarProps{NavLabel: "N", Items: []SidebarItem{{Label: " ", Href: "/a"}}}},
		{"group with href", SidebarProps{NavLabel: "N", Items: []SidebarItem{
			{Label: "G", Href: "/g", Children: []SidebarItem{{Label: "C", Href: "/c"}}},
		}}},
		{"bad variant", SidebarProps{NavLabel: "N", Variant: "diagonal", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"bad collapse", SidebarProps{NavLabel: "N", Collapse: "sometimes", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"bad group markup", SidebarProps{NavLabel: "N", GroupMarkup: "canvas", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"server state while auto", SidebarProps{NavLabel: "N", Variant: "collapsible",
			Collapse: "auto", ServerCollapsed: new(true), Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"control bytes in storage key", SidebarProps{NavLabel: "N", Collapse: "auto",
			CollapseStorageKey: "a\r\nb", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"control bytes in group id prefix", SidebarProps{NavLabel: "N",
			GroupIDPrefix: "a\x00b", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Sidebar(tc.p, nil)
		}()
	}
	// The region's own refusals.
	for _, tc := range []struct {
		name string
		p    SidebarProps
	}{
		{"no nav label", SidebarProps{Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
		{"no items", SidebarProps{NavLabel: "N"}},
		{"bad group markup", SidebarProps{NavLabel: "N", GroupMarkup: "canvas", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("region %s: rendering should have been refused", tc.name)
				}
			}()
			SidebarRegion(tc.p, nil)
		}()
	}
}

func TestSidebarUnsafeHrefDegrades(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", Items: []SidebarItem{
		{Label: "Evil", Href: "javascript:alert(1)"},
	}})
	if strings.Contains(h, `href="javascript:`) {
		t.Errorf("an unsafe href reached the link:\n%s", h)
	}
	if !strings.Contains(h, `href="#"`) {
		t.Errorf("the unsafe href must degrade to #:\n%s", h)
	}
}
