package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// A screen with no title loads as <title>AppName</title>. Its navigation
// answer sent no X-Gofastr-Title, and the runtime then keeps
// document.title: the tab went on naming the page the reader left. The
// RenderAlt arm (an auth wall's alt with no title) was the same shape.
// The server now always names a title, falling back to the app name as
// the full page does.
func TestPartialNavUntitledScreenTitledByAppName(t *testing.T) {
	refuse := app.PolicyFunc(func(context.Context) app.Decision {
		return decide.RenderAlt(func() component.Component { return &textComp{tag: "sign-in"} })
	})
	a := app.NewApp("Acme")
	a.Register("/", &testHomeComp{}, nil)
	a.RegisterScreen(app.NewScreen("/settings", &testHomeComp{}).WithTitle("Settings"), nil)
	a.Register("/plain", &textComp{tag: "plain"}, nil)
	a.Register("/guarded", &textComp{tag: "guarded"}, nil, func(s *app.Screen) { s.WithTitle("Guarded").WithPolicy(refuse) })
	ds := New(a)

	for _, tc := range []struct{ path, want string }{
		{"/plain", "Acme"},
		{"/guarded", "Acme"},
		{"/settings", "Settings — Acme"},
	} {
		full := httptest.NewRecorder()
		ds.ServeHTTP(full, httptest.NewRequest(http.MethodGet, tc.path, nil))
		body := full.Body.String()
		i, j := strings.Index(body, "<title>"), strings.Index(body, "</title>")
		if i < 0 || j < i {
			t.Fatalf("%s: setup: no <title> in full page", tc.path)
		}
		if got := body[i+len("<title>") : j]; got != tc.want {
			t.Fatalf("%s: setup: full <title> = %q, want %q", tc.path, got, tc.want)
		}

		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-From", "/settings")
		nav := httptest.NewRecorder()
		ds.ServeHTTP(nav, req)
		raw := nav.Header().Get("X-Gofastr-Title")
		got, err := url.PathUnescape(raw)
		if err != nil {
			t.Fatalf("%s: X-Gofastr-Title %q does not decode: %v", tc.path, raw, err)
		}
		if got != tc.want {
			t.Errorf("%s: navigation X-Gofastr-Title = %q, want %q (the full load's title)", tc.path, got, tc.want)
		}
	}
}
