package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// tallEmbedComp renders content taller than the loader's initial 150px frame,
// so a height change in the parent is unambiguous evidence that the content
// arrived, hydrated and laid out, not only that a request returned 200.
type tallEmbedComp struct{}

func (c *tallEmbedComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *tallEmbedComp) RenderCtx(ctx context.Context) render.HTML {
	viewer := "anonymous"
	if u, ok := handlerUser(ctx); ok {
		viewer = u
	}
	var b strings.Builder
	b.WriteString(`<p id="viewer">viewer:` + viewer + `</p>`)
	// A poll island. poll is a DEMAND-LOADED module that builds its own fetch
	// headers: the class of request that carried no identity at all until the
	// frame started attaching the grant to every same-origin fetch. Polls fire
	// on a timer, so this needs no click and works across the origin boundary
	// where chromedp cannot reach into the frame.
	b.WriteString(`<div data-fui-poll="5s" data-fui-poll-src="/polled">waiting</div>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "<p>row %d</p>", i)
	}
	return render.HTML(b.String())
}

func handlerUser(ctx context.Context) (string, bool) {
	if u, ok := handler.GetUser(ctx); ok {
		if s, ok := u.(string); ok {
			return s, true
		}
	}
	return "", false
}

// TestEmbedEndToEndInABrowser drives the whole feature the way a customer does:
// a page on ANOTHER origin loads the loader script, the loader frames the app,
// the frame completes the postMessage handshake, exchanges its nonce, fetches
// the surface as the granted subject, injects it, and reports its height back.
//
// It is a two-origin test on purpose. Everything interesting about this feature,
// the cookie that is not sent, frame-ancestors, the postMessage targetOrigin,
// CORP on the loader, is invisible when both pages share an origin.
func TestEmbedEndToEndInABrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}

	var exchangeHits, contentHits, grantedContentHits int32
	var pollHits, grantedPollHits int32

	// The customer's site has to know the app's URL, and the app's allowlist
	// has to know the customer's origin. Start the customer server first with
	// a handler that reads both out of variables filled in once the app is up.
	var appURL, nonce string
	customerMux := http.NewServeMux()
	customerMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>customer</title></head><body>
<h1 id="host-page">Acme</h1>
<div id="reports"></div>
<script>
  window.__embedEvents = [];
  window.addEventListener('message', (e) => {
    if (e.data && e.data.proto === 'gofastr-embed/1') window.__embedEvents.push(e.data.type);
  });
</script>
<script src="%s/__gofastr/embed.js" data-surface="reports" data-token="%s" data-target="#reports"></script>
</body></html>`, appURL, nonce)
	})
	customer := httptest.NewServer(customerMux)
	t.Cleanup(customer.Close)

	application := app.NewApp("Embed E2E")
	application.SetDefaultLayout(app.NewLayout("main").WithHeader(&testHeaderComp{}))
	reportsScreen := app.NewScreen("/reports", &tallEmbedComp{}).WithTitle("Reports")
	application.RegisterScreen(reportsScreen, app.EmbedLayout())

	eh, err := fembed.New(fembed.Config{
		Surfaces: []fembed.Surface{{
			Name:    "reports",
			Screen:  reportsScreen,
			Origins: []string{customer.URL},
		}},
		BurnStore: fembed.NewMemoryBurnStore(),
		Resolve:   func(_ context.Context, subject string) (any, error) { return subject, nil },
	})
	if err != nil {
		t.Fatalf("embed.New: %v", err)
	}
	eh.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	ds := New(application, WithEmbed(eh))

	// Count what the browser actually asks for, so a green test cannot mean
	// "the page rendered something that happened to be tall".
	appSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/__gofastr/embed-exchange":
			atomic.AddInt32(&exchangeHits, 1)
		case strings.HasSuffix(r.URL.Path, "/content"):
			atomic.AddInt32(&contentHits, 1)
			if r.Header.Get(embedGrantHeader) != "" {
				atomic.AddInt32(&grantedContentHits, 1)
			}
		case r.URL.Path == "/polled":
			atomic.AddInt32(&pollHits, 1)
			if r.Header.Get(embedGrantHeader) != "" {
				atomic.AddInt32(&grantedPollHits, 1)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<span>polled</span>"))
			return
		}
		ds.ServeHTTP(w, r)
	}))
	t.Cleanup(appSrv.Close)
	appURL = appSrv.URL

	if nonce, err = eh.MintNonce(context.Background(), "reports", "user-7", customer.URL, nil); err != nil {
		t.Fatalf("MintNonce: %v", err)
	}

	ctx := newEmbedBrowserCtx(t)

	var frameCount int
	var height float64
	var events []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(customer.URL+"/"),
		chromedp.WaitVisible(`#host-page`, chromedp.ByID),
		// The handshake is: frame says ready → parent hands the nonce → frame
		// exchanges it → frame fetches content → frame reports height.
		// Wait on the frame EXISTING and reporting any height at all. The
		// later assertions are what judge the value. An earlier version
		// polled for "height > 200" and then asserted "height > 200", so a
		// genuine failure aborted in Run and the assertion was unreachable.
		chromedp.Poll(`(() => {
			const f = document.querySelector('#reports iframe');
			return !!f && f.getBoundingClientRect().height > 0
				&& f.getAttribute('style').indexOf('height: 150px') < 0;
		})()`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('#reports iframe').length`, &frameCount),
		chromedp.Evaluate(`document.querySelector('#reports iframe').getBoundingClientRect().height`, &height),
		chromedp.Evaluate(`window.__embedEvents`, &events),
	); err != nil {
		t.Fatalf("chromedp: %v (exchange=%d content=%d)", err,
			atomic.LoadInt32(&exchangeHits), atomic.LoadInt32(&contentHits))
	}

	if frameCount != 1 {
		t.Errorf("the loader created %d frames, want exactly 1 — a second frame would exchange the same single-use nonce again", frameCount)
	}
	if height <= 200 {
		t.Errorf("frame height = %.0f, want > 200 — the content never arrived or never laid out", height)
	}
	for _, e := range events {
		if e == "error" {
			t.Fatalf("the frame reported an error; events = %v", events)
		}
	}
	if got := atomic.LoadInt32(&exchangeHits); got != 1 {
		t.Errorf("exchange hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&contentHits); got != 1 {
		t.Errorf("content hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&grantedContentHits); got != 1 {
		t.Errorf("%d of the content requests carried a grant, want 1 — the frame fetched its content unauthenticated", got)
	}

	// Give the poll a full cadence to fire. The module clamps to a 5s minimum
	// (a typo must not be able to DoS the server) and jitters the first tick,
	// so a shorter window observes nothing and proves nothing.
	//
	// Every request the frame makes has to carry
	// the grant, not only the ones rpc.js builds: a poll, a toggle, an
	// optimistic action and an infinite scroll each assemble their own headers,
	// and inside a frame there is no cookie to fall back on. An anonymous poll
	// silently replaces authenticated content with a logged-out render.
	if err := chromedp.Run(ctx, chromedp.Sleep(7*time.Second)); err != nil {
		t.Fatalf("chromedp (poll window): %v", err)
	}
	polls := atomic.LoadInt32(&pollHits)
	if polls == 0 {
		t.Fatal("the poll island never fired inside the frame — demand-loaded modules are not hydrating on injected content")
	}
	if granted := atomic.LoadInt32(&grantedPollHits); granted != polls {
		t.Errorf("%d of %d poll requests from inside the frame carried the grant — a module that builds its own headers is fetching anonymously", granted, polls)
	}
}

// TestEmbedRefusesToRenderUnframed is the other half: opening the embed URL
// directly is not a way to view the surface. There is no parent to hand over a
// nonce, so nothing renders, and the page says so rather than spinning.
func TestEmbedRefusesToRenderUnframed(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	f := newEmbedFixture(t)
	srv := httptest.NewServer(f.host)
	t.Cleanup(srv.Close)

	ctx := newEmbedBrowserCtx(t)
	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/__gofastr/embed/reports"),
		chromedp.Poll(`document.getElementById('gofastr-embed-root').getAttribute('data-fui-embed-state') === 'error'`,
			nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('gofastr-embed-root').getAttribute('data-fui-embed-state')`, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if state != "error" {
		t.Fatalf("embed state = %q, want error — an unframed embed URL has no way to obtain a credential", state)
	}
}

func newEmbedBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
		chromedp.WindowSize(1024, 768),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run: allocate against the
	// browser context so the browser's lifetime is the browser context's.
	// Passing a timeout context here would make the browser die when that
	// deadline passed. The watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}
