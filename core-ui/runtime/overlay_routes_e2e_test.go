package runtime_test

// TestOverlayDoesNotMoveRouteSignals (docs/DESIGN-layout-outlets.md
// "Route signals": "Never at click time, never on an intercept overlay
// mount — the overlay does not activate a route"): a drawer-intercept
// navigation mounts the overlay chrome without touching the page's
// route.* state. The kept shell's route.title binding still shows the
// underlying page's title, the store's route.path still names the
// page, and the overlay request carries no X-Gofastr-Fills header (its
// body stays bare — the page's fills live on untouched).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// overlayListScreen is the page the overlay mounts over.
type overlayListScreen struct{}

func (overlayListScreen) Render() render.HTML {
	return render.HTML(`<p id="list-body">LIST</p><a id="to-detail" href="/products/7">Detail</a>`)
}

// overlayDetailScreen is the intercepted detail: a drawer when reached
// from the list, its own page on a direct load.
type overlayDetailScreen struct {
	id string
}

func (d *overlayDetailScreen) SetParams(m map[string]string) { d.id = m["id"] }
func (d *overlayDetailScreen) Render() render.HTML {
	return render.HTML(`<p id="detail-body">DETAIL-` + render.Text(d.id) + `</p>`)
}
func (d *overlayDetailScreen) ScreenTitle() string { return "Product 7" }

// overlayRig is a real uihost app whose shell binds route.title (a
// kept-layer binding) and whose /products/:id route intercepts from
// /products as a drawer. Every navigation request's headers are
// recorded.
type overlayRig struct {
	srv *httptest.Server

	mu   sync.Mutex
	hdrs []map[string]string
}

func (r *overlayRig) navs() []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]string, len(r.hdrs))
	copy(out, r.hdrs)
	return out
}

func newOverlayRig(t *testing.T) *overlayRig {
	t.Helper()
	a := app.NewApp("overlay-rig")
	aside := app.NewOutlet("aside", app.OutletOptions{Default: app.NewStaticComponent("ASIDE-DEFAULT")})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.HTML(`<span id="route-title-bind">`),
			store.Route.Title.Bind(ctx, "span", nil),
			render.HTML(`</span>`),
			l.Place(aside),
			l.Primary(),
		)
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/products", overlayListScreen{}).WithTitle("Products"), nil)
	a.Register("/products/{id}", &overlayDetailScreen{}, nil,
		app.InterceptFrom("/products", app.ScreenDrawer))

	ds := uihost.New(a)
	rt := router.New()
	ds.Mount(rt)

	r := &overlayRig{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Gofastr-Navigate") == "1" || req.Header.Get("X-Gofastr-Intercept") != "" {
			r.mu.Lock()
			r.hdrs = append(r.hdrs, map[string]string{
				"navigate":  req.Header.Get("X-Gofastr-Navigate"),
				"intercept": req.Header.Get("X-Gofastr-Intercept"),
				"fills":     req.Header.Get("X-Gofastr-Fills"),
				"path":      req.URL.Path,
			})
			r.mu.Unlock()
		}
		rt.ServeHTTP(w, req)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// TestOverlayDoesNotMoveRouteSignals: opening the intercept drawer
// mounts the overlay but leaves route.* alone — the kept shell's
// route.title binding still shows the list page's title, the store's
// route.path still names the list, and the overlay request sent no
// X-Gofastr-Fills.
func TestOverlayDoesNotMoveRouteSignals(t *testing.T) {
	rig := newOverlayRig(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var bindText, routePath string
	var overlayOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/products"),
		chromedp.WaitVisible(`#list-body`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),

		chromedp.Click(`#to-detail`, chromedp.ByID),
		chromedp.WaitVisible(`#detail-body`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('route-title-bind')?.textContent || ''`, &bindText),
		chromedp.Evaluate(`window.__gofastr && window.__gofastr._getSignal ? String(window.__gofastr._getSignal('route.path')) : (window.__gofastr_signals ? String(window.__gofastr_signals['route.path']) : '')`, &routePath),
		chromedp.Evaluate(`!!document.getElementById('fui-intercept')`, &overlayOpen),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !overlayOpen {
		t.Fatal("the intercepted navigation must mount the overlay (no dialog/overlay element)")
	}
	if want := "Products"; bindText != want {
		t.Errorf("route.title binding = %q, want the underlying page's %q — an overlay mount does not activate a route", bindText, want)
	}
	if routePath != "" && routePath != "/products" {
		t.Errorf("route.path = %q, want /products (the page underneath)", routePath)
	}
	for _, h := range rig.navs() {
		if h["path"] == "/products/7" && h["fills"] != "" {
			t.Errorf("the overlay request sent X-Gofastr-Fills: %q — an overlay's body stays bare", h["fills"])
		}
	}
}
