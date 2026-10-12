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
		chromedp.Evaluate(`!!document.querySelector('`+productForm+` [data-hui-combobox-pick] input[type="hidden"][name="supplier_id"]')`, &picker),
	); err != nil {
		t.Fatalf("read record: %v", err)
	}
	if value != strings.TrimSpace(name) || !picker {
		t.Fatalf("record holds name %q (row said %q), supplier picker %t", value, name, picker)
	}
}

// TestBackofficeE2E_RelationDropdown asserts the product form renders a
// supplier picker listing the seeded suppliers, searches it, picks one,
// and submits, proving the relationship picker is wired end to end.
func TestBackofficeE2E_RelationDropdown(t *testing.T) {
	if testing.Short() {
		t.Skip("chromedp e2e: skipped under -short")
	}
	base := backofficeServer(t)
	ctx := backofficeBrowser(t)
	login(t, ctx, base)
	waitHydrated(t, ctx)

	search := productForm + ` [data-hui-combobox-pick] input[role="combobox"]`
	picked := productForm + ` [data-hui-combobox-pick] input[type="hidden"][name="supplier_id"]`
	var optionText string
	if err := chromedp.Run(ctx,
		chromedp.Click(`a[href="/admin/entities/products/create"]`, chromedp.ByQuery),
		chromedp.WaitVisible(search, chromedp.ByQuery),
		chromedp.Evaluate(`[...document.querySelectorAll('`+productForm+` [data-hui-combobox-pick] [role="option"]')].map(o=>o.dataset.label).join('|')`, &optionText),
	); err != nil {
		t.Fatalf("open product form: %v", err)
	}
	if !strings.Contains(optionText, "Acme Supply") || !strings.Contains(optionText, "Globex Parts") {
		t.Fatalf("supplier picker not populated with related records; options = %q", optionText)
	}

	// Search for Acme and pick it: the hidden input takes its id.
	acme := productForm + ` [data-hui-combobox-pick] [role="option"][data-label="Acme Supply"]`
	var id string
	if err := chromedp.Run(ctx,
		chromedp.Click(search, chromedp.ByQuery),
		chromedp.SendKeys(search, "Acme", chromedp.ByQuery),
		chromedp.WaitVisible(acme, chromedp.ByQuery),
		chromedp.Click(acme, chromedp.ByQuery),
		chromedp.Value(picked, &id, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("pick a supplier: %v", err)
	}
	if id == "" {
		t.Fatal("picking Acme Supply left the supplier empty")
	}

	// Fill the required fields, submit, and land back on the list.
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(productForm+` input[name="name"]`, "Relation Widget", chromedp.ByQuery),
		chromedp.SendKeys(productForm+` input[name="price"]`, "10", chromedp.ByQuery),
		chromedp.Click(productSave, chromedp.ByQuery),
		// The create form opens as a drawer over the list, so the table
		// is visible before the save lands; the drawer closing is the
		// signal. Navigating sooner meets the dirty form's leave guard.
		chromedp.WaitNotPresent(productForm, chromedp.ByQuery),
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
