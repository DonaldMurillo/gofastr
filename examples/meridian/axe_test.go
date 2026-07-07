package main

// =============================================================================
// axe-core accessibility gate for the Meridian flagship app.
//
// Meridian is the design-system completeness canary (CLAUDE.md hard rule 9):
// every surface — marketing, auth, app, admin — is scanned under BOTH color
// schemes, plus the first OPEN-WIDGET scan in the repo (the Quick-add modal
// scanned in its open state). The reusable harness lives in internal/axetest;
// this file owns only the page list, the allowlist, and the gate.
//
// Run ISOLATED — never parallel with other chromedp suites:
//
//	go test ./examples/meridian/ -run TestAxeMeridian
//
// Unlike the site's component gallery, Meridian is a real app: gallery-style
// allowlist justifications (heading skips from isolated components, duplicate
// landmarks from stacked demos) do NOT apply — fix violations at the right
// layer (framework/ui or core-ui upstream; or the screen if the app composed
// wrong) and keep the allowlist for genuine false positives only.
// =============================================================================

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/axetest"
)

// axeAllowlist names axe-core rule IDs deliberately skipped, with a
// justification. Starts EMPTY — Meridian is a real app, so every violation is
// fixed at its source, not tolerated. Add an entry ONLY for a genuine false
// positive or a documented framework-level design decision, and report it
// prominently in the gate output + final report rather than burying it.
var axeAllowlist = map[string]string{}

// axePageSettle is the post-navigation settle before forcing the scheme +
// scanning, so transitions from the SPA page-swap settle first.
func axePageSettle() chromedp.Action { return chromedp.Sleep(450 * time.Millisecond) }

