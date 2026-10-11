package entityui

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Every match runs over the rows the screen offered, not just as many
// rows: one leaves the view and another enters it, the count is the same,
// and the run is refused instead of touching the row nobody confirmed.
func TestEveryMatchRefusesSwappedRows(t *testing.T) {
	x := viewInvoices(t, false)
	page := listHTML(t, x.ui.List("invoices").View("open").PageSize(1).Bulk().Delete(), x.userCtx("/invoices", "", "u1"))
	match := hiddenValue(t, page, "match")
	if _, err := x.db.Exec(`UPDATE invoices SET status = 'paid' WHERE id = 'o2'`); err != nil {
		t.Fatal(err)
	}
	if _, err := x.db.Exec(`UPDATE invoices SET status = 'open' WHERE id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{
		"action": "delete", "scope": "every",
		"query": hiddenValue(t, page, "query"), "match": match,
	})
	if code != http.StatusConflict {
		t.Fatalf("SECURITY: status %d: %v; the run would delete p1, which entered the view after the screen drew it", code, out)
	}
	if got := invoiceIDs(t, x); !slices.Contains(got, "p1") || !slices.Contains(got, "o1") {
		t.Fatalf("rows left = %v; a refused run must touch nothing", got)
	}
}

// The digest covers the ids, each length-prefixed, so two different id
// sets never share one.
func TestMatchDigestSeparatesIDs(t *testing.T) {
	if matchDigest([]string{"a,b"}) == matchDigest([]string{"a", "b"}) {
		t.Fatal("a,b and a+b share a digest")
	}
	if matchDigest([]string{"b", "a"}) != matchDigest([]string{"a", "b"}) {
		t.Fatal("the digest depends on order")
	}
	if matchDigest(nil) == matchDigest([]string{""}) {
		t.Fatal("no ids and one empty id share a digest")
	}
}

// A list whose matches pass the cap offers no every-match scope: the run
// would be refused, so the bar does not draw a choice that cannot work.
func TestEveryMatchOverCapNotOffered(t *testing.T) {
	x, _, _ := guardedInvoices(t, entity.AccessControl{}, EveryMatchCap+1, Extensions{}, nil)
	page := listHTML(t, x.ui.List("invoices").PageSize(10).Bulk().Delete(), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(page, `name="match"`) {
		t.Fatal("the bar offers every match past the cap")
	}
}
