package entityui

import (
	"bytes"
	stdhtml "html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// viewInvoices is the invoices fixture with an "open" view and four rows:
// one draft, two open, one paid.
func viewInvoices(t *testing.T, def bool) *testUI {
	t.Helper()
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Display.Views = []entity.ListView{{Key: "open", Where: `status = "open"`, Default: def}}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"],
		map[string]any{"id": "o1", "number": "O-1", "status": "open"},
		map[string]any{"id": "o2", "number": "O-2", "status": "open"},
		map[string]any{"id": "p1", "number": "P-1", "status": "paid"})
	return newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
}

// hiddenValue reads one hidden input's value out of rendered HTML.
func hiddenValue(t *testing.T, page, name string) string {
	t.Helper()
	m := regexp.MustCompile(`<input name="` + name + `" type="hidden"(?: value="([^"]*)")?>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no hidden %q input:\n%s", name, page)
	}
	return stdhtml.UnescapeString(m[1])
}

// Every match runs over the rows the screen showed: a builder's View
// rides the bar's query, so a delete on the open view leaves the draft
// and the paid row alone.
func TestBulkEveryMatchKeepsBuilderView(t *testing.T) {
	x := viewInvoices(t, false)
	page := listHTML(t, x.ui.List("invoices").View("open").PageSize(1).Bulk().Delete(), x.userCtx("/invoices", "", "u1"))
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "every",
		"query": hiddenValue(t, page, "query"), "count": hiddenValue(t, page, "count"),
	})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, out)
	}
	if got := invoiceIDs(t, x); !slices.Equal(got, []string{"inv-1", "p1"}) {
		t.Fatalf("rows left = %v, want inv-1 p1 (only the open view's rows)", got)
	}
}

// Every match refuses when the rows it would touch are not the count the
// screen offered: the list changed, or the query was not the screen's.
func TestBulkEveryMatchStaleCountRefused(t *testing.T) {
	x := viewInvoices(t, false)
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "every", "query": "view=open", "count": "3",
	})
	if code != http.StatusConflict {
		t.Fatalf("status %d: %v, want 409", code, out)
	}
	code, _ = postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "every", "query": "view=open",
	})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("no count: status %d, want 422", code)
	}
	if got := invoiceIDs(t, x); len(got) != 4 {
		t.Fatalf("a refused every match deleted rows: %v", got)
	}
}

// The Export link carries a builder's View, so the file holds the rows
// the screen showed.
func TestExportKeepsBuilderView(t *testing.T) {
	x := viewInvoices(t, false)
	page := listHTML(t, x.ui.List("invoices").View("open").Bulk(), x.userCtx("/invoices", "", "u1"))
	m := regexp.MustCompile(`href="/api/invoices/_export\.csv\?([^"]*)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no export link:\n%s", page)
	}
	rec, rows := getExport(t, x, bulkCtx("u1", nil), stdhtml.UnescapeString(m[1]))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if len(rows) != 3 {
		t.Fatalf("%d lines, want a header and the 2 open rows:\n%s", len(rows), rec.Body.String())
	}
}

// A list pinned with Where draws no Export: the export route reads the
// query, and a pin is not in it.
func TestExportHiddenOnPinnedList(t *testing.T) {
	x := viewInvoices(t, false)
	page := listHTML(t, x.ui.List("invoices").Where("status", "open").Bulk(), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(page, "_export.csv") {
		t.Fatalf("a pinned list drew an Export link:\n%s", page)
	}
}

// With a default view, the All tab still reaches every row: it names
// the reserved "all" view rather than dropping the param.
func TestAllTabReachableWithDefaultView(t *testing.T) {
	x := viewInvoices(t, true)
	page := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if !strings.Contains(page, `href="/invoices?view=all"`) {
		t.Fatalf("the All tab does not name the all view:\n%s", page)
	}
	all := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "?view=all", "u1"))
	if !strings.Contains(all, "P-1") {
		t.Fatalf("?view=all hid the paid row:\n%s", all)
	}
}

// The bulk route takes JSON only: a text/plain body, which a cross-site
// form can send without a preflight, is refused before it is read.
func TestBulkRefusesNonJSONBody(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	for _, ct := range []string{"text/plain", "application/x-www-form-urlencoded", ""} {
		req := httptest.NewRequest(http.MethodPost, "/api/invoices/_bulk",
			bytes.NewReader([]byte(`{"action":"delete","scope":"selected","ids":"a1"}`))).WithContext(bulkCtx("u1", nil))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		x.ui.BulkHandler("invoices").ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status %d, want 415", ct, rec.Code)
		}
	}
	if got := invoiceIDs(t, x); len(got) != 4 {
		t.Fatalf("a non-JSON body deleted rows: %v", got)
	}
}

// Only ?prefill_<field>= prefills: a bare ?<field>= on the create page
// is some other param and is ignored.
func TestCreatePrefillNeedsPrefix(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?"+url.Values{"customer_id": {"cus-1"}}.Encode(), "u1")))
	if strings.Contains(body, `selected="" value="cus-1"`) {
		t.Fatalf("a bare ?customer_id= prefilled the form:\n%s", body)
	}
}
