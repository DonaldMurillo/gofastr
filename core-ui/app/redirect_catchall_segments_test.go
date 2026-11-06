package app

import "testing"

func TestRedirectKeepsCatchAllSegments(t *testing.T) {
	app := NewApp("redirect")
	app.Register("/help/{rest...}", &catchScreen{}, nil)
	app.RedirectPattern("/docs/{rest...}", "/help/{rest...}")
	for source, want := range map[string]string{
		"/docs/red%2Foak":          "/help/red%2Foak",
		"/docs/red/oak":            "/help/red/oak",
		"/docs/red%2Foak/leaves":   "/help/red%2Foak/leaves",
		"/docs/red%252Foak/leaves": "/help/red%252Foak/leaves",
		"/docs/50%25/a%3Fb%23c":    "/help/50%25/a%3Fb%23c",
		"/docs/%2F/%252F":          "/help/%2F/%252F",
	} {
		t.Run(source, func(t *testing.T) {
			got, ok := app.ResolveRedirect(RoutePath(source))
			if !ok || got != want {
				t.Fatalf("redirect = %q, %v; want %q", got, ok, want)
			}
			_, params, ok := app.Router.Resolve(RoutePath(got))
			if !ok || params["rest"] == "" {
				t.Fatalf("redirect target does not resolve: %q, %v", got, params)
			}
		})
	}
}

func TestRedirectKeepsParamBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, from, to, source, want string
	}{
		{"single to rest", "/old/{key}", "/new/{key...}", "/old/red%2Foak", "/new/red%2Foak"},
		{"rest to single", "/old/{key...}", "/new/{key}", "/old/red%2Foak/leaf", "/new/red%2Foak%2Fleaf"},
		{"prefix and rest", "/old/{locale}/{key...}", "/new/{locale}/{key...}", "/old/en%2FUS/red%2Foak/leaf", "/new/en%2FUS/red%2Foak/leaf"},
		{"constrained prefix", "/old/{id:int}/{key...}", "/new/{id}/{key...}", "/old/%31/red%2Foak", "/new/1/red%2Foak"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := NewApp("redirect")
			app.RedirectPattern(test.from, test.to)
			got, ok := app.ResolveRedirect(RoutePath(test.source))
			if !ok || got != test.want {
				t.Fatalf("redirect = %q, %v; want %q", got, ok, test.want)
			}
		})
	}
	app := NewApp("redirect")
	app.RedirectPattern("/docs/{rest...}", "/help/{rest...}")
	app.RedirectPattern("/help/{rest...}", "/archive/{rest...}")
	got, ok := app.ResolveRedirect(RoutePath("/docs/red%2Foak/leaf"))
	if !ok || got != "/archive/red%2Foak/leaf" {
		t.Fatalf("chained redirect = %q, %v; lost source segment boundaries", got, ok)
	}
}
