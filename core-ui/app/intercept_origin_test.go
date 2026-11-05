package app

import (
	"slices"
	"testing"
)

func TestScreenInterceptOrigins(t *testing.T) {
	registrations := []struct {
		name     string
		register func(*App, *Screen)
	}{
		{"router", func(application *App, screen *Screen) { application.Router.Screen(screen, nil) }},
		{"app", func(application *App, screen *Screen) { application.RegisterScreen(screen, nil) }},
		{"intercept option", func(application *App, screen *Screen) {
			intercept := screen.Intercept
			application.Register(screen.Path, screen.Component, nil, InterceptFrom(intercept.From, intercept.As, intercept.AlsoFrom...))
		}},
		{"option", func(application *App, screen *Screen) {
			application.Register(screen.Path, screen.Component, nil, func(target *Screen) { target.Intercept = screen.Intercept })
		}},
		{"group", func(application *App, screen *Screen) {
			group := NewScreenGroup("/workspace", nil)
			screen.Path = "panel"
			group.SubGroup("nested", nil).Screen(screen, nil)
			application.Router.ScreenGroup(group)
		}},
	}
	for _, registration := range registrations {
		t.Run(registration.name, func(t *testing.T) {
			for _, test := range []struct{ pattern, path, canonical string }{
				{"/orders/{id}", "/orders/2", "/orders/:id"},
				{"/orders/:id", "/orders/3", "/orders/:id"},
				{"/teams/{team}/members/{id:int}", "/teams/west/members/7", "/teams/:team/members/:id:int"},
				{"/guides/{rest...}", "/guides/setup/linux", "/guides/:rest*"},
			} {
				for _, also := range []bool{false, true} {
					t.Run(test.pattern+map[bool]string{false: "/from", true: "/also"}[also], func(t *testing.T) {
						application := NewApp("intercept")
						application.Register(test.pattern, &ixDetail{}, nil)
						application.Register("/known", &ixList{}, nil)
						intercept := &Intercept{From: test.pattern, As: ScreenSheet}
						if also {
							intercept.From = "/known"
							intercept.AlsoFrom = []string{test.pattern}
						}
						screen := NewScreen("/panel", &ixList{})
						screen.Intercept = intercept
						registration.register(application, screen)
						application.Router.ValidateIntercepts()
						got, ok := application.Router.InterceptFor(screen.Path, test.path)
						if !ok || got.As != ScreenSheet || got.From != test.canonical && !slices.Contains(got.AlsoFrom, test.canonical) {
							t.Fatalf("origin %q did not produce normalized intercept %q: %+v, matched=%v", test.path, test.canonical, got, ok)
						}
					})
				}
			}
			for _, test := range []struct {
				name      string
				intercept Intercept
			}{
				{"empty from", Intercept{As: ScreenDrawer}},
				{"relative from", Intercept{From: "orders", As: ScreenDrawer}},
				{"relative also", Intercept{From: "/known", AlsoFrom: []string{"/orders/{id}", "orders"}, As: ScreenDrawer}},
				{"empty also", Intercept{From: "/known", AlsoFrom: []string{""}, As: ScreenDrawer}},
				{"page presentation", Intercept{From: "/known", As: ScreenPage}},
				{"dialog presentation", Intercept{From: "/known", As: ScreenDialog}},
			} {
				t.Run(test.name, func(t *testing.T) {
					defer func() {
						if recover() == nil {
							t.Fatal("invalid intercept was accepted at registration")
						}
					}()
					screen := NewScreen("/panel", &ixList{})
					screen.Intercept = &test.intercept
					registration.register(NewApp("invalid"), screen)
				})
			}
		})
	}
}
