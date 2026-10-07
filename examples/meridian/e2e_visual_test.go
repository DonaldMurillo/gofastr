package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
)

type visualViewportMetrics struct {
	ViewportWidth float64 `json:"viewportWidth"`
	DocumentWidth float64 `json:"documentWidth"`
}

type visualInkBandMetrics struct {
	Background string `json:"background"`
	Heading    string `json:"heading"`
	Paragraph  string `json:"paragraph"`
}

// TestE2EVisualSurfaces is the rendered flagship canary required for
// framework/design-system changes. It covers one representative marketing,
// auth, app, and admin surface at desktop and phone widths in both schemes.
func TestE2EVisualSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("visual e2e: builds + boots the binary")
	}
	base := e2eBootApp(t)
	browser := axetest.NewBrowser(t)
	if err := chromedp.Run(browser, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("chrome warm-up: %v", err)
	}

	for _, surface := range []struct {
		name string
		path string
	}{
		{name: "marketing", path: "/"},
		{name: "auth", path: "/login"},
		// Public SDK docs (sdkdocs.Mount): overview + one entity reference,
		// the CodeTabs/DataTable-heavy surfaces most likely to overflow.
		{name: "sdkdocs", path: "/docs/api"},
		{name: "sdkdocs-entity", path: "/docs/api/entities/customers"},
	} {
		captureMeridianSurface(t, browser, base, surface.name, surface.path)
	}

	// Authenticate once on the shared browser; fresh capture tabs inherit the
	// session cookie for the app and admin surfaces below.
	loginCtx, cancel := axetest.NewTab(t, browser)
	e2eLogin(t, loginCtx, base)
	invoiceID := seedVisualRecords(t, loginCtx)
	cancel()

	for _, surface := range []struct {
		name string
		path string
	}{
		{name: "app", path: "/app"},
		{name: "admin", path: "/admin"},
		{name: "admin-list", path: "/admin/entities/customers"},
		// A generated form with a bool field: the checkbox in a
		// ui.FormField must keep its own size, not the text track's.
		{name: "admin-form", path: "/admin/entities/plans/create"},
		// Payments soft-delete: the Deleted view lists the binned one.
		{name: "admin-trash", path: "/admin/entities/payments?view=deleted"},
		{name: "admin-record-api", path: "/admin/entities/invoices/" + invoiceID + "?tab=api"},
	} {
		captureMeridianSurface(t, browser, base, surface.name, surface.path)
	}
}

// seedVisualRecords creates, through the signed-in tab's own session, a
// customer, an invoice and two payments, then deletes one payment, so
// the admin's record and trash surfaces draw real rows. It returns the
// invoice's id.
func seedVisualRecords(t *testing.T, ctx context.Context) string {
	t.Helper()
	const js = `(async () => {
		const post = async (path, body) => {
			const r = await fetch(path, {method: "POST", headers: {"Content-Type": "application/json"}, body: JSON.stringify(body)});
			if (r.status !== 201) throw new Error(path + " = " + r.status + " " + await r.text());
			const j = await r.json();
			return j.id ?? j.data?.id;
		};
		const customer = await post("/api/customers", {name: "Ada Lovelace", email: "ada+" + Date.now() + "@example.com", company: "Analytical"});
		const invoice = await post("/api/invoices", {customer_id: customer, number: "INV-V" + Date.now(), amount: "1200.00", status: "open"});
		await post("/api/payments", {invoice_id: invoice, customer_id: customer, amount: "600.00"});
		const binned = await post("/api/payments", {invoice_id: invoice, customer_id: customer, amount: "75.00", method: "wire"});
		const d = await fetch("/api/payments/" + binned, {method: "DELETE"});
		if (d.status >= 300) throw new Error("delete payment = " + d.status);
		return invoice;
	})()`
	var id string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &id, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})); err != nil {
		t.Fatalf("seed visual records: %v", err)
	}
	if id == "" {
		t.Fatal("seed visual records: no invoice id")
	}
	return id
}

