package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// TestSidebarNativeMobileWrapsMarkerOnce pins the NativeMobile branch's
// marker scoping: the inline root carries data-fui-comp="ui-sidebar"
// (its display:contents rule and the sheet's scoped selectors key on
// it), the native mobile region carries its own (it sits outside the
// inline root and needs the sheet's link styling), and the neutral
// fui-sidebar-native wrapper carries NONE — the branch used to wrap the
// wrapper as well, emitting a redundant ancestor marker directly above
// the real one.
func TestSidebarNativeMobileWrapsMarkerOnce(t *testing.T) {
	out := string(component.RenderComponent(Sidebar(SidebarConfig{
		NativeMobile: true,
		Items:        []SidebarItem{{Label: "Home", Href: "/"}},
	})))
	const marker = `data-fui-comp="ui-sidebar"`
	if n := strings.Count(out, marker); n != 2 {
		t.Fatalf("NativeMobile sidebar carries %d %s markers, want exactly 2 (inline root + mobile region):\n%.400s", n, marker, out)
	}
	// The neutral wrapper must be the marker-free outer div.
	root := out[:strings.Index(out, ">")+1]
	if strings.Contains(root, marker) {
		t.Fatalf("the fui-sidebar-native wrapper itself carries the marker:\n%s", root)
	}
	if !strings.Contains(root, `class="fui-sidebar-native"`) {
		t.Fatalf("outer wrapper lost its class:\n%s", root)
	}
}
