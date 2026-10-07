package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// paymentsWithViews gives payments a search field and a view, so a full
// list draws its search box and view tabs.
func paymentsWithViews() map[string]entity.EntityConfig {
	ents := invoiceEntities()
	pay := ents["payments"]
	pay.Fields = append(pay.Fields, schema.Field{Name: "method", Type: schema.String})
	pay.SearchFields = []string{"method"}
	pay.Display = &entity.DisplayConfig{Views: []entity.ListView{{Key: "card", Label: "Card", Where: `method = "card"`}}}
	ents["payments"] = pay
	return ents
}

func relatedTabHTML(t *testing.T, x *testUI) string {
	t.Helper()
	b := x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments")
	return string(b.RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
}

// A Related list is a section, not a page: a heading with its count and
// a small Add button, the table, and no search, filters or view tabs.
func TestRelatedListIsEmbedded(t *testing.T) {
	rows := invoiceRows()
	rows["payments"] = []map[string]any{
		{"id": "p1", "invoice_id": "inv-1", "amount": "10.00", "method": "card"},
		{"id": "p2", "invoice_id": "inv-1", "amount": "20.00", "method": "cash"},
	}
	x := newTestUI(t, paymentsWithViews(), rows, withAPI(map[string]string{"invoices": "/api/invoices", "payments": "/api/payments"}))
	body := relatedTabHTML(t, x)
	sec := body[strings.Index(body, `id="eui-related-payments"`):]
	for _, gone := range []string{"fui-filter-toolbar", "fui-tab-nav", "fui-button--primary", "New Payment"} {
		if strings.Contains(sec, gone) {
			t.Errorf("the related list drew %q:\n%s", gone, sec)
		}
	}
	add := `href="/rec/payments/create?prefill_invoice_id=inv-1"`
	i := strings.Index(sec, add)
	if i < 0 {
		t.Fatalf("no Add link with the foreign key prefilled:\n%s", sec)
	}
	open := sec[strings.LastIndex(sec[:i], "<a "):i]
	if !strings.Contains(open, "fui-button--secondary") || !strings.Contains(open, "fui-button--small") {
		t.Errorf("Add is not a small secondary button: %s", open)
	}
	if !strings.Contains(sec, ">Add payment<") {
		t.Errorf("Add is not labelled with the singular noun:\n%s", sec)
	}
	if !strings.Contains(sec, `<span class="fui-muted" data-cui-comp="ui-muted">2</span>`) {
		t.Errorf("the heading carries no row count:\n%s", sec)
	}
	if !strings.Contains(sec, "<table") {
		t.Errorf("the rows are not a table:\n%s", sec)
	}
}

// An empty Related list is one compact line: no table head, no second
// call to action.
func TestRelatedListEmptyIsOneLine(t *testing.T) {
	x := newTestUI(t, paymentsWithViews(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "payments": "/api/payments"}))
	body := relatedTabHTML(t, x)
	sec := body[strings.Index(body, `id="eui-related-payments"`):]
	if !strings.Contains(sec, "fui-empty-state--compact") {
		t.Fatalf("the empty related list is not compact:\n%s", sec)
	}
	if strings.Contains(sec, "<table") {
		t.Errorf("the empty related list drew a table:\n%s", sec)
	}
	if n := strings.Count(sec, "/rec/payments/create"); n != 1 {
		t.Errorf("the empty related list offers Add %d times, want once:\n%s", n, sec)
	}
}

// A page list is unchanged: its New stays primary and its search stays.
func TestPageListIsNotEmbedded(t *testing.T) {
	x := newTestUI(t, paymentsWithViews(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "payments": "/api/payments"}))
	body := listHTML(t, x.ui.List("payments"), x.userCtx("/rec/payments", "", "u1"))
	for _, want := range []string{"fui-filter-toolbar", "fui-button--primary", "New Payment"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page list lost %q", want)
		}
	}
}
