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
	"encoding/json"
	"sort"
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

// axeAdminEntityTables discovers every entity exposed in the admin back-office
// by scraping the entity-screen sidebar nav. The adminSidebar (an
// interactive.SectionMenu) lists one link per exposed entity, built from the
// same entitiesToExpose() that mounts the /admin/e/<table> routes — so this
// derives the scan set from the running app's actual exposure rather than a
// hand-maintained list: add an entity and its list/new screens are scanned
// with no test edit. Must run AFTER e2eLogin (admin pages are auth-gated).
func axeAdminEntityTables(t *testing.T, browser context.Context, base string) []string {
	t.Helper()
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/e/customers"),
		axePageSettle(),
		// Collect entity-base hrefs (exactly /admin/e/<table>, no /new|/view|/edit
		// suffix) from the sidebar nav, dedupe, sort.
		chromedp.Evaluate(`(() => {
			const re = /^\/admin\/e\/[^/]+$/;
			const seen = new Set();
			for (const a of document.querySelectorAll('a[href^="/admin/e/"]')) {
				const h = a.getAttribute('href') || '';
				if (re.test(h)) seen.add(h.split('/')[3]);
			}
			return JSON.stringify([...seen].sort());
		})()`, &raw),
	); err != nil {
		t.Fatalf("discover admin entities: %v", err)
	}
	var tables []string
	if err := json.Unmarshal([]byte(raw), &tables); err != nil {
		t.Fatalf("parse admin entities %q: %v", raw, err)
	}
	if len(tables) == 0 {
		t.Fatal("no admin entities discovered from /admin/e/customers sidebar — is AllEntities wired?")
	}
	return tables
}

// axeAdminRowID navigates to an admin entity list and returns the id of the
// first row's View link — the last path segment of /admin/e/<table>/view/<id>.
// Used to scan a real seeded record's edit + view screens. (Distinct from
// axeFirstDetailID: admin detail URLs carry an extra /view/ segment, so the
// app-style TrimPrefix would yield "view/<id>" instead of "<id>".)
func axeAdminRowID(t *testing.T, browser context.Context, base, table string) string {
	t.Helper()
	listPath := "/admin/e/" + table
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	var id string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+listPath),
		chromedp.WaitVisible(`.ui-data-table`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const sel = '.ui-data-table a[href^="`+listPath+`/view/"]';
			const a = document.querySelector(sel);
			return a ? (a.getAttribute('href') || '').split('/').pop() : '';
		})()`, &id),
	); err != nil {
		t.Fatalf("discover admin row id on %s: %v", listPath, err)
	}
	if id == "" {
		t.Fatalf("no admin view link on %s — is %s seeded?", listPath, table)
	}
	return id
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
	vs, err := axetest.Scan(ctx, scheme, axeAllowlist, axetest.WithEnabledRules("target-size"))
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
		// Public SDK docs (sdkdocs.Mount).
		"/docs/api", "/docs/api/auth", "/docs/api/errors",
		"/docs/api/entities/customers",
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

	// App (authenticated) screens.
	appPages := []string{
		"/app", "/app/customers", "/app/customers/" + customerID,
		"/app/invoices", "/app/invoices/" + invoiceID, "/app/subscriptions",
	}
	for _, p := range appPages {
		for _, scheme := range axetest.Schemes {
			if axeReport(t, "page", p, axeScanPage(t, browser, base, p, scheme)) {
				failed = true
			}
		}
	}

	// Admin back-office. main.go wires the admin battery with AllEntities, so
	// it serves a list + new per registered entity (5 CRUD entities today).
	// Scan the lot: derive the entity tables from the running app's own sidebar
	// (axeAdminEntityTables) so a newly added entity is covered with no test
	// edit, then add customers edit + view for a real seeded row. target-size is
	// enabled in axeScanPage so dense admin table links + sidebar nav are
	// audited against the WCAG 2.2 24px floor.
	adminTables := axeAdminEntityTables(t, browser, base)
	adminCustomerID := axeAdminRowID(t, browser, base, "customers")
	adminPages := []string{"/admin", "/admin/queue", "/admin/audit"}
	for _, table := range adminTables {
		adminPages = append(adminPages, "/admin/e/"+table, "/admin/e/"+table+"/new")
	}
	// customers edit + view — seeded data exists, so exercise the form +
	// read-only detail screens too. (The other entities' edit/view need a row
	// we'd have to create; customers is the canonical seeded one.)
	adminPages = append(adminPages,
		"/admin/e/customers/view/"+adminCustomerID,
		"/admin/e/customers/edit/"+adminCustomerID,
	)
	sort.Strings(adminPages)
	for _, p := range adminPages {
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
		vs, err := axetest.Scan(ctx, scheme, axeAllowlist, axetest.WithEnabledRules("target-size"))
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
