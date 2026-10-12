package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A busy Instant area keeps full opacity while a busy plain area dims:
// the host's dim is how a kept region says it is about to change, but
// an Instant trail swaps in one frame and keeps its root, so dimming it
// blinks the part that stays (the admin's "Meridian" crumb flashed on
// every navigation). Both carry the host's sheet and the layout's
// transition CSS, in either order.
func TestInstantAreaDoesNotDimWhileBusy(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	shell := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs", Transition: app.Instant()}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return "" })
	host := frameworkDimCSS
	layout := shell.TransitionCSS()
	for _, order := range [][2]string{{host, layout}, {layout, host}} {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<!doctype html><html><head><style>%s</style><style>%s</style></head><body>
<div id="instant" data-cui-area="shell~crumbs" data-cui-vt="vt-shell-crumbs" aria-busy="true">Meridian / Customers</div>
<div id="plain" data-cui-area="shell~side" aria-busy="true">Side</div>
</body></html>`, order[0], order[1])
		})
		srv := httptest.NewServer(mux)
		ctx := chromedptest.Context(t)
		var got map[string]string
		probe := `new Promise(r => setTimeout(() => r({
			instant: getComputedStyle(document.getElementById("instant")).opacity,
			plain: getComputedStyle(document.getElementById("plain")).opacity,
		}), 600))`
		err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/"),
			chromedp.Evaluate(probe, &got, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }))
		srv.Close()
		if err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got["instant"] != "1" {
			t.Errorf("a busy Instant area dimmed to %s", got["instant"])
		}
		if got["plain"] == "1" {
			t.Errorf("a busy plain area no longer dims; the probe proves nothing")
		}
	}
}
