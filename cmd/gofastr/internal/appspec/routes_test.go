package appspec

import (
	"slices"
	"testing"
)

func TestRouteParamNamesReadBothSyntaxes(t *testing.T) {
	for route, want := range map[string][]string{
		"/posts/{slug}":           {"slug"},
		"/files/{path...}":        {"path"},
		"/items/{id:int}":         {"id"},
		"/posts/:slug":            {"slug"},
		"/files/:path*":           {"path"},
		"/items/:id:int":          {"id"},
		"/orgs/{org}/users/:user": {"org", "user"},
		"/plain/{}/x":             nil,
		"/plain/:":                nil,
	} {
		if got := RouteParamNames(route); !slices.Equal(got, want) {
			t.Errorf("RouteParamNames(%q) = %q, want %q", route, got, want)
		}
	}
}

func TestRouteShapeKeepsConstraintsAndCatchAll(t *testing.T) {
	if RouteShape("/posts/{id}") != RouteShape("/posts/:slug") {
		t.Error("two param names in one position must share a shape")
	}
	if RouteShape("/posts/{id:int}") == RouteShape("/posts/{id}") {
		t.Error("a typed constraint must change the shape")
	}
	if RouteShape("/files/{path...}") == RouteShape("/files/{path}") {
		t.Error("a catch-all must change the shape")
	}
}

func TestScreenParamNamePrefersIDThenTheLastParam(t *testing.T) {
	for route, want := range map[string]string{
		"/posts/{id}/comments/{comment}":                "id",
		"/organizations/{organization_id}/users/{user}": "user",
		"/about": "id",
	} {
		if got := ScreenParamName(AppSpecScreen{Route: route}); got != want {
			t.Errorf("ScreenParamName(%q) = %q, want %q", route, got, want)
		}
	}
}
