package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type ixList struct{ component.ContextOnly }

func (s *ixList) RenderCtx(context.Context) render.HTML { return render.Text("LIST") }

type ixDetail struct {
	component.ContextOnly
	id string
}

func (s *ixDetail) SetParams(p map[string]string)         { s.id = p["id"] }
func (s *ixDetail) RenderCtx(context.Context) render.HTML { return render.Text("DETAIL " + s.id) }
func (s *ixDetail) ScreenTitle() string                   { return "Product" }

func ixApp(t *testing.T) *App {
	t.Helper()
	a := NewApp("ix")
	a.Register("/products", &ixList{}, nil)
	a.Register("/products/:id", &ixDetail{}, nil,
		InterceptFrom("/products", ScreenDrawer))
	return a
}

func TestInterceptFromRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
	}{
		{"relative from", func() { InterceptFrom("products", ScreenDrawer) }},
		{"empty from", func() { InterceptFrom("", ScreenDrawer) }},
		{"page is not an overlay", func() { InterceptFrom("/products", ScreenPage) }},
		{"dialog is not an overlay", func() { InterceptFrom("/products", ScreenDialog) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a registration panic")
				}
			}()
			tc.fn()
		})
	}
}

// The client asks for an overlay; the server decides. Every origin that
// is not the declared one must fall back to the canonical full render.
func TestInterceptForOnlyMatchesDeclaredOrigin(t *testing.T) {
	a := ixApp(t)
	cases := []struct {
		name           string
		target, origin string
		want           bool
	}{
		{"declared origin", "/products/42", "/products", true},
		{"origin with query is the same screen", "/products/42", "/products?page=2&sort=name", true},
		{"different screen", "/products/42", "/products/9", false},
		{"unregistered origin", "/products/42", "/nowhere", false},
		{"empty origin", "/products/42", "", false},
		{"target has no intercept", "/products", "/products", false},
		{"unregistered target", "/nope/1", "/products", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ix, ok := a.Router.InterceptFor(tc.target, tc.origin)
			if ok != tc.want {
				t.Fatalf("InterceptFor(%q, %q) = %v, want %v", tc.target, tc.origin, ok, tc.want)
			}
			if ok && ix.As != ScreenDrawer {
				t.Fatalf("presentation = %v, want ScreenDrawer", ix.As)
			}
		})
	}
}

// The overlay is a different WRAPPER around the same render, same
// params, same Load, same content. If those diverged, a shared link and
// an intercepted click would show different things.
func TestOverlayRenderMatchesCanonicalContent(t *testing.T) {
	a := ixApp(t)
	ctx := context.Background()

	page, err := a.RenderPartialResult(ctx, "/products/42")
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	overlay, err := a.RenderOverlayResult(ctx, "/products/42", ScreenDrawer)
	if err != nil {
		t.Fatalf("overlay: %v", err)
	}

	// Params still reach the component through the overlay path.
	for _, got := range []string{string(page.HTML), string(overlay.HTML)} {
		if !strings.Contains(got, "DETAIL 42") {
			t.Errorf("render lost its params/content: %s", got)
		}
	}
	// A page partial is deliberately UNWRAPPED, the runtime swaps it
	// into the <main> that is already on the page. The overlay is the
	// same content plus drawer scaffolding, which is the entire
	// difference between the two renders.
	if strings.Contains(string(page.HTML), "role=") {
		t.Errorf("canonical partial should carry no wrapper: %s", page.HTML)
	}
	if !strings.Contains(string(overlay.HTML), `role="complementary"`) {
		t.Errorf("overlay missing drawer ARIA scaffolding: %s", overlay.HTML)
	}
	if !strings.Contains(string(overlay.HTML), `aria-label="Product"`) {
		t.Errorf("overlay drawer is unlabelled: %s", overlay.HTML)
	}
	// The title travels either way, the overlay needs it for its label.
	if page.Title != "Product" || overlay.Title != "Product" {
		t.Errorf("titles = %q / %q, want both %q", page.Title, overlay.Title, "Product")
	}
}

// Registration options outrank what the component declares about itself.
func TestRegisterAppliesScreenOptions(t *testing.T) {
	a := ixApp(t)
	scr, ok := a.Router.ScreenByPattern("/products/:id")
	if !ok {
		t.Fatal("screen not registered")
	}
	if scr.Intercept == nil {
		t.Fatal("InterceptFrom did not reach the screen")
	}
	if scr.Intercept.From != "/products" || scr.Intercept.As != ScreenDrawer {
		t.Fatalf("intercept = %+v", scr.Intercept)
	}
	// It stays a page registration: the canonical render is unchanged.
	if scr.Type != ScreenPage {
		t.Fatalf("intercepting screen must stay a page, got %v", scr.Type)
	}
}
