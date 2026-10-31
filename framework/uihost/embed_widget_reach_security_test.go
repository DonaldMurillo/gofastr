package uihost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A widget on an embedded screen loads its style, state and chrome from
// /core-ui/widget/<name>/…, and the embed middleware refused every one
// of those paths with 403 before the widget's own gate ran: the runtime
// reach listed /__gofastr/widget, where no widget mounts. The widget
// paths now pass for a widget available on the grant's own surface and
// stay refused for any other.
func TestEmbedGrantReachesItsSurfaceWidgets(t *testing.T) {
	only := func(path string) []widget.RouteMatcher {
		return []widget.RouteMatcher{func(p string) bool { return p == path }}
	}
	rt := router.New()
	widget.Mount(rt, &widget.Definition{Name: "u31-mine", Routes: only("/reports")})
	widget.Mount(rt, &widget.Definition{Name: "u31-theirs", Routes: only("/admin")})
	widget.Mount(rt, &widget.Definition{Name: "u31-gated", Routes: only("/reports"), RequireSession: true})

	f := newEmbedFixture(t)
	grant := f.grantFor(t, "reports")
	h := f.embed.Middleware()(rt)

	for _, c := range []struct {
		path string
		want int
	}{
		{"/core-ui/widget/u31-mine/style.css", http.StatusOK},
		{"/core-ui/widget/u31-mine/chrome", http.StatusOK},
		{"/core-ui/widget/u31-gated/chrome", http.StatusOK},
		{"/core-ui/widget/u31-theirs/style.css", http.StatusForbidden},
		{"/core-ui/widget/u31-theirs/chrome", http.StatusForbidden},
		{"/core-ui/widget/u31-nope/style.css", http.StatusForbidden},
		{"/core-ui/widget/u31-mine/other", http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		req.Header.Set("X-Gofastr-Embed", grant)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("GET %s with a /reports grant: status %d, want %d", c.path, rec.Code, c.want)
		}
	}
}