func captureMeridianSurface(t *testing.T, browser context.Context, base, surface, path string) {
	t.Helper()
	for _, viewport := range []struct {
		name          string
		width, height int64
	}{
		{name: "desktop", width: 1440, height: 1000},
		{name: "mobile", width: 390, height: 844},
	} {
		for _, scheme := range axetest.Schemes {
			t.Run(surface+"/"+viewport.name+"/"+scheme, func(t *testing.T) {
				var metrics visualViewportMetrics
				var inkBand visualInkBandMetrics
				var shot []byte
				var lastErr error
				for attempt := 1; attempt <= 6; attempt++ {
					tab, tabCancel := chromedp.NewContext(browser)
					// 30s, not ~12s: CI runners raster in software on two
					// cores and routinely need >12s for a cold 1440px
					// navigate+capture even serialized, a tight deadline
					// there burns every attempt and times the package out.
					ctx, timeoutCancel := context.WithTimeout(tab, 30*time.Second)
					shot = nil
					actions := []chromedp.Action{
						chromedp.EmulateViewport(viewport.width, viewport.height),
						chromedp.Navigate(base + path),
						chromedp.WaitReady("body", chromedp.ByQuery),
						// New-headless Chrome produces compositor frames only
						// for the active target. A fresh tab opened while
						// another target held focus (the warm-up tab, the
						// login tab) can stay occluded, and the fromSurface
						// screenshot below then waits forever for a frame that
						// never comes, deterministic on CI, invisible on
						// macOS. Activate the tab before capturing.
						chromedp.ActionFunc(func(ctx context.Context) error {
							return page.BringToFront().Do(ctx)
						}),
						axetest.Prepare(scheme),
						// Prepare changes the scheme attribute. Let Chromium commit the
						// resulting paint before CaptureScreenshot; otherwise a fromSurface
						// capture can contain a transient black compositor tile even though
						// the DOM and computed colors are already correct.
						chromedp.Sleep(300 * time.Millisecond),
					}
					if surface == "marketing" {
						actions = append(actions, chromedp.Evaluate(`(() => {
							const heading = [...document.querySelectorAll('.fui-card__heading')].find(h => h.textContent.trim() === "Simple, honest pricing");
							const card = heading && heading.closest('[data-cui-comp="ui-card"]');
							const paragraph = heading && heading.nextElementSibling;
							if (!heading || !card || !paragraph) return {};
							return {
								background: getComputedStyle(card).backgroundColor,
								heading: getComputedStyle(heading).color,
								paragraph: getComputedStyle(paragraph).color,
							};
						})()`, &inkBand))
					}
					actions = append(actions,
						chromedp.Evaluate(`({viewportWidth: innerWidth, documentWidth: document.documentElement.scrollWidth})`, &metrics),
						// Viewport capture is deliberate: full-page capture can wait on
						// Meridian's long-lived SSE document and never complete.
						chromedp.ActionFunc(func(ctx context.Context) error {
							var err error
							shot, err = page.CaptureScreenshot().
								WithFromSurface(true).
								WithOptimizeForSpeed(true).
								Do(ctx)
							return err
						}),
					)
					lastErr = chromedp.Run(ctx, actions...)
					timeoutCancel()
					tabCancel()
					if lastErr == nil && surface == "marketing" &&
						(inkBand.Background == "" || inkBand.Heading == inkBand.Background || inkBand.Paragraph == inkBand.Background) {
						lastErr = fmt.Errorf("ink band lacks visible contrast: %#v", inkBand)
					}
					if lastErr == nil {
						lastErr = screenshotHasVariation(shot)
					}
					if lastErr == nil {
						break
					}
					t.Logf("capture attempt %d failed: %v", attempt, lastErr)
				}
				if lastErr != nil {
					t.Fatalf("capture %s after retries: %v", path, lastErr)
				}
				if metrics.DocumentWidth > metrics.ViewportWidth+1 {
					t.Errorf("horizontal overflow: document=%.1f viewport=%.1f", metrics.DocumentWidth, metrics.ViewportWidth)
				}
				if dir := os.Getenv("GOFASTR_VISUAL_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						t.Fatal(err)
					}
					file := filepath.Join(dir, fmt.Sprintf("meridian-%s-%s-%s.png", surface, viewport.name, scheme))
					if err := os.WriteFile(file, shot, 0o644); err != nil {
						t.Fatal(err)
					}
					t.Logf("wrote %s", file)
				}
			})
		}
	}
}

func screenshotHasVariation(data []byte) error {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode screenshot: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Empty() {
		return fmt.Errorf("empty screenshot")
	}
	r0, g0, b0, a0 := img.At(bounds.Min.X, bounds.Min.Y).RGBA()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if r != r0 || g != g0 || b != b0 || a != a0 {
				return nil
			}
		}
	}
	return fmt.Errorf("single-color screenshot")
}
