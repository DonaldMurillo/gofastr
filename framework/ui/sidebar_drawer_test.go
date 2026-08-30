package ui

import (
	"strings"
	"testing"
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
	if !strings.Contains(out, `<ul class="ui-sidebar__sublist" id="workspace-nav-drawer-g1" hidden>`) {
		t.Errorf("drawer slot group container must carry the -drawer-prefixed id:\n%s", out)
	}
}
