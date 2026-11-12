package entityui

import (
	stdhtml "html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The export sits under the REST list's path but read only entityui's
// own list params, so a REST filter (?status=paid), a misspelt key or a
// cursor was ignored and the file held every row the caller may read:
// the opposite of what the list asked for. It now refuses a parameter
// that is not the list's own, and the list's export link carries only
// its own params, never another keyed list's.
func TestExportRefusesForeignParams(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	for _, query := range []string{
		"status=paid", "nosuchfield=1", "cursor=ZZ", "status%5Beq%5D=paid",
		"_list=inv&other_q=x", "_list=inv&filter=status+%3D+%22paid%22",
		"q=A",
	} {
		rec, _ := getExport(t, x, bulkCtx("u1", nil), query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "export_unknown_param") {
			t.Errorf("?%s = %d %s, want 400 export_unknown_param", query, rec.Code, rec.Body)
		}
	}
	for _, query := range []string{
		"", "filter=number+%3D+%22A-2%22", "q=", "_list=inv&inv_filter=number+%3D+%22A-2%22",
		"_id=a1&_id=a2", "sort=number&dir=desc&page=2&cols=number&as=cards&per=50",
	} {
		if rec, _ := getExport(t, x, bulkCtx("u1", nil), query); rec.Code != http.StatusOK {
			t.Errorf("?%s = %d %s, want 200", query, rec.Code, rec.Body)
		}
	}

	html := listHTML(t, x.ui.List("invoices").Bulk().Key("inv"), x.userCtx("/invoices", "?other_q=zzz&inv_filter=number+%3D+%22A-2%22", "u1"))
	m := regexp.MustCompile(`href="/api/invoices/_export\.csv\?([^"]*)"`).FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no export link:\n%s", html)
	}
	query := stdhtml.UnescapeString(m[1])
	if strings.Contains(query, "other_q") {
		t.Errorf("export link carries another list's param: %s", query)
	}
	rec, rows := getExport(t, x, bulkCtx("u1", nil), query)
	if rec.Code != http.StatusOK || len(rows) != 2 {
		t.Errorf("export link ?%s = %d with %d lines, want 200 with the header and A-2", query, rec.Code, len(rows))
	}
}

// An f_ param naming a field that is no facet of the list was taken as
// the list's own, and the export, which narrows only by configured
// facets, ignored it: ?f_status=paid answered every row with a 200. It
// is refused now; a configured facet still narrows.
func TestExportRefusesNonFacetParam(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	for _, query := range []string{"f_status=paid", "_list=inv&inv_f_status=paid"} {
		rec, _ := getExport(t, x, bulkCtx("u1", nil), query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "export_unknown_param") {
			t.Errorf("?%s = %d %s, want 400 export_unknown_param", query, rec.Code, rec.Body)
		}
	}

	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	display := *inv.Display
	display.Facets = []string{"status"}
	inv.Display = &display
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "status": "paid", "owner_id": "u1"},
	}
	faceted := newTestUIExt(t, ents, rows, Extensions{}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	if rec, rows := getExport(t, faceted, bulkCtx("u1", nil), "f_status=paid"); rec.Code != http.StatusOK || len(rows) != 2 {
		t.Errorf("facet ?f_status=paid = %d with %d lines, want 200 with the header and A-2", rec.Code, len(rows))
	}
}

// ?q= on an entity with no SearchFields was taken as the list's own and
// then ignored: the export answered 200 with every row while the REST
// list refuses the same ?q= with search_not_configured. It is refused
// now, the export link leaves it off, and a searchable entity still
// narrows by it.
func TestExportRefusesUnsearchableQ(t *testing.T) {
	x := ownedInvoices(t, Extensions{})
	for _, query := range []string{"q=B-1", "_list=inv&inv_q=B-1"} {
		rec, _ := getExport(t, x, bulkCtx("u1", nil), query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "export_unknown_param") {
			t.Errorf("?%s = %d %s, want 400 export_unknown_param", query, rec.Code, rec.Body)
		}
	}
	html := listHTML(t, x.ui.List("invoices").Bulk(), x.userCtx("/invoices", "?q=zzz", "u1"))
	if m := regexp.MustCompile(`href="/api/invoices/_export\.csv(\?[^"]*)?"`).FindStringSubmatch(html); m == nil {
		t.Errorf("no export link:\n%s", html)
	} else if strings.Contains(m[1], "q=") {
		t.Errorf("export link carries an unsearchable q: %s", m[1])
	}

	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	inv.SearchFields = []string{"number"}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "status": "draft", "owner_id": "u1"},
	}
	searchable := newTestUIExt(t, ents, rows, Extensions{}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	if rec, rows := getExport(t, searchable, bulkCtx("u1", nil), "q=A-2"); rec.Code != http.StatusOK || len(rows) != 2 {
		t.Errorf("searchable ?q=A-2 = %d with %d lines, want 200 with the header and A-2", rec.Code, len(rows))
	}
}
