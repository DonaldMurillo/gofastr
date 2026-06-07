package main

// Browser-level (chromedp) e2e for the entity admin. Drives the real user
// path through a headless Chrome so the runtime-dependent behaviour is
// exercised end to end: SSR list hydration, the data-fui-confirm delete
// (native confirm → DELETE RPC → SPA refresh), and a form create round-trip.
//
// Gated by -short (slow; needs a headless Chrome), matching the repo's e2e
// convention.

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func backofficeServer(t *testing.T) string {
	t.Helper()
	app := setupApp(":memory:")
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

func backofficeBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1280, 800),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	// Auto-accept any window.confirm (the data-fui-confirm delete dialog).
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		if _, ok := ev.(*page.EventJavascriptDialogOpening); ok {
			go func() { _ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true)) }()
		}
	})
	return ctx
}

// login signs in via the demo login form, landing on the products list.
func login(t *testing.T, ctx context.Context, base string) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/login"),
		chromedp.WaitVisible(`button[type=submit]`, chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table`, chromedp.ByQuery), // landed on the list
	); err != nil {
		t.Fatalf("login: %v", err)
	}
}

func rowCount(ctx context.Context) (int, error) {
	var n int
	err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelectorAll('tbody tr').length`, &n))
	return n, err
}

// waitHydrated blocks until runtime.js has installed its global click/submit
// dispatcher — clicking a data-fui-rpc button before that is a no-op.
func waitHydrated(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var ready bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.__fuiGlobalDispatch === true`, &ready))
		if ready {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("runtime did not hydrate (document.__fuiGlobalDispatch)")
}

func TestBackofficeE2E_DeleteFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	if n, err := rowCount(ctx); err != nil || n == 0 {
		t.Fatalf("expected seeded products; got %d rows (err=%v)", n, err)
	}

	// Capture the first row's id. The list is paginated, so a delete refills the
	// page from later rows — assert the SPECIFIC row disappears, not that the
	// total row count drops.
	var rowID string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('tbody tr')?.id || ''`, &rowID)); err != nil || rowID == "" {
		t.Fatalf("no first row id to delete (id=%q err=%v)", rowID, err)
	}

	// Click that row's Delete. data-fui-confirm calls window.confirm — stub it to
	// accept — then the runtime DELETEs and the island swaps in the fresh table.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.confirm = () => true; true`, nil),
		chromedp.Click(`tbody tr:first-child button[data-fui-rpc^="/admin/e/products/_delete/"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click delete: %v", err)
	}

	// Poll until the deleted row is gone from the page.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var present bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`!!document.querySelector('tbody tr[id=%q]')`, rowID), &present))
		if !present {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("deleted row %s still present after delete", rowID)
}

func TestBackofficeE2E_CreateFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)

	name := fmt.Sprintf("E2E Widget %d", time.Now().UnixNano())
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/e/products/new"),
		chromedp.WaitVisible(`input[name="name"]`, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="name"]`, name, chromedp.ByQuery),
		chromedp.SendKeys(`input[name="price"]`, "42", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table`, chromedp.ByQuery), // redirected back to the list
	); err != nil {
		t.Fatalf("create flow: %v", err)
	}

	// The list is paginated, so the new row may be on a later page — find it via
	// search (also exercises the search path) rather than assuming page 1.
	var body string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/e/products?q="+url.QueryEscape(name)),
		chromedp.WaitVisible(`tbody tr`, chromedp.ByQuery),
		chromedp.Text(`tbody`, &body, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("search for created product: %v", err)
	}
	if !strings.Contains(body, name) {
		t.Fatalf("created product not found via search; got %q", body)
	}
}
