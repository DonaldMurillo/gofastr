package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type statusCodeComp struct{ code int }

func (c *statusCodeComp) Render() render.HTML   { return render.HTML("<p>x</p>") }
func (c *statusCodeComp) ScreenStatusCode() int { return c.code }

// TestPageStatusIgnoresOutOfRangeScreenCode: app.SetStatus ignores a
// code outside 100-999, ScreenStatusCode did not, and net/http panics on
// one in WriteHeader. It is ignored now, like SetStatus's.
func TestPageStatusIgnoresOutOfRangeScreenCode(t *testing.T) {
	for _, code := range []int{42, 1000, -1} {
		if got := pageStatus(app.RenderResult{Component: &statusCodeComp{code: code}}, 0); got != 0 {
			t.Errorf("ScreenStatusCode %d: page status %d, want 0 (ignored)", code, got)
		}
	}
	if got := pageStatus(app.RenderResult{Component: &statusCodeComp{code: http.StatusTeapot}}, 0); got != http.StatusTeapot {
		t.Errorf("a valid ScreenStatusCode: got %d, want 418", got)
	}
}

// TestNegotiatedMarkdownBlockStatusClamped: a block decision answers
// its negotiated markdown as writeBlock answers the HTML. A Block whose
// status is outside 400-599 sent its withheld doc as 200 (status 0) or
// under a success status; it is a 500, as i18n.HTTPError sends it.
func TestNegotiatedMarkdownBlockStatusClamped(t *testing.T) {
	for _, status := range []int{0, http.StatusOK} {
		a := app.NewApp("dyn")
		a.Register("/", &testHomeComp{}, nil)
		block := app.PolicyFunc(func(context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionBlock, Status: status}
		})
		a.RegisterScreen(app.NewScreen("/secret", &secretRenderComp{}).WithPolicy(block), nil)
		ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())

		md := httptest.NewRequest(http.MethodGet, "/secret", nil)
		md.Header.Set("Accept", "text/markdown")
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, md)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Block(%d) markdown: status %d, want 500", status, rec.Code)
		}
		html := httptest.NewRecorder()
		ds.ServeHTTP(html, httptest.NewRequest(http.MethodGet, "/secret", nil))
		if html.Code != rec.Code {
			t.Errorf("Block(%d): HTML answered %d, markdown %d", status, html.Code, rec.Code)
		}
	}
}
