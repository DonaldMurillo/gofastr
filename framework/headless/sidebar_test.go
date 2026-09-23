package headless

import (
	"strings"
	"testing"
)

func renderSidebar(p SidebarProps) string { return string(Sidebar(p, nil)) }

func TestSidebarRendersNavGroupsAndLinks(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "Primary", Title: "Workspace", Items: []SidebarItem{
		{Label: "Home", Href: "/", Active: true},
		{Label: "Settings", Children: []SidebarItem{
			{Label: "Profile", Href: "/settings/profile"},
		}},
	}})
	for _, want := range []string{
		`<nav aria-label="Primary" data-hui-sidebar="" data-hui-sidebar-variant="persistent">`,
		`<li><a aria-current="page" href="/"><span>Home</span></a></li>`,
		`<details data-hui-sidebar-group="">`,
		`<summary><span>Settings</span></summary>`,
		`<a href="/settings/profile">`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("sidebar missing %q:\n%s", want, h)
		}
	}
}

func TestSidebarCollapseContract(t *testing.T) {
	h := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible", Collapse: "auto",
		CollapseStorageKey: "nav.side", Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	for _, want := range []string{
		`data-hui-sidebar-collapse="auto"`,
		`data-hui-sidebar-storage="nav.side"`,
		`data-hui-sidebar-toggle=""`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("collapsible sidebar missing %q:\n%s", want, h)
		}
	}
	// Server-owned: no storage attribute, the module never writes.
	server := renderSidebar(SidebarProps{NavLabel: "N", Variant: "collapsible",
		Items: []SidebarItem{{Label: "One", Href: "/1"}}})
	if strings.Contains(server, "data-hui-sidebar-storage") {
		t.Errorf("a sidebar with no CollapseStorageKey must not carry the storage hook:\n%s", server)
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
		{"control bytes in storage key", SidebarProps{NavLabel: "N", Collapse: "auto",
			CollapseStorageKey: "a\r\nb", Items: []SidebarItem{{Label: "A", Href: "/a"}}}},
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
