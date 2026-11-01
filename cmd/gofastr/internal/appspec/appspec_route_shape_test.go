package appspec

import (
	"strings"
	"testing"
)

// The router matches dynamic screens in registration order, so two
// routes that differ only in a parameter's name (or its syntax) leave
// the second screen unreachable.
func TestRoutesDifferingOnlyInParamRefused(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"/posts/{id}", "/posts/{slug}"},
		{"/posts/{id}", "/posts/:id"},
		{"/files/{path...}", "/files/:rest*"},
		{"/n/{id:int}", "/n/:num:int"},
	} {
		bp := AppSpec{Screens: []AppSpecScreen{
			{Name: "first", Route: tc.a, Type: "page"},
			{Name: "second", Route: tc.b, Type: "page"},
		}}
		err := ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), "differ only in parameter names") {
			t.Errorf("%s vs %s: want a shadowed-route error, got %v", tc.a, tc.b, err)
		}
	}
}

// Different shapes stay distinct: a constraint or a catch-all is a
// different match, and so is a literal segment.
func TestRoutesWithDistinctShapesAllowed(t *testing.T) {
	for _, tc := range []struct{ a, b string }{
		{"/posts/{id}", "/posts/new"},
		{"/posts/{id:int}", "/posts/{slug}"},
		{"/posts/{id}", "/posts/{rest...}"},
	} {
		bp := AppSpec{Screens: []AppSpecScreen{
			{Name: "first", Route: tc.a, Type: "page"},
			{Name: "second", Route: tc.b, Type: "page"},
		}}
		if err := ValidateAppSpec(bp); err != nil && strings.Contains(err.Error(), "differ only in parameter names") {
			t.Errorf("%s vs %s: refused distinct shapes: %v", tc.a, tc.b, err)
		}
	}
}
