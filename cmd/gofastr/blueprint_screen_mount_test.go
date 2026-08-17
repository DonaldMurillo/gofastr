package main

import (
	"strings"
	"testing"
)

// Screens mounted through app.NewScreen (the auth-gated and guest-policy
// paths in blueprintScreenMountStmt) do NOT get the component probed for
// ScreenDescriber the way site.Register does — uihost strict mode reads
// the Screen struct's Description field. A blueprint screen that declares
// a description must therefore chain WithDescription at the mount, or the
// generated app panics at boot: `screen "/app": no description`. This is
// exactly how the meridian blueprint rotted: it generated, it compiled,
// it died.
func TestAuthScreensMountWithDescription(t *testing.T) {
	login := BlueprintScreen{Name: "login", Route: "/login", Title: "Sign in",
		Description: "Sign in to your account.",
		Body:        []BlueprintBlock{{Kind: "login_form"}}}
	bp := Blueprint{App: BlueprintApp{Auth: BlueprintAuth{Enabled: true}}, Screens: []BlueprintScreen{login}}
	for _, tc := range []struct {
		name   string
		screen BlueprintScreen
	}{
		{"auth-gated", BlueprintScreen{
			Name: "dashboard", Route: "/app", Title: "Overview",
			Description: "Your revenue at a glance.",
			Access:      BlueprintAccess{Auth: true},
		}},
		{"guest login form", login},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mount := blueprintScreenMountStmt(tc.screen, bp)
			if !strings.Contains(mount, `.WithDescription("`+tc.screen.Description+`")`) {
				t.Errorf("mount statement drops the declared description (strict-mode boot panic):\n%s", mount)
			}
			if !strings.Contains(mount, `.WithTitle("`) {
				t.Errorf("mount statement lost its title:\n%s", mount)
			}
		})
	}
}
