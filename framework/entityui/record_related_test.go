package entityui

import (
	"strings"
	"testing"
)

// RelatedAt with an empty base draws the related list with no record
// links, no row menu and no New: the entity has no screen to link to.
func TestRelatedAtEmptyBaseDrawsNoLinks(t *testing.T) {
	rows := invoiceRows()
	rows["payments"] = []map[string]any{{"id": "pay-1", "invoice_id": "inv-1", "amount": "40.00"}}
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RelatedAt("payments", "").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if !strings.Contains(body, `id="eui-related-payments"`) || !strings.Contains(body, "40.00") {
		t.Fatalf("the payments section or its row is missing:\n%s", body)
	}
	section := body[strings.Index(body, `id="eui-related-payments"`):]
	for _, bad := range []string{"/pay-1", "/create", "fui-menu", "fui-copy"} {
		if strings.Contains(section, bad) {
			t.Errorf("a no-link related list drew %q:\n%s", bad, section)
		}
	}
	// One data column (the pinned invoice_id drops out) and no actions column.
	if n := strings.Count(section, "<th "); n != 1 {
		t.Errorf("no-link related list has %d header cells, want 1:\n%s", n, section)
	}
}

// RelatedAt with a base hangs the list's links off it rather than the
// derived sibling path.
func TestRelatedAtBaseOverridesDerived(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RelatedAt("payments", "/money/payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if !strings.Contains(body, `href="/money/payments/create?prefill_invoice_id=inv-1"`) {
		t.Fatalf("New does not hang off the given base:\n%s", body)
	}
	if strings.Contains(body, "/rec/payments") {
		t.Fatalf("the derived base leaked past RelatedAt:\n%s", body)
	}
}

// The record keeps one <h1>: a related list is a section of the record,
// so its heading sits below the record's title.
func TestRelatedListsKeepOneH1(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if n := strings.Count(body, "<h1"); n != 1 {
		t.Fatalf("record with a related list has %d <h1>, want 1:\n%s", n, body)
	}
}
