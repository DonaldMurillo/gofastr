package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// authBaseSpec is an app whose auth battery mounts at /account, with a
// login screen, a signup screen, a gated screen and a header that signs
// out.
func authBaseSpec(base string) appspec.AppSpec {
	bp := appspec.AppSpec{}
	bp.App.Name = "acct"
	bp.App.Auth.Enabled = true
	bp.App.Auth.BasePath = base
	bp.App.Admin.SeedEmail = "admin@example.com"
	bp.App.Admin.SeedPassword = "admin-pw-123" // not-a-secret: fixture seed so the e2e and axe tests render their login
	bp.Nav = []appspec.AppSpecNavItem{{Label: "Dashboard", Href: "/dashboard"}}
	bp.Screens = []appspec.AppSpecScreen{
		{Name: "login", Route: "/login", Layout: "marketing", Body: []appspec.AppSpecBlock{{Kind: "login_form"}}},
		{Name: "signup", Route: "/signup", Body: []appspec.AppSpecBlock{{Kind: "signup_form"}}},
		{Name: "dashboard", Route: "/dashboard", Layout: "app", Access: appspec.AppSpecAccess{Auth: true}, Body: []appspec.AppSpecBlock{{Kind: "text", Text: "hi"}}},
	}
	return bp
}

// With base_path set, the battery mounts login, register and logout
// under it, and the generated app kept posting to /auth: Sign out
// answered 404 and the session stayed.
func TestAuthPathsFollowBasePath(t *testing.T) {
	bp := authBaseSpec("/account")
	login := renderAppSpecLoginFormExpr(bp, bp.Screens[0].Body[0])
	signup := renderAppSpecSignupFormExpr(bp, bp.Screens[1].Body[0])
	app := RenderAppSpecApp(bp)
	e2e := renderAppSpecE2ETest(bp)
	for _, c := range []struct{ name, src, want string }{
		{"login form", login, `"/account/login"`},
		{"signup form", signup, `"/account/register"`},
		{"header sign out", app, `actions = ui.SignOut(ui.SignOutConfig{Ctx: ctx, Next: "/", Action: "/account/logout"})`},
		{"sidebar sign out", app, `authAction = ui.SignOut(ui.SignOutConfig{Ctx: ctx, Next: "/", Action: "/account/logout"})`},
		{"e2e login", e2e, `client.PostForm(base+"/account/login"`},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s does not use the base path, want %s", c.name, c.want)
		}
	}
	for name, src := range map[string]string{"login form": login, "signup form": signup, "app": app, "e2e": e2e} {
		if strings.Contains(src, `"/auth/`) || strings.Contains(src, `+"/auth/`) {
			t.Errorf("%s still names /auth:\n%s", name, src)
		}
	}
}

// The PWA service worker must never cache or intercept the auth routes,
// and the axe test signs in before it audits the gated screens: both
// follow base_path.
func TestAuthBaseReachesPWAAndAxe(t *testing.T) {
	bp := authBaseSpec("/account")
	bp.App.PWA.Enabled = true
	if got := appSpecPWAOptionLiteral(bp); !strings.Contains(got, `DenyPaths: []string{"/account"}`) {
		t.Errorf("the PWA deny list misses /account: %s", got)
	}
	if got := renderAppSpecAxeTest(bp); !strings.Contains(got, `client.PostForm(base+"/account/login"`) {
		t.Error("the axe test does not sign in under /account")
	}
	// The framework's own deny list covers /auth already.
	if got := appSpecPWAOptionLiteral(authBaseSpec("")); strings.Contains(got, "DenyPaths") {
		t.Errorf("the default base adds a deny path: %s", got)
	}
}

// The default base keeps the output it had: SignOut's own default is
// /auth/logout, so it names no Action.
func TestAuthPathsDefaultBase(t *testing.T) {
	bp := authBaseSpec("")
	if got := renderAppSpecLoginFormExpr(bp, bp.Screens[0].Body[0]); !strings.Contains(got, `"/auth/login"`) {
		t.Errorf("login form: %s", got)
	}
	if got := renderAppSpecE2ETest(bp); !strings.Contains(got, `client.PostForm(base+"/auth/login"`) {
		t.Error("e2e login does not post to /auth/login")
	}
	if app := RenderAppSpecApp(bp); !strings.Contains(app, `ui.SignOut(ui.SignOutConfig{Ctx: ctx, Next: "/"})`) {
		t.Error("the default sign out changed")
	}
}
