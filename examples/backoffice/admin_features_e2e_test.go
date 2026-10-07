package main

// Browser-level (chromedp) e2e for the admin's list and record features on
// top of the basic CRUD flows: column sorting (a sort header is a link the
// client router intercepts), search (filters server-side), the record
// opened from its row, and the BelongsTo relationship picker. These
// exercise the runtime path the httptest tests in battery/admin can't
// reach.
//
// Gated by -short, like the other backoffice e2e.

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestBackofficeE2E_SortByName clicks the "name" column header and asserts
// the list re-rendered ascending: the alphabetically-first product leads
// and the header reports its sort.
func TestBackofficeE2E_SortByName(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	sortSel := `thead a[href*="sort=name"]`
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(sortSel, chromedp.ByQuery),
		chromedp.Click(sortSel, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("click sort: %v", err)
	}
	if !pollUntil(10*time.Second, func() bool {
		var sorted bool
		_ = chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('th[aria-sort="ascending"] a[href*="sort=name"]')`, &sorted))
		return sorted && strings.Contains(firstRowText(ctx), "Circular Saw")
	}) {
		t.Fatalf("sort not applied; first row = %q", firstRowText(ctx))
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

// TestBackofficeE2E_RecordFromRow clicks a row's title link and asserts the
// record opens holding that product's values and the supplier picker.
func TestBackofficeE2E_RecordFromRow(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	link := `tbody tr:first-child td[data-label="Name"] a`
	var name string
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(link, chromedp.ByQuery),
		chromedp.Text(link, &name, chromedp.ByQuery),
		chromedp.Click(link, chromedp.ByQuery),
		chromedp.WaitVisible(productForm+` input[name="name"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("open record: %v", err)
	}
	var value string
	var picker bool
	if err := chromedp.Run(ctx,
		chromedp.Value(productForm+` input[name="name"]`, &value, chromedp.ByQuery),
		chromedp.Evaluate(`!!document.querySelector('`+productForm+` select[name="supplier_id"]')`, &picker),
	); err != nil {
		t.Fatalf("read record: %v", err)
	}
	if value != strings.TrimSpace(name) || !picker {
		t.Fatalf("record holds name %q (row said %q), supplier picker %t", value, name, picker)
	}
}

// TestBackofficeE2E_RelationDropdown asserts the product form renders a
// supplier <select> populated with the seeded suppliers, selects one, and
// submits, proving the relationship picker is wired end to end.
func TestBackofficeE2E_RelationDropdown(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	picker := productForm + ` select[name="supplier_id"]`
	var optionText string
	if err := chromedp.Run(ctx,
		chromedp.Click(`a[href="/admin/entities/products/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(picker, chromedp.ByQuery),
		chromedp.Evaluate(`[...document.querySelector('`+picker+`').options].map(o=>o.textContent).join('|')`, &optionText),
	); err != nil {
		t.Fatalf("open product form: %v", err)
	}
	if !strings.Contains(optionText, "Acme Supply") || !strings.Contains(optionText, "Globex Parts") {
		t.Fatalf("supplier picker not populated with related records; options = %q", optionText)
	}

	// Select Acme, fill the required fields, submit, and land back on the list.
	if err := chromedp.Run(ctx,
		chromedp.SetValue(picker, acmeID(t, ctx, picker), chromedp.ByQuery),
		chromedp.SendKeys(productForm+` input[name="name"]`, "Relation Widget", chromedp.ByQuery),
		chromedp.SendKeys(productForm+` input[name="price"]`, "10", chromedp.ByQuery),
		chromedp.Click(productForm+` button[type=submit]`, chromedp.ByQuery),
		chromedp.WaitVisible(`table`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("submit with relation: %v", err)
	}
	// Paginated list, find the new product via search rather than assuming page 1.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin/entities/products?q=Relation"),
		chromedp.WaitVisible(`tbody tr`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("search for created product: %v", err)
	}
	if !pollUntil(10*time.Second, func() bool {
		body := tbodyText(ctx)
		return strings.Contains(body, "Relation Widget") && strings.Contains(body, "Acme Supply")
	}) {
		t.Fatalf("product created with a supplier not found via search; tbody = %q", tbodyText(ctx))
	}
}

// acmeID is the picker option value for Acme Supply.
func acmeID(t *testing.T, ctx context.Context, picker string) string {
	t.Helper()
	var id string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`[...document.querySelector('`+picker+`').options].find(o=>o.textContent.trim()==='Acme Supply')?.value || ''`, &id)); err != nil || id == "" {
		t.Fatalf("no Acme Supply option (err=%v)", err)
	}
	return id
}
