package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// nfCtxScreen is a 404 screen written the documented way for a
// context-reading component: ContextOnly plus RenderCtx.
type nfCtxScreen struct{ component.ContextOnly }

func (nfCtxScreen) RenderCtx(ctx context.Context) render.HTML {
	if ctx == nil {
		return ""
	}
	return render.HTML("<p>ctx-404</p>")
}

// nfBothScreen implements RenderNotFound and RenderCtx; the path-aware
// one wins.
type nfBothScreen struct{ component.ContextOnly }

func (nfBothScreen) RenderCtx(context.Context) render.HTML { return render.HTML("<p>ctx-body</p>") }
func (nfBothScreen) RenderNotFound(path string) render.HTML {
	return render.HTML("<p>nf-body</p>")
}

// A custom 404 screen built on component.ContextOnly rendered an empty
// body: the adapter called Render, which ContextOnly answers with "".
// An app with no root layout ignored the configured screen altogether
// and served the built-in body, though WithNotFoundScreen promises the
// bare <main> then.
func TestNotFoundScreenRendersCtx(t *testing.T) {
	newApp := func(layout bool) *app.App {
		a := app.NewApp("nf-ctx")
		if layout {
			a.SetDefaultLayout(headerLayout("main", &testHeaderComp{}))
		}
		a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
		return a
	}
	for _, layout := range []bool{true, false} {
		for _, c := range []struct {
			screen     component.Component
			want, deny string
		}{
			{nfCtxScreen{}, "ctx-404", "No route matched"},
			{nfBothScreen{}, "nf-body", "ctx-body"},
		} {
			ds := New(newApp(layout), WithNotFoundScreen(c.screen))
			w := httptest.NewRecorder()
			ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
			body := w.Body.String()
			if w.Code != http.StatusNotFound || !strings.Contains(body, c.want) {
				t.Errorf("layout=%v %T: status %d, want 404 with %q:\n%s", layout, c.screen, w.Code, c.want, body)
			}
			if strings.Contains(body, c.deny) {
				t.Errorf("layout=%v %T: body carries %q", layout, c.screen, c.deny)
			}
		}
	}

	// A screen that panics on the bare path serves the built-in body.
	ds := New(newApp(false), WithNotFoundScreen(panicScreen{}))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/nowhere", nil))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "No route matched") {
		t.Errorf("panicking screen, no layout: status %d, want 404 with the built-in body:\n%s", w.Code, w.Body.String())
	}
}
