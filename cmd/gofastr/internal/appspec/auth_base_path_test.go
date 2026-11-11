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
		bp := AppSpec{App: AppSpecApp{Name: "app", Module: "m"}}
		bp.App.Auth.Enabled = true
		bp.App.Auth.DevMode = true
		bp.App.Auth.BasePath = base
		bp.Screens = []AppSpecScreen{{Name: "signin", Route: "/signin", Type: "page", Body: []AppSpecBlock{{
			Type:     "section",
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
	} {
		err := ValidateAppSpec(spec(c.base, c.kind, c.action))
		if err == nil || !strings.Contains(err.Error(), "mounted") {
			t.Errorf("%s action %q under base %q accepted: %v", c.kind, c.action, c.base, err)
		}
	}
}
