package uihost_test

// PROTOTYPE (spike/layout-resolve): the partial answer's
// X-Gofastr-Transition carries the picked keyed-transition name.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type plainComp struct{ html string }

func (c *plainComp) Render() render.HTML { return render.Text(c.html) }

func TestTransitionHeaderOnTheWire(t *testing.T) {
	build := func(tf func(ctx context.Context) string) *app.App {
		a := app.NewApp("t")
		shell := app.NewLayout("shell", app.LayoutSpec{
			Primary: app.PrimaryConfig{
				Transitions: map[string]app.Transition{
					"slide": app.Slide(app.Right, 120*time.Millisecond),
				},
				TransitionFor: tf,
			},
		}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return l.Primary()
		})
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/", &plainComp{html: "HOME"}), nil)
		a.RegisterScreen(app.NewScreen("/s", &plainComp{html: "S"}), nil)
		return a
	}
	pick := func(ctx context.Context) string { return "slide" }
	host := uihost.New(build(pick), uihost.WithNoLiveChannel())

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-From", "/")
		rec := httptest.NewRecorder()
		host.ServeHTTP(rec, req)
		return rec
	}
	if got := get("/s").Header().Get("X-Gofastr-Transition"); got != "slide" {
		t.Fatalf("X-Gofastr-Transition = %q, want slide", got)
	}
	// Nothing picked (an unknown name resolves to no transition): the
	// header is absent, never an empty value.
	none := func(ctx context.Context) string { return "nosuch" }
	host2 := uihost.New(build(none), uihost.WithNoLiveChannel())
	req := httptest.NewRequest(http.MethodGet, "/s", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-From", "/")
	rec := httptest.NewRecorder()
	host2.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Gofastr-Transition"); got != "" {
		t.Fatalf("an unknown pick carries no header, got %q", got)
	}
}

var _ component.Component = (*plainComp)(nil)
