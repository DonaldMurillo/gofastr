package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Roles are request data: a sidebar filtered by them must render for
// every viewer, including one whose roles match nothing.
func withRoles(t *testing.T, roles ...string) {
	t.Helper()
	prev := rolesExtractor
	SetRolesExtractor(func(context.Context) []string { return roles })
	t.Cleanup(func() { rolesExtractor = prev })
}

func TestSidebarEveryItemGatedAwayRendersNothing(t *testing.T) {
	withRoles(t, "guest")
	cfg := SidebarConfig{Items: []SidebarItem{
		{Label: "Admin", Href: "/admin", Roles: []string{"admin"}},
		{Label: "Billing", Href: "/billing", Roles: []string{"admin"}},
	}}
	if got := component.RenderComponentCtx(context.Background(), Sidebar(cfg)); got != "" {
		t.Errorf("a sidebar with every entry gated away rendered %q, want nothing", got)
	}
	if got := (sidebarDrawerSlot{cfg: cfg}).RenderCtx(context.Background()); got != "" {
		t.Errorf("the drawer slot with every entry gated away rendered %q, want nothing", got)
	}
}

func TestSidebarGroupWithEveryChildGatedAwayIsDropped(t *testing.T) {
	withRoles(t, "guest")
	cfg := SidebarConfig{Items: []SidebarItem{
		{Label: "Home", Href: "/"},
		{Label: "Settings", Children: []SidebarItem{
			{Label: "Users", Href: "/settings/users", Roles: []string{"admin"}},
		}},
	}}
	got := string(component.RenderComponentCtx(context.Background(), Sidebar(cfg)))
	if !strings.Contains(got, `href="/"`) {
		t.Fatalf("the visible entry is missing:\n%s", got)
	}
	if strings.Contains(got, "Settings") {
		t.Errorf("a group with every child gated away still renders:\n%s", got)
	}
}

func TestSidebarVisibleEntriesStillRenderForTheRightRole(t *testing.T) {
	withRoles(t, "admin")
	cfg := SidebarConfig{Items: []SidebarItem{
		{Label: "Admin", Href: "/admin", Roles: []string{"admin"}},
	}}
	got := string(component.RenderComponentCtx(context.Background(), Sidebar(cfg)))
	if !strings.Contains(got, `href="/admin"`) {
		t.Errorf("an admin lost the admin entry:\n%s", got)
	}
}
