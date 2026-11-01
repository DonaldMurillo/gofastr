package headless

import (
	"strings"
	"testing"
)

// The phone's hamburger is the only way into the navigation, so an
// empty (or blank) DrawerLabel takes Strings.SidebarOpenDrawer instead
// of rendering a button a screen reader calls "button". Both renders
// of the trigger, the shell's and the standalone, and a translated
// Strings reaches both.
func TestSidebarDrawerTriggerFallsBackToStrings(t *testing.T) {
	items := []SidebarItem{{Label: "One", Href: "/1"}}
	es := &Strings{SidebarOpenDrawer: "Abrir navegación"}
	for name, got := range map[string]string{
		"shell":            renderSidebar(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", Items: items}),
		"shell blank":      renderSidebar(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", DrawerLabel: "  ", Items: items}),
		"standalone":       string(SidebarDrawerTrigger(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", Items: items}, nil)),
		"standalone blank": string(SidebarDrawerTrigger(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", DrawerLabel: "\t", Items: items}, nil)),
	} {
		if !strings.Contains(got, `aria-label="Open navigation"`) {
			t.Errorf("%s: an unlabelled drawer trigger is not named from Strings:\n%s", name, got)
		}
	}
	for name, got := range map[string]string{
		"shell":      string(Sidebar(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", Items: items, Strings: es}, nil)),
		"standalone": string(SidebarDrawerTrigger(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer", Items: items, Strings: es}, nil)),
	} {
		if !strings.Contains(got, `aria-label="Abrir navegación"`) {
			t.Errorf("%s: the drawer trigger's default name ignored the request's Strings:\n%s", name, got)
		}
	}
	// A caller's own label still wins.
	own := string(SidebarDrawerTrigger(SidebarProps{NavLabel: "N", DrawerName: "nav-drawer",
		DrawerLabel: "Menu", Items: items, Strings: es}, nil))
	if !strings.Contains(own, `aria-label="Menu"`) {
		t.Errorf("the caller's DrawerLabel lost to the default:\n%s", own)
	}
}
