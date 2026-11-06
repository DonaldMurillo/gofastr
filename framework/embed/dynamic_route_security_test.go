package embed

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestEmbedRejectsDynamicRoutes(t *testing.T) {
	for _, route := range []string{"/reports/:id", "/reports/:id:int", "/teams/{team}/summary", "/catalog/{slug:string}"} {
		t.Run(route, func(t *testing.T) {
			host, err := New(Config{
				Surfaces: []Surface{{
					Name:    "customer-summary",
					Screen:  testScreen{route},
					Origins: []string{"https://customer.example"},
				}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err == nil || host != nil {
				t.Fatalf("New accepted dynamic screen route %q", route)
			}
			for _, detail := range []string{"customer-summary", route, "dynamic", "static"} {
				if !strings.Contains(err.Error(), detail) {
					t.Errorf("boot error %q does not name %q", err, detail)
				}
			}
		})
	}
	for _, route := range []string{"/reports/monthly", "/teams/overview/", "/catalog/customer-summary"} {
		t.Run(route, func(t *testing.T) {
			_, err := New(Config{
				Surfaces:  []Surface{{Name: "summary", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("New refused static screen route: %v", err)
			}
		})
	}
}

// The router decides what is dynamic: only a whole ":name" or "{name}"
// segment captures. A literal brace or colon inside a segment is a
// static path the router matches exactly, and embed must accept it.
func TestEmbedAcceptsLiteralBraceRoutes(t *testing.T) {
	for _, route := range []string{"/reports/rev{1}", "/reports/{}", "/reports/a:b"} {
		t.Run(route, func(t *testing.T) {
			application := app.NewApp("literal")
			screen := app.NewScreen(route, app.NewContextComponent(func(context.Context) render.HTML { return "static" }))
			application.RegisterScreen(screen, nil)
			got, params, ok := application.Router.Resolve(route)
			if !ok || got != screen || len(params) != 0 {
				t.Fatalf("router does not treat %q as static: ok=%v params=%v", route, ok, params)
			}
			if _, _, ok := application.Router.Resolve("/reports/other"); ok {
				t.Fatalf("router matched /reports/other against %q", route)
			}
			if _, err := New(Config{
				Surfaces:  []Surface{{Name: "literal", Screen: screen, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			}); err != nil {
				t.Fatalf("New refused static screen route: %v", err)
			}
		})
	}
}
