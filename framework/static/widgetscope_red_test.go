//go:build red

package static

// A widget scoped with .Pages("/x") is offered on /x only: the live
// catalog (/__gofastr/widgets?page=) and the SSR inline both filter by
// widget.AvailableOn (core-ui/widget Builder.Pages: "The widget is hidden
// from the catalog + SSR-inlining on every page whose path isn't in the
// list"). The static export dumps the catalog with no page filter
// (dumpWidgetAssets: "no page filter = every widget") and the static
// runtime's boot pass (frag/widgets-boot-static.js) mounts every
// non-hidden entry of it, so a page-scoped banner or panel is drawn on
// every page of an exported site.

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// Open finding from the 2026-11-10 bug hunt, kept as a red probe: it
// asserts the correct behaviour and fails until the fix lands. A static
// export mounts a widget scoped with .Pages on every exported page. The
// fix adds a per-widget page list to widgets.json (a documented
// artifact) and needs a page-key rule for static paths (/x/, /x,
// /x/index.html, under BasePath and a locale prefix).
func TestRedStaticExportKeepsWidgetPageScope(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	execPath := browserExecutable(t)

	a := coreapp.NewApp("Scoped Widget Export")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/bughunt-scoped", &homeScreen{}, nil)
	host := uihost.New(a)
	// Auto-mounting (not Hidden), on one page only.
	w := widget.New("bughunt-scoped-panel").Pages("/bughunt-scoped").Slot("body", homeScreen{}).Build()
	widget.Mount(router.New(), &w)

	// The live contract this export should mirror.
	for _, d := range widget.AvailableOn("/") {
		if d.Name == "bughunt-scoped-panel" {
			t.Fatal("setup: the widget is offered on / by the live catalog")
		}
	}

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.FileServer(http.Dir(out))}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })
	base := "http://" + ln.Addr().String()

	ctx := chromedptest.Context(t,
		chromedptest.AllocatorOptions(chromedp.ExecPath(execPath)),
		chromedptest.Timeout(60*time.Second),
	)
	await := func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }
	// The boot catalog pass has settled once gofastr:widgets-synced fired
	// (or, when it fired before this listener, shortly after load).
	settled := `new Promise((done) => { addEventListener('gofastr:widgets-synced', () => done(true)); setTimeout(() => done(true), 3000); })`
	var onScoped, onHome bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/bughunt-scoped/"),
		chromedp.Evaluate(settled, nil, await),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="bughunt-scoped-panel"]')`, &onScoped),
		chromedp.Navigate(base+"/"),
		chromedp.Evaluate(settled, nil, await),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="bughunt-scoped-panel"]')`, &onHome),
	); err != nil {
		t.Fatal(err)
	}
	if !onScoped {
		t.Fatal("setup: the widget did not mount on its own page /bughunt-scoped/")
	}
	if onHome {
		t.Fatal(`a widget scoped with .Pages("/bughunt-scoped") is mounted on / in the static export; the live host offers it on /bughunt-scoped only`)
	}
}