// axeFirstDetailID navigates to listPath, finds the first table "View" link
// pointing under basePath, and returns the record id (the last path segment).
// Used to pick a real seeded customer / invoice for the detail-page scans.
func axeFirstDetailID(t *testing.T, browser context.Context, base, listPath, basePath string) string {
	t.Helper()
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	// Login once set the session cookie for the whole browser, so this fresh
	// tab is already authenticated.
	var href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+listPath),
		chromedp.WaitVisible(`.ui-data-table`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const sel = '.ui-data-table a[href^="`+basePath+`/"]';
			const links = [...document.querySelectorAll(sel)];
			// Skip /new and /edit paths — the table only renders View links,
			// but guard against any toolbar link that slipped inside.
			const view = links.find(a => {
				const h = a.getAttribute('href') || '';
				return !h.endsWith('/new') && !h.endsWith('/edit') && !h.includes('/edit/');
			});
			return view ? view.getAttribute('href') : '';
		})()`, &href),
	); err != nil {
		t.Fatalf("discover detail id on %s: %v", listPath, err)
	}
	if href == "" {
		t.Fatalf("no detail link on %s under %s — is the entity seeded?", listPath, basePath)
	}
	return strings.TrimPrefix(href, basePath+"/")
}

// axeReport emits one t.Errorf line per violation node so the full slate is
// visible, not just the first failure. Returns true if any violations fired.
func axeReport(t *testing.T, label, path string, vs []axetest.Violation) bool {
	t.Helper()
	if len(vs) == 0 {
		return false
	}
	t.Errorf("axe violations on %s [%s]:", path, label)
	for _, v := range vs {
		t.Errorf("  • [%s · %s · %s scheme] %s", v.ID, v.Impact, v.Scheme, v.Help)
		for _, n := range v.Nodes {
			snippet := n.HTML
			if len(snippet) > 160 {
				snippet = snippet[:160] + "…"
			}
			t.Errorf("    target=%v  html=%s", n.Target, snippet)
		}
	}
	return true
}

// axeScanPage opens a fresh tab, navigates, settles, forces scheme, and scans.
// Returns the allowlist-filtered violations.
func axeScanPage(t *testing.T, browser context.Context, base, path, scheme string) []axetest.Violation {
	t.Helper()
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+path),
		axePageSettle(),
		axetest.Prepare(scheme),
	); err != nil {
		t.Errorf("axe setup %s (%s): %v", path, scheme, err)
		return nil
	}
	vs, err := axetest.Scan(ctx, scheme, axeAllowlist)
	if err != nil {
		t.Errorf("axe scan %s (%s): %v", path, scheme, err)
		return nil
	}
	return vs
}

// TestAxeMeridianClean is the gate. It scans marketing, auth, app, and admin
// pages under both color schemes, plus the open Quick-add modal — the first
// open-widget axe coverage in the repo. Prints every violation before failing.
func TestAxeMeridianClean(t *testing.T) {
	if testing.Short() {
		t.Skip("axe e2e: builds + boots the binary")
	}
	base := e2eBootApp(t)
	browser := axetest.NewBrowser(t)
	if err := chromedp.Run(browser, chromedp.Navigate("about:blank")); err != nil {
		t.Fatalf("chrome warm-up failed: %v", err)
	}

	var failed bool

	// Anonymous pages — scanned BEFORE login so guest-only routes (/login,
	// /signup) aren't redirected to /app by guestPolicy. Marketing pages are
	// public; auth pages are guest-only (an authenticated visitor is bounced
	// to /app, so scanning them post-login would audit the dashboard instead).
	anonPages := []string{
		"/", "/pricing", "/about", "/terms", "/privacy",
		"/login", "/signup",
	}
	for _, p := range anonPages {
		for _, scheme := range axetest.Schemes {
			if axeReport(t, "page", p, axeScanPage(t, browser, base, p, scheme)) {
				failed = true
			}
		}
	}

	// Log in once on a tab from THIS browser so the session cookie is set for
	// every fresh tab the authenticated scans open. chromedp.Submit doesn't
	// fire submit — e2eLogin clicks the button.
	loginCtx, loginCancel := axetest.NewTab(t, browser)
	e2eLogin(t, loginCtx, base)
	loginCancel()

	// Pick real seeded records for the detail-page scans.
	customerID := axeFirstDetailID(t, browser, base, "/app/customers", "/app/customers")
	invoiceID := axeFirstDetailID(t, browser, base, "/app/invoices", "/app/invoices")

	authPages := []string{
		// app (authenticated)
		"/app", "/app/customers", "/app/customers/" + customerID,
		"/app/invoices", "/app/invoices/" + invoiceID, "/app/subscriptions",
		// admin back-office (admin role — mounted at /admin, see main.go)
		"/admin", "/admin/queue", "/admin/audit", "/admin/e/customers",
	}
	for _, p := range authPages {
		for _, scheme := range axetest.Schemes {
			if axeReport(t, "page", p, axeScanPage(t, browser, base, p, scheme)) {
				failed = true
			}
		}
	}

	// OPEN-WIDGET case — the first open-state axe scan in the repo. Opens the
	// Quick-add customer modal (preset.Modal) and scans the open DOM in both
	// schemes, so the modal's focus trap, backdrop, labels, and contrast are
	// audited, not just the closed-page shell.
	for _, scheme := range axetest.Schemes {
		ctx, cancel := axetest.NewTab(t, browser)
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/app/customers"),
			axePageSettle(),
			axetest.Prepare(scheme),
			chromedp.Click(`button[data-fui-open="customer-quick-add"]`, chromedp.ByQuery),
			chromedp.WaitVisible(`#qa-name`, chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond),
		); err != nil {
			t.Errorf("open-widget setup (%s scheme): %v", scheme, err)
			cancel()
			failed = true
			continue
		}
		vs, err := axetest.Scan(ctx, scheme, axeAllowlist)
		cancel()
		if err != nil {
			t.Errorf("open-widget scan (%s scheme): %v", scheme, err)
			failed = true
			continue
		}
		if axeReport(t, "open-widget:quick-add", "/app/customers (modal open)", vs) {
			failed = true
		}
	}

	if failed {
		t.Errorf("\nfix the violations at the right layer (framework/ui or core-ui " +
			"upstream for component defects; the meridian screen for composition " +
			"defects) OR add the rule id to axeAllowlist with a justification — " +
			"only for a genuine false positive.")
	}
}
