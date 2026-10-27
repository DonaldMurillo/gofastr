package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type ledgerComp struct{}

func (*ledgerComp) SetParams(map[string]string) {}
func (*ledgerComp) Render() render.HTML         { return "<p>LEDGER body</p>" }
func (*ledgerComp) ScreenSEO(context.Context) SEO {
	return SEO{Description: "LEDGER seo", Canonical: "/acct"}
}

// A dynamic route's pattern doc names no URL, so the region guards that
// move its pages cannot run for it: a page under an outlet guard that
// sends visitors to /login serves the withheld doc, with no front matter.
func TestPatternDocRunsRegionGuards(t *testing.T) {
	aside := app.NewOutlet("aside", app.OutletOptions{Policy: app.PolicyFunc(func(context.Context) app.Decision {
		return app.Decision{Kind: app.DecisionRedirect, URL: "/login"}
	})})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a := app.NewApp("x")
	a.SetDefaultLayout(shell)
	scr := app.NewScreen("/acct/:id", &ledgerComp{})
	scr.Title = "Invoice LEDGER"
	a.RegisterScreen(scr, nil)
	ds := New(a, WithPublicLLMMD(), WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/acct/:id/llm.md", nil))
	if md := rec.Body.String(); strings.Contains(md, "LEDGER") || strings.HasPrefix(md, "---") {
		t.Errorf("pattern doc of a guarded page leaked:\n%s", md)
	}
}
