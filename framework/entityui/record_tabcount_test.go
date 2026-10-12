package entityui

import (
	"context"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// tabBadge is the count text the strip draws on the ?tab=key link, or
// "" when it draws none.
func tabBadge(t *testing.T, body, key string) string {
	t.Helper()
	re := regexp.MustCompile(`\?tab=` + key + `"[^>]*>[^<]*(?:<span [^>]*class="fui-tab-nav__badge"[^>]*>([^<]*)</span>)?`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no %s tab:\n%s", key, body)
	}
	return m[1]
}

// Related counts the rows pointing at this record; Activity counts its
// trail entries.
func TestRecordTabsCountTheirRows(t *testing.T) {
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"], map[string]any{"id": "inv-2", "number": "INV-2", "amount": "5.00", "status": "draft", "customer_id": "cus-1"})
	rows["payments"] = []map[string]any{
		{"id": "p1", "invoice_id": "inv-1", "amount": "10.00"},
		{"id": "p2", "invoice_id": "inv-1", "amount": "20.00"},
		{"id": "p3", "invoice_id": "inv-2", "amount": "30.00"},
	}
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments",
	}), withAudit(fakeAudit{entries: []AuditEntry{{Operation: "create"}, {Operation: "update"}, {Operation: "update"}}}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").Activity().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	if got := tabBadge(t, body, "related"); got != "2" {
		t.Errorf("Related counts %q, want 2", got)
	}
	if got := tabBadge(t, body, "activity"); got != "3" {
		t.Errorf("Activity counts %q, want 3", got)
	}
	if got := tabBadge(t, body, "edit"); got != "" {
		t.Errorf("Edit draws a count %q", got)
	}
}

// The Related count reads through the related entity's own scope: a
// payment another user owns is not counted, the way the list does not
// draw it.
func TestRecordRelatedCountIsScoped(t *testing.T) {
	installOwnerExtractor(t)
	ents := invoiceEntities()
	pay := ents["payments"]
	pay.Fields = append(pay.Fields, schema.Field{Name: "user_id", Type: schema.String, Hidden: true})
	pay.Scope = &entity.ScopeConfig{OwnerField: "user_id"}
	ents["payments"] = pay
	rows := invoiceRows()
	rows["payments"] = []map[string]any{
		{"id": "p1", "invoice_id": "inv-1", "amount": "10.00", "user_id": "u1"},
		{"id": "p2", "invoice_id": "inv-1", "amount": "20.00", "user_id": "u2"},
		{"id": "p3", "invoice_id": "inv-1", "amount": "30.00", "user_id": "u2"},
	}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices", "payments": "/api/payments"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	if got := tabBadge(t, body, "related"); got != "1" {
		t.Errorf("SECURITY: Related counts %q for u1, want 1 (two payments belong to u2)", got)
	}
}

// A BeforeList scope on the related entity narrows the count as it
// narrows the list.
func TestRecordRelatedCountHonorsBeforeList(t *testing.T) {
	rows := invoiceRows()
	rows["payments"] = []map[string]any{
		{"id": "p1", "invoice_id": "inv-1", "amount": "10.00"},
		{"id": "p2", "invoice_id": "inv-1", "amount": "20.00"},
	}
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{"invoices": "/api/invoices", "payments": "/api/payments"}))
	ch, err := x.host.Crud(mustEntity(t, x, "payments"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("amount < $1", "15")
		return nil
	})
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	if got := tabBadge(t, body, "related"); got != "1" {
		t.Errorf("SECURITY: Related counts %q past the BeforeList scope, want 1", got)
	}
}
