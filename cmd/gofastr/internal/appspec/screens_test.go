package appspec

import (
	"testing"
)

func listScreen(name, route string, create bool) AppSpecScreen {
	return AppSpecScreen{Name: name, Route: route, Body: []AppSpecBlock{{Kind: "entity_list", Entity: "posts", Create: create}}}
}

func TestSynthesizedCreateScreenHangsOffTheHomeList(t *testing.T) {
	home := listScreen("posts", "/posts", true)
	home.Layout, home.Access = "marketing", AppSpecAccess{Auth: true, Role: "editor"}
	bp := AppSpec{Screens: []AppSpecScreen{listScreen("dashboard", "/", false), home}}
	got := AppSpecSynthesizeCRUDScreens(bp).Screens
	if len(got) != 3 {
		t.Fatalf("got %d screens, want the two authored and one create screen", len(got))
	}
	c := got[2]
	if c.Route != "/posts/create" || c.Layout != "marketing" || c.Access != home.Access || !IsEntityCreateBlock(c.Body[0]) {
		t.Fatalf("create screen = %+v; want /posts/create with the home list's layout and access", c)
	}
	bp.Screens = append(bp.Screens, AppSpecScreen{Name: "custom", Route: "/posts/create"})
	if n := len(AppSpecSynthesizeCRUDScreens(bp).Screens); n != 3 {
		t.Fatalf("got %d screens; a create screen must not be added where a screen already sits", n)
	}
}

func TestSynthesizeLeavesTheCallersArrayAlone(t *testing.T) {
	screens := make([]AppSpecScreen, 1, 4)
	screens[0] = listScreen("posts", "/posts", true)
	AppSpecSynthesizeCRUDScreens(AppSpec{Screens: screens})
	if spare := screens[:2][1]; spare.Route != "" {
		t.Fatalf("synthesis wrote %q into the caller's spare capacity", spare.Route)
	}
}

func TestHomeListPrefersTheDetailBaseThenCreate(t *testing.T) {
	detail := AppSpecScreen{Name: "post", Route: "/all-posts/{id}", Body: []AppSpecBlock{{Kind: "entity_detail", Entity: "posts"}}}
	bp := AppSpec{Screens: []AppSpecScreen{listScreen("first", "/first", false), listScreen("creating", "/creating", true), listScreen("all", "/all-posts", false), detail}}
	if got := AppSpecEntityListScreen(bp, "posts"); got == nil || got.Route != "/all-posts" {
		t.Fatalf("home list = %+v, want the list the detail screen sits under", got)
	}
	bp.Screens = bp.Screens[:3]
	if got := AppSpecEntityListScreen(bp, "posts"); got == nil || got.Route != "/creating" {
		t.Fatalf("home list = %+v, want the first list with create: true", got)
	}
	bp.Screens = []AppSpecScreen{listScreen("first", "/first", false), listScreen("second", "/second", false)}
	if got := AppSpecEntityListScreen(bp, "posts"); got == nil || got.Route != "/first" {
		t.Fatalf("home list = %+v, want the first list", got)
	}
}

func TestLoginScreenIsSynthesizedOnlyWhenNoneExists(t *testing.T) {
	auth := AppSpecApp{Auth: AppSpecAuth{Enabled: true, BasePath: "/account"}}
	signup := AppSpecScreen{Name: "join", Route: "/join", Layout: "marketing", Body: []AppSpecBlock{{Kind: "signup_form"}}}
	bp := AppSpec{App: auth, Screens: []AppSpecScreen{signup}}
	s, ok := AppSpecLoginScreen(bp)
	if !ok || s.Route != "/login" || s.Layout != "marketing" || s.Body[0].Props["action"] != "/account/login" || s.Body[0].Props["register_href"] != "/join" {
		t.Fatalf("login screen = %+v, %v; want /login in the marketing layout posting to /account/login and linking /join", s, ok)
	}
	if !IsSynthesizedLoginScreen(bp, s) {
		t.Fatal("the synthesized screen must read back as synthesized")
	}
	edited := s
	edited.Title = "Welcome back"
	if IsSynthesizedLoginScreen(bp, edited) {
		t.Fatal("an edited login screen must not read back as synthesized")
	}
	for name, screens := range map[string][]AppSpecScreen{
		"a screen named login": {{Name: "Login", Route: "/sign-in"}},
		"a screen at /login":   {{Name: "enter", Route: "/login"}},
		"a login_form":         {{Name: "enter", Route: "/enter", Body: []AppSpecBlock{{Kind: "login_form"}}}},
	} {
		if _, ok := AppSpecLoginScreen(AppSpec{App: auth, Screens: screens}); ok {
			t.Errorf("%s: a login screen was synthesized anyway", name)
		}
	}
	if _, ok := AppSpecLoginScreen(AppSpec{}); ok {
		t.Error("a login screen was synthesized with auth off")
	}
}
