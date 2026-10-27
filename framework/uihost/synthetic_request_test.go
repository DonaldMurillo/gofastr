package uihost

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// echoRequest renders its request's path and query.
type echoRequest struct{ component.ContextOnly }

func (echoRequest) SetParams(map[string]string) {}

func (echoRequest) RenderCtx(ctx context.Context) render.HTML {
	r := app.RequestFromContext(ctx)
	return render.HTML("<p>path=" + render.Escape(r.URL.Path) + " query=" + render.Escape(r.URL.RawQuery) + "</p>")
}

// An export renders /items/a?b (ID "a?b") under a request made for it.
// Built from the raw routing path, the request saw path /items/a and
// query b, where the live URL /items/a%3Fb gives path /items/a?b.
func TestStaticRequestKeepsParamBytes(t *testing.T) {
	a := app.NewApp("t")
	a.Register("/items/:id", echoRequest{}, nil)
	ds := New(a)
	page, _, err := ds.RenderStaticPageResult(context.Background(), "/items/a?b")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(page, "path=/items/a?b query=<") {
		t.Errorf("request path is not the page's:\n%s", page)
	}
}

// The sitemap judges a page under a request for its URL: a policy that
// reads the request path saw /items/a for the page /items/a#b.
func TestSitemapRequestKeepsParamBytes(t *testing.T) {
	a := app.NewApp("t")
	a.RegisterScreen(app.NewScreen("/items/:id", echoRequest{}).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			if r := app.RequestFromContext(ctx); r == nil || r.URL.Path != "/items/a#b" {
				return app.Decision{Kind: app.DecisionBlock, Status: http.StatusNotFound}
			}
			return app.Decision{Kind: app.DecisionAllow}
		})), nil)
	ds := New(a, WithSitemap(SitemapConfig{BaseURL: "https://x.test"}))
	if !ds.sitemapPublic(context.Background(), "/items/a#b") {
		t.Error("sitemap judged /items/a#b under a request for another path")
	}
}
