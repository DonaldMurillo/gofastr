package hosttest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

type constrainedPrefetchScreen struct{ body render.HTML }

func (*constrainedPrefetchScreen) SetParams(map[string]string) {}
func (s *constrainedPrefetchScreen) Render() render.HTML       { return s.body }

func TestHoverPrefetchUsesServerRouteConstraints(t *testing.T) {
	a := app.NewApp("preload-constraints")
	a.Register("/hub", app.NewStaticComponent(`
		<a id="number" href="/items/%37">Number</a>
		<a id="slug" href="/items/help?tab=about">Help</a>
	`), nil)
	a.Register("/items/{id:int}", &constrainedPrefetchScreen{body: `<h1 id="number-screen">Number</h1>`}, nil,
		app.Preload("hover"), app.InterceptFrom("/hub", app.ScreenDrawer))
	a.Register("/items/{slug}", &constrainedPrefetchScreen{body: `<h1 id="slug-screen">Help</h1>`}, nil, app.Preload("hover"))

	rt := router.New()
	uihost.New(a).Mount(rt)
	type request struct {
		uri, prefetch, intercept string
	}
	var mu sync.Mutex
	var requests []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/items/help" || r.URL.Path == "/items/7" {
			mu.Lock()
			requests = append(requests, request{r.URL.RequestURI(), r.Header.Get("X-Gofastr-Prefetch"), r.Header.Get("X-Gofastr-Intercept")})
			mu.Unlock()
		}
		rt.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	hover := func(id string) chromedp.Action {
		return chromedp.ActionFunc(func(ctx context.Context) error {
			var center struct{ X, Y float64 }
			if err := chromedp.Evaluate(`(() => {
				const r = document.getElementById('`+id+`').getBoundingClientRect();
				return { X: r.x + r.width/2, Y: r.y + r.height/2 };
			})()`, &center).Do(ctx); err != nil {
				return err
			}
			return input.DispatchMouseEvent(input.MouseMoved, center.X, center.Y).Do(ctx)
		})
	}
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.Poll(`window.__gofastr.loadedModules?.preload && window.__gofastr.loadedModules?.intercept`, nil),
		hover("number"),
		hover("slug"),
	); err != nil {
		t.Fatal(err)
	}
	// Wait for the hover response before clicking, so a correct click can consume it.
	err := chromedp.Run(ctx, chromedp.Poll(`performance.getEntriesByName(location.origin + '/items/help?tab=about').length > 0`, nil,
		chromedp.WithPollingTimeout(2*time.Second)))
	if err != nil {
		t.Errorf("hover did not prefetch the slug route: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click("#slug"), chromedp.WaitVisible("#slug-screen")); err != nil {
		t.Fatal(err)
	}

	var inDrawer bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.Poll(`window.__gofastr.loadedModules?.preload && window.__gofastr.loadedModules?.intercept`, nil),
		chromedp.Click("#number"),
		chromedp.WaitVisible("#number-screen"),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-intercept-as="drawer"] #number-screen')`, &inDrawer),
	); err != nil {
		t.Fatal(err)
	}
	if !inDrawer {
		t.Error("clicking /items/%37 did not open the integer route in a drawer")
	}

	mu.Lock()
	defer mu.Unlock()
	for _, req := range requests {
		t.Logf("GET %s X-Gofastr-Prefetch=%q X-Gofastr-Intercept=%q", req.uri, req.prefetch, req.intercept)
	}
	want := request{"/items/help?tab=about", "1", ""}
	drawer := request{"/items/%37", "", "1"}
	if len(requests) != 2 || requests[0] != want || requests[1] != drawer {
		t.Errorf("browser requests = %+v, want one hover prefetch %+v and one drawer request %+v", requests, want, drawer)
	}
}
