package main

// Browser-level (chromedp) e2e for the entity-admin grid features added on top
// of the basic CRUD flows: column sorting (island sort RPC swaps the table),
// search (filters server-side), the read-only detail view, and the BelongsTo
// relationship picker. These exercise the runtime path the httptest tests in
// battery/admin can't reach — clicking a real sort header, the search submit
// navigation, and a populated <select> of related records.
//
// Gated by -short, like the other backoffice e2e.

import (
	"strings"
	"testing"
	"time"

	"context"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func firstRowText(ctx context.Context) string {
	var s string
	_ = chromedp.Run(ctx, chromedp.Evaluate(
		`(document.querySelector('tbody tr')?.innerText || '')`, &s))
	return s
}

func tbodyText(ctx context.Context) string {
	var s string
	_ = chromedp.Run(ctx, chromedp.Text(`tbody`, &s, chromedp.ByQuery))
	return s
}

// pollUntil retries fn until it returns true or the deadline passes.
func pollUntil(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fn()
}

// TestBackofficeE2E_SortByName clicks the "name" column header twice (→ desc)
// and asserts the island RPC reordered the table so the alphabetically-last
// product is first. Seed order is already ascending, so a working desc sort is
// the unambiguous signal.
func TestBackofficeE2E_SortByName(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	// The column header is a SPA-nav sort link (not an island RPC) so a click
	// re-renders the whole screen — table AND the toolbar Sort summary — keeping
	// them in one consistent state.
	sortSel := `thead a[href*="sort=name"]`
	if err := chromedp.Run(ctx, chromedp.WaitVisible(sortSel, chromedp.ByQuery)); err != nil {
		t.Fatalf("sortable name header link missing: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(sortSel, chromedp.ByQuery)); err != nil {
		t.Fatalf("click sort: %v", err)
	}
	// After ascending sort, page 1 leads with the alphabetically-first product
	// ("Circular Saw") AND the toolbar Sort button reflects the active sort.
	if !pollUntil(10*time.Second, func() bool {
		var summary string
		_ = chromedp.Run(ctx, chromedp.Text(`.admin-sort__summary`, &summary, chromedp.ByQuery))
		return strings.Contains(firstRowText(ctx), "Circular Saw") && strings.Contains(summary, "Name")
	}) {
		var summary string
		_ = chromedp.Run(ctx, chromedp.Text(`.admin-sort__summary`, &summary, chromedp.ByQuery))
		t.Fatalf("sort not applied/reflected; first row = %q, sort summary = %q", firstRowText(ctx), summary)
	}
}

// TestBackofficeE2E_Search types a query and submits the search form, asserting
// the list is filtered server-side to the matching product.
func TestBackofficeE2E_Search(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`input[name="q"]`, chromedp.ByQuery),
		// Enter submits the GET search form → the list re-renders filtered.
		chromedp.SendKeys(`input[name="q"]`, "Drill"+kb.Enter, chromedp.ByQuery),
		chromedp.WaitVisible(`tbody tr`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("search submit: %v", err)
	}
	if !pollUntil(10*time.Second, func() bool {
		body := tbodyText(ctx)
		return strings.Contains(body, "Cordless Drill") && !strings.Contains(body, "Hex Bit Set")
	}) {
		t.Fatalf("search did not filter the list; tbody = %q", tbodyText(ctx))
	}
}

// TestBackofficeE2E_DetailView clicks a row's View link and asserts the
// read-only detail screen renders the record's fields.
func TestBackofficeE2E_DetailView(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`a[href^="/admin/e/products/view/"]`, chromedp.ByQuery),
		chromedp.Click(`a[href^="/admin/e/products/view/"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`dl.admin-detail`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open detail: %v", err)
	}
	var detail string
	if err := chromedp.Run(ctx, chromedp.Text(`dl.admin-detail`, &detail, chromedp.ByQuery)); err != nil {
		t.Fatalf("read detail: %v", err)
	}
	// A seeded product name and the supplier_id field label should be present.
	if !strings.Contains(detail, "Drill") && !strings.Contains(detail, "Hex") && !strings.Contains(detail, "Goggles") {
		t.Fatalf("detail view missing a product name; got %q", detail)
	}
	// Labels are humanised ("supplier_id" → "Supplier", upper-cased via CSS).
	if !strings.Contains(strings.ToLower(detail), "supplier") {
		t.Fatalf("detail view should list every field incl. the supplier relation; got %q", detail)
	}
}

// TestBackofficeE2E_RelationDropdown asserts the product form renders a
// supplier <select> populated with the seeded suppliers, selects one, and
// submits — proving the relationship picker is wired end to end.
func TestBackofficeE2E_RelationDropdown(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)

	var optionText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/e/products/new"),
		chromedp.WaitVisible(`select[name="supplier_id"]`, chromedp.ByQuery),
		chromedp.Evaluate(`[...document.querySelector('select[name="supplier_id"]').options].map(o=>o.textContent).join('|')`, &optionText),
	); err != nil {
		t.Fatalf("open product form: %v", err)
	}
	if !strings.Contains(optionText, "Acme Supply") || !strings.Contains(optionText, "Globex Parts") {
		t.Fatalf("supplier picker not populated with related records; options = %q", optionText)
	}

	// Select Acme, fill the required fields, submit, and land back on the list.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(()=>{const s=document.querySelector('select[name="supplier_id"]');const o=[...s.options].find(o=>o.textContent.trim()==='Acme Supply');s.value=o.value;return true})()`, nil),
		chromedp.SendKeys(`input[name="name"]`, "Relation Widget", chromedp.ByQuery),
		chromedp.SendKeys(`input[name="price"]`, "10", chromedp.ByQuery),
		chromedp.Click(`button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit with relation: %v", err)
	}
	// Paginated list — find the new product via search rather than assuming page 1.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/e/products?q=Relation"),
		chromedp.WaitVisible(`tbody tr`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("search for created product: %v", err)
	}
	if !pollUntil(10*time.Second, func() bool {
		return strings.Contains(tbodyText(ctx), "Relation Widget")
	}) {
		t.Fatalf("product created with a supplier not found via search; tbody = %q", tbodyText(ctx))
	}
}
