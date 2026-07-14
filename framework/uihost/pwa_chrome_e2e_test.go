package uihost

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestPWAChromeE2E drives the full PWA lifecycle in a real Chrome:
// registration + installability metadata, offline fallback against a
// genuinely dead server (listener closed — no CDP network emulation,
// which does not reliably reach service-worker fetches), and
// cache-version cleanup across a v1 → v2 deployment on the same origin.
// Serialized by design: phases share one browser and one origin.
func TestPWAChromeE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}

	makeHost := func(extraCSS string) *UIHost {
		a := app.NewApp("PWA Demo")
		a.Register("/", &plainComp{}, nil)
		a.Register("/other", &plainComp{}, nil)
		opts := []Option{WithPWA(PWAConfig{ThemeColor: "#112233"})}
		if extraCSS != "" {
			// Changing app.css rotates the deployment fingerprint —
			// this is what makes v2 a "new asset fingerprint" deploy.
			opts = append(opts, WithCustomCSS(extraCSS))
		}
		return New(a, opts...)
	}

	var current atomic.Pointer[UIHost]
	current.Store(makeHost(""))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Load().ServeHTTP(w, r)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	base := "http://" + addr
	srv := &http.Server{Handler: handler}
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

	// ── Phase 1: registration + installability metadata ─────────────
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var hasManifestLink bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`!!document.querySelector('link[rel="manifest"][href="/manifest.webmanifest"]')`, &hasManifestLink)); err != nil {
		t.Fatal(err)
	}
	if !hasManifestLink {
		t.Errorf("page should carry the manifest link")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`fetch('/manifest.webmanifest').then(r => r.json()).then(m => { window.__m = m; }); true`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := poll(`!!window.__m`, 10*time.Second); err != nil {
		t.Fatalf("manifest fetch: %v", err)
	}
	var manifest map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__m`, &manifest)); err != nil {
		t.Fatal(err)
	}
	if manifest["name"] != "PWA Demo" || manifest["start_url"] != "/" ||
		manifest["scope"] != "/" || manifest["display"] != "standalone" ||
		manifest["theme_color"] != "#112233" {
		t.Errorf("installability metadata wrong: %v", manifest)
	}

	// Registration: register.js runs on load; the worker precaches the
	// shell, activates, and claims the page.
	if err := poll(`!!(navigator.serviceWorker && navigator.serviceWorker.controller)`, 30*time.Second); err != nil {
		t.Fatalf("service worker never took control: %v", err)
	}
	cacheKeys := func() []string {
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`caches.keys().then(k => { window.__ck = k; }); true`, nil)); err != nil {
			t.Fatal(err)
		}
		if err := poll(`Array.isArray(window.__ck)`, 10*time.Second); err != nil {
			t.Fatalf("caches.keys: %v", err)
		}
		var keys []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__ck`, &keys)); err != nil {
			t.Fatal(err)
		}
		if err := chromedp.Run(ctx, chromedp.Evaluate(`delete window.__ck; true`, nil)); err != nil {
			t.Fatal(err)
		}
		return keys
	}
	v1Keys := cacheKeys()
	if len(v1Keys) != 1 || !strings.HasPrefix(v1Keys[0], "gofastr-pwa-pwa-demo-") {
		t.Fatalf("expected one owned cache, got %v", v1Keys)
	}
	v1Cache := v1Keys[0]

	// ── Phase 2: offline fallback against a dead server ─────────────
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/other"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("offline navigation: %v", err)
	}
	var offlineBody string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &offlineBody)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(offlineBody, "You're offline") {
		t.Errorf("offline navigation should render the offline screen, got: %q", offlineBody)
	}

	// ── Phase 3: v2 deploy → new cache version, old cache removed ───
	current.Store(makeHost("/* v2 */"))
	var ln2 net.Listener
	for deadline := time.Now().Add(10 * time.Second); ; {
		ln2, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("rebind %s: %v", addr, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	srv2 := &http.Server{Handler: handler}
	go srv2.Serve(ln2)
	defer srv2.Close()

	// A normal page load re-registers, which doubles as the update
	// check; the v2 worker installs its cache and waits.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("v2 navigate: %v", err)
	}
	if err := poll(`(function(){ caches.keys().then(k => { window.__n = k.length; }); return window.__n === 2; })()`, 30*time.Second); err != nil {
		t.Fatalf("v2 install never created its cache: %v", err)
	}

	// The old worker keeps controlling until its clients go away — no
	// forced skipWaiting. Release the origin (about:blank), let the v2
	// worker activate and clean up, then come back and verify only the
	// v2 cache remains.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if err := chromedp.Run(ctx,
			chromedp.Navigate("about:blank"),
			chromedp.Sleep(500*time.Millisecond),
			chromedp.Navigate(base+"/"),
			chromedp.WaitReady("body", chromedp.ByQuery),
		); err != nil {
			t.Fatalf("release/return cycle: %v", err)
		}
		keys := cacheKeys()
		if len(keys) == 1 {
			if keys[0] == v1Cache {
				t.Fatalf("v2 activation kept the OLD cache: %v", keys)
			}
			if !strings.HasPrefix(keys[0], "gofastr-pwa-pwa-demo-") {
				t.Fatalf("surviving cache not owned by the app: %v", keys)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old cache never cleaned up, still: %v", keys)
		}
	}
}
