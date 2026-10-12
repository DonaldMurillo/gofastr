package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// emptyRegion is the list's empty-state block: the header's New sits
// outside it.
func emptyRegion(t *testing.T, html string) string {
	t.Helper()
	i := strings.Index(html, `data-cui-comp="ui-empty-state"`)
	if i < 0 {
		t.Fatalf("no empty state:\n%s", html)
	}
	return html[i:]
}

// A search that matches nothing says so and offers to clear it; the
// sort resets as on any narrowing link, and New is not offered.
func TestEmptySearchOffersClear(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", `?q=ZZZ&sort=number`, "u1"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices match") || strings.Contains(html, "No invoices yet") {
		t.Fatalf("a search with no match drew the wrong empty state:\n%s", empty)
	}
	if !strings.Contains(empty, `href="/invoices"`) || !strings.Contains(empty, "Clear search and filters") {
		t.Errorf("the clear link does not land on the bare list:\n%s", empty)
	}
	if strings.Contains(empty, "/create") {
		t.Errorf("a list narrowed to nothing offered New:\n%s", empty)
	}
}

// Clearing a filter keeps the view it was narrowing.
func TestEmptyFilterClearKeepsView(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", `?view=drafts&filter=number+%3D+%22A-2%22`, "u1"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices match") || !strings.Contains(empty, `href="/invoices?view=drafts"`) {
		t.Errorf("a filtered view's clear link lost the view:\n%s", empty)
	}
}

// A facet narrows the list the way a search does.
func TestEmptyFacetOffersClear(t *testing.T) {
	x := countedInvoices(t, func(d *entity.DisplayConfig) { d.Facets = []string{"status"} })
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", `?f_status=open`, "u1"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices match") || !strings.Contains(empty, `href="/invoices"`) {
		t.Errorf("a facet with no match drew the wrong empty state:\n%s", empty)
	}
}

// An open saved view whose filter matches nothing clears to the bare
// list: its filter is what narrows it.
func TestEmptySavedViewClears(t *testing.T) {
	x := countedInvoices(t)
	store := newMemSavedViews()
	x.ui = x.ui.WithSavedViews(store)
	store.put("u1", SavedView{ID: "sv-open", Entity: "invoices", Name: "Open", Filter: `status = "open"`})
	html := listHTML(t, x.ui.List("invoices").SavedViews(), x.userCtx("/invoices", `?saved=sv-open`, "u1"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices match") || !strings.Contains(empty, `href="/invoices"`) {
		t.Errorf("a saved view with no match did not clear to the bare list:\n%s", empty)
	}
}

// A view with no rows says the others may hold some, with no New.
func TestEmptyViewNamesTheView(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", `?view=drafts`, "u3"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices in this view") || strings.Contains(empty, "/create") {
		t.Errorf("an empty view drew the wrong empty state:\n%s", empty)
	}
}

// An unnarrowed empty list keeps its own state and New.
func TestEmptyListOffersNew(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u3"))
	empty := emptyRegion(t, html)
	if !strings.Contains(empty, "No invoices yet") || !strings.Contains(empty, "/create") {
		t.Errorf("an empty list lost its own state or New:\n%s", empty)
	}
}
