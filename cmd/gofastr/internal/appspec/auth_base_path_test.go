package appspec

import (
	"strings"
	"testing"
)

// base_path is joined straight onto the battery's routes and the
// generated forms' actions, so only a clean absolute path is accepted.
func TestAuthBasePathMustBeClean(t *testing.T) {
	for _, p := range []string{"", "/auth", "/account", "/v1/auth", "/my-auth_2", "/a.b"} {
		bp := AppSpec{}
		bp.App.Auth.BasePath = p
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "base_path") {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range []string{"auth", "/auth/", "/", "//evil.example", "/a//b", "/..", "/a/.", "/.hidden", "/a b", "/a?x=1", "/a#b", `/a"b`} {
		bp := AppSpec{}
		bp.App.Auth.BasePath = p
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "base_path") {
			t.Errorf("%q accepted: %v", p, err)
		}
	}
}

// An explicit login_form/signup_form action is the one place the
// generated forms do not follow base_path: off the mounted route the
// form posts to a 404 and nobody can sign in.
func TestAuthFormActionFollowsBasePath(t *testing.T) {
	spec := func(base, kind, action string) AppSpec {
		bp := AppSpec{App: AppSpecApp{Name: "app", Module: "m", BaseURL: "https://app.example"}}
		bp.App.Auth.Enabled = true
		bp.App.Auth.DevMode = true
		bp.App.Auth.BasePath = base
		bp.Screens = []AppSpecScreen{{Name: "signin", Route: "/signin", Type: "page", Body: []AppSpecBlock{{
			Kind:     "section",
			Children: []AppSpecBlock{{Kind: kind, Props: map[string]any{"action": action}}},
		}}}}
		return bp
	}
	for _, c := range []struct{ base, kind, action string }{
		{"/account", "login_form", "/account/login"},
		{"/account", "signup_form", "/account/register"},
		{"/account", "login_form", "/account/login?next=/app"},
		{"", "login_form", "/auth/login"},
		{"/account", "login_form", "https://app.example/account/login"},
	} {
		if err := ValidateAppSpec(spec(c.base, c.kind, c.action)); err != nil {
			t.Errorf("%s action %q under base %q refused: %v", c.kind, c.action, c.base, err)
		}
	}
	for _, c := range []struct{ base, kind, action string }{
		{"/account", "login_form", "/auth/login"},
		{"/account", "signup_form", "/auth/register"},
		{"/account", "login_form", "/account/register"},
		{"/account", "signup_form", "/account/login"},
		{"", "login_form", "/login"},
		{"/account", "login_form", "https://app.example/auth/login"},
		{"/account", "login_form", "login"},
		{"/account", "login_form", "#"},
		{"/account", "login_form", "https://attacker.example/account/login"},
		{"/account", "login_form", "http://app.example/account/login"},
	} {
		err := ValidateAppSpec(spec(c.base, c.kind, c.action))
		if err == nil || !strings.Contains(err.Error(), "mounted") && !strings.Contains(err.Error(), "base_url") {
			t.Errorf("%s action %q under base %q accepted: %v", c.kind, c.action, c.base, err)
		}
	}
}

// An absolute action is checked against app.base_url, the app's own
// origin. With no base_url there is nothing to compare it to, so it is
// refused: the form would send the password wherever the URL names.
func TestAuthFormAbsoluteActionNeedsBaseURL(t *testing.T) {
	bp := AppSpec{App: AppSpecApp{Name: "app", Module: "m"}}
	bp.App.Auth.Enabled = true
	bp.App.Auth.DevMode = true
	bp.Screens = []AppSpecScreen{{Name: "signin", Route: "/signin", Type: "page", Body: []AppSpecBlock{
		{Kind: "login_form", Props: map[string]any{"action": "https://app.example/auth/login"}},
	}}}
	if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("absolute action with no app.base_url accepted: %v", err)
	}
}

// A fragment (generate app --add) carries no app: section, so it cannot
// say where the project mounted auth: an action at the project's real
// base is not refused against the /auth default.
func TestFragmentAuthActionNotRefused(t *testing.T) {
	bp := AppSpec{Screens: []AppSpecScreen{{Name: "signin", Route: "/signin", Type: "page", Body: []AppSpecBlock{
		{Kind: "login_form", Props: map[string]any{"action": "/account/login"}},
	}}}}
	bp.MarkFragment()
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("fragment login_form action /account/login refused: %v", err)
	}
}

// app.base_url is the sitemap origin, and the generated app's always-on
// strict mode panics at boot on one it cannot use: validate refuses the
// same shapes first.
func TestBaseURLMustBeHTTPOrigin(t *testing.T) {
	for _, u := range []string{"", "https://demo.example", "http://localhost:8080", "https://demo.example/"} {
		bp := AppSpec{}
		bp.App.BaseURL = strings.TrimRight(u, "/")
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "base_url") {
			t.Errorf("%q refused: %v", u, err)
		}
	}
	for _, u := range []string{"javascript:alert(1)//", "ftp://demo.example", "demo.example", "//demo.example", "https://", "https://u:p@demo.example", "https://demo.example?x=1", "https://demo.example#f", "https://demo.example/app"} {
		bp := AppSpec{}
		bp.App.BaseURL = strings.TrimRight(u, "/")
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "base_url") {
			t.Errorf("%q accepted: %v", u, err)
		}
	}
}

// admin.path is the admin battery's mount prefix, joined onto its routes
// the way base_path is: "/" trimmed to an empty prefix panicked the
// battery's Init and aborted the boot.
func TestAdminPathMustBeClean(t *testing.T) {
	for _, p := range []string{"", "/admin", "/back-office", "/ops/admin"} {
		bp := AppSpec{}
		bp.App.Admin.Path = p
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "admin.path") {
			t.Errorf("%q refused: %v", p, err)
		}
	}
	for _, p := range []string{"/", "admin", "/admin/", "//evil.example", "/a//b", "/..", "/a b", "/a?x=1"} {
		bp := AppSpec{}
		bp.App.Admin.Path = p
		if err := ValidateAppSpec(bp); err == nil || !strings.Contains(err.Error(), "admin.path") {
			t.Errorf("%q accepted: %v", p, err)
		}
	}
}

// A fragment (generate app --add) has no app section; its sign-in form is
// judged by the base path of the project it joins (SetProjectApp), and
// skipped only when none was read.
func TestFragmentAuthFormUsesProjectBase(t *testing.T) {
	frag := func(action string) AppSpec {
		bp := AppSpec{Screens: []AppSpecScreen{{Name: "signin", Route: "/signin", Type: "page", Body: []AppSpecBlock{{
			Kind: "login_form", Props: map[string]any{"action": action},
		}}}}}
		bp.MarkFragment()
		return bp
	}
	if err := ValidateAppSpec(frag("/auth/login")); err != nil {
		t.Fatalf("fragment with no project settings refused: %v", err)
	}
	project := AppSpecApp{APIPrefix: "api"}
	project.Auth.Enabled = true
	project.Auth.BasePath = "/account"
	bad := frag("/auth/login")
	bad.SetProjectApp(project)
	if err := ValidateAppSpec(bad); err == nil || !strings.Contains(err.Error(), "/account/login") {
		t.Errorf("fragment action /auth/login under project base /account: want refusal naming /account/login, got %v", err)
	}
	good := frag("/account/login")
	good.SetProjectApp(project)
	if err := ValidateAppSpec(good); err != nil {
		t.Errorf("fragment action /account/login under project base /account refused: %v", err)
	}
}
