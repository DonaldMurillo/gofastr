package entityui

import (
	"fmt"
	"strings"
	"testing"
)

// manyInvoices is the invoice world with n invoices.
func manyInvoices(t *testing.T, n int) *testUI {
	t.Helper()
	rows := invoiceRows()
	rows["invoices"] = nil
	for i := 1; i <= n; i++ {
		rows["invoices"] = append(rows["invoices"], map[string]any{
			"id": fmt.Sprintf("inv-%02d", i), "number": fmt.Sprintf("INV-%02d", i), "status": "draft", "customer_id": "cus-1",
		})
	}
	return newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
}

// Under the rows the footer says which rows show out of how many, and
// offers 25, 50 and 100 rows per page as links back to page one that
// keep the rest of the URL. ?per= takes only a size on offer.
func TestListFooterRangeAndPerPage(t *testing.T) {
	x := manyInvoices(t, 30)
	h := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "?sort=number&page=2", "u1"))
	for _, want := range []string{
		"26–30 of 30",
		"Rows per page",
		`href="/invoices?per=50&amp;sort=number"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("the footer misses %q:\n%s", want, h)
		}
	}
	all := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "?per=50", "u1"))
	if !strings.Contains(all, "1–30 of 30") || !strings.Contains(all, "INV-30") {
		t.Errorf("per=50 did not show every row:\n%s", all)
	}
	odd := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "?per=7", "u1"))
	if !strings.Contains(odd, "1–25 of 30") {
		t.Errorf("a size not on offer was honoured:\n%s", odd)
	}
	fixed := listHTML(t, x.ui.List("invoices").PageSize(10), x.userCtx("/invoices", "?per=50", "u1"))
	if strings.Contains(fixed, "Rows per page") || !strings.Contains(fixed, "1–10 of 30") {
		t.Errorf("a builder's fixed size was overridden or offered a menu:\n%s", fixed)
	}
	few := listHTML(t, manyInvoices(t, 3).ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(few, "Rows per page") || !strings.Contains(few, "1–3 of 3") {
		t.Errorf("a list that fits one page offered sizes, or lost its range:\n%s", few)
	}
}
