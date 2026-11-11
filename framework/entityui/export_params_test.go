package entityui

import (
	stdhtml "html"
	"net/http"
	"regexp"
	"strings"
	"testing"
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
	} {
		rec, _ := getExport(t, x, bulkCtx("u1", nil), query)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "export_unknown_param") {
			t.Errorf("?%s = %d %s, want 400 export_unknown_param", query, rec.Code, rec.Body)
		}
	}
	for _, query := range []string{
		"", "filter=number+%3D+%22A-2%22", "q=A", "_list=inv&inv_filter=number+%3D+%22A-2%22",
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
