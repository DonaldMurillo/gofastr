package entityui

import (
	"strings"
	"testing"
)

// A bulk list draws the bar as a form RPC to <api>/_bulk, a select
// column whose checkboxes join that form, the page's own ids as hidden
// fields, and the Export CSV download. Another owner's rows are on none
// of them.
func TestBulkListDrawsBarAndSelect(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	html := listHTML(t, x.ui.List("invoices").Bulk(), x.userCtx("/invoices", "", "u1"))
	for _, want := range []string{
		`action="/api/invoices/_bulk"`,
		`data-cui-rpc="/api/invoices/_bulk"`,
		`id="eui-invoices-bulk"`,
		`form="eui-invoices-bulk"`,
		`name="ids"`,
		`value="a1"`,
		"Select A-1",
		`name="page"`,
		`value="delete">Delete</option>`,
		`value="selected">Selected rows</option>`,
		`href="/api/invoices/_export.csv"`,
		"download",
		"Export CSV",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("bulk list missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, `value="b1"`) {
		t.Errorf("another owner's row reached the bulk form:\n%s", html)
	}
	if strings.Contains(html, `value="every"`) {
		t.Errorf("every match offered when the page already holds every row:\n%s", html)
	}
}

// Bulk is opt-in on an app page, and Display.NoBulk wins over .Bulk().
func TestBulkListOffUnlessAsked(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(html, "_bulk") || strings.Contains(html, "_export.csv") || strings.Contains(html, `name="ids"`) {
		t.Errorf("a list without .Bulk() drew bulk chrome:\n%s", html)
	}

	// The same signed-in caller on the same rows draws the bar until
	// Display.NoBulk turns it off, so the refusal is NoBulk's alone.
	ents := invoiceEntities()
	y := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	if html = listHTML(t, y.ui.List("invoices").Bulk(), y.userCtx("/invoices", "", "u1")); !strings.Contains(html, `data-cui-rpc="/api/invoices/_bulk"`) {
		t.Fatalf("control: the bulk bar is missing before NoBulk:\n%s", html)
	}
	inv := ents["invoices"]
	d := *inv.Display
	d.NoBulk = true
	inv.Display = &d
	ents["invoices"] = inv
	y = newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	html = listHTML(t, y.ui.List("invoices").Bulk(), y.userCtx("/invoices", "", "u1"))
	if strings.Contains(html, "_bulk") || strings.Contains(html, "_export.csv") {
		t.Errorf("Display.NoBulk still drew bulk chrome:\n%s", html)
	}
}

// Every match is offered once the matches run past the page, and not on
// a list with .Where pins, whose pins the every-match read would drop.
func TestBulkEveryMatchPastOnePage(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	html := listHTML(t, x.ui.List("invoices").Bulk().PageSize(1), x.userCtx("/invoices", "", "u1"))
	if !strings.Contains(html, `value="every">Every match (2)</option>`) {
		t.Errorf("every match missing past one page:\n%s", html)
	}
	html = listHTML(t, x.ui.List("invoices").Bulk().PageSize(1).Where("status", "draft"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(html, `value="every"`) {
		t.Errorf("every match offered on a pinned list:\n%s", html)
	}
}

// Cards have no select column, so the bar offers no "Selected rows".
func TestBulkCardsHaveNoSelectedScope(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	html := listHTML(t, x.ui.List("invoices").Bulk().As("cards"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(html, `value="selected"`) || strings.Contains(html, `name="ids"`) {
		t.Errorf("cards offered a selection they cannot make:\n%s", html)
	}
	if !strings.Contains(html, `value="page">This page (2)</option>`) {
		t.Errorf("cards lost the page scope:\n%s", html)
	}
}

// A keyed list's export link names its key, so the handler reads the
// namespaced params.
func TestBulkExportCarriesKey(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	html := listHTML(t, x.ui.List("invoices").Bulk().Key("inv"), x.userCtx("/invoices", "?inv_filter=status+%3D+%22draft%22", "u1"))
	if !strings.Contains(html, `href="/api/invoices/_export.csv?_list=inv&amp;inv_filter=status+%3D+%22draft%22"`) {
		t.Errorf("export link lost the key or the query:\n%s", html)
	}
	if !strings.Contains(html, `id="eui-inv-bulk"`) {
		t.Errorf("keyed bulk form not namespaced:\n%s", html)
	}
}
