package static

// Real-browser proof of "static site as an app": export a site with
// WithPWA, serve the files, let the service worker install (precaching
// the WHOLE export), then kill the server and navigate to a page the
// browser has NEVER visited — its real content must render from cache,
// not the offline screen. Mirrors framework/uihost's PWA Chrome e2e
// (dead listener instead of CDP network emulation, which does not
// reliably reach service-worker fetches). Gated by -short.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func TestPWAStaticChromeE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}

	a := coreapp.NewApp("Static App")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/products/:slug", &productScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	srv := &http.Server{Handler: http.FileServer(http.Dir(out))}
	go srv.Serve(ln)

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer allocCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()
	ctx, cancel := context.WithTimeout(browserCtx, 120*time.Second)
	defer cancel()

	poll := func(expr string, timeout time.Duration) error {
		deadline := time.Now().Add(timeout)
		for {
			var ok bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &ok)); err != nil {
				return err
			}
			if ok {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for %s", expr)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Land once on the home page; the worker installs and precaches the
	// whole export. Never touch the product pages while online.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := poll(`!!(navigator.serviceWorker && navigator.serviceWorker.controller)`, 30*time.Second); err != nil {
		t.Fatalf("service worker never took control: %v", err)
	}
	// Install is atomic (addAll-equivalent): a controlling worker means
	// the full precache — including never-visited pages — is in place.

	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}

	// Offline navigation to a page this browser has NEVER loaded.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/products/beta"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("offline navigation: %v", err)
	}
	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &body)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "product-beta") {
		t.Errorf("never-visited page must serve its REAL content offline, got: %q", body)
	}
	if strings.Contains(body, "offline") && !strings.Contains(body, "product-beta") {
		t.Errorf("offline screen shown instead of precached content: %q", body)
	}
}
