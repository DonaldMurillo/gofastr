package app

import "testing"

func TestCatchAllRefusesEmptyRemainder(t *testing.T) {
	for _, pattern := range []string{"/{rest...}", "/docs/{rest...}"} {
		t.Run(pattern, func(t *testing.T) {
			router := NewRouter()
			router.Screen(NewScreen(pattern, &catchScreen{}), nil)
			for _, path := range []string{"/", "//", "///", "/docs", "/docs/", "/docs///"} {
				_, params, matched := router.Resolve(path)
				if matched && params["rest"] == "" {
					t.Errorf("%q matched %q with an empty remainder", pattern, path)
				}
			}
			prefix := ""
			if pattern == "/docs/{rest...}" {
				prefix = "/docs"
			}
			for _, remainder := range []string{"leaf", "leaf/twig", "%2F", "%252F"} {
				_, params, matched := router.Resolve(prefix + "/" + remainder)
				if !matched || params["rest"] == "" {
					t.Errorf("%q refused nonempty remainder %q", pattern, remainder)
				}
			}
		})
	}
	app := NewApp("redirect")
	app.RedirectPattern("/{rest...}", "/target/{rest...}")
	for _, path := range []string{"/", "//", "///"} {
		if target, ok := app.Router.resolveRedirectOnce(path); ok {
			t.Errorf("empty root remainder redirected %q to %q", path, target)
		}
	}
}
