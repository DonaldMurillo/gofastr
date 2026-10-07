package entityui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// activityBody renders inv-1's Activity tab as u1 over the given trail.
func activityBody(t *testing.T, x *testUI) string {
	t.Helper()
	return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Activity().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=activity", "u1")))
}

// The audit log stores wire names (issuedOn, customerId): a multi-word
// field's change is listed, and an unchanged one is not.
func TestActivityListsWireNamedFields(t *testing.T) {
	x := newInvoiceUI(t, withAudit(fakeAudit{entries: []AuditEntry{{
		At: time.Now(), Actor: "u1", Operation: "update",
		Before: map[string]any{"issuedOn": "2026-01-05", "customerId": "c1", "memo": "same"},
		After:  map[string]any{"issuedOn": "2026-02-09", "customerId": "c1", "memo": "same"},
	}}}))
	body := activityBody(t, x)
	if !strings.Contains(body, `data-cui-internal="">Issued On</span>`) {
		t.Fatalf("a wire-named field's change is missing:\n%s", body)
	}
	if strings.Contains(body, `data-cui-internal="">Customer</span>`) || strings.Contains(body, `data-cui-internal="">Memo</span>`) {
		t.Fatalf("an unchanged field is listed as a change:\n%s", body)
	}
}

// WithActorName names the actor, escaped; a resolver that panics leaves
// the id in its place and the tab still renders.
func TestActivityActorName(t *testing.T) {
	trail := withAudit(fakeAudit{entries: []AuditEntry{{At: time.Now(), Actor: "u1", Operation: "create"}}})

	x := newInvoiceUI(t, trail)
	x.ui = x.ui.WithActorName(func(_ context.Context, id string) string { return "<b>ana</b>@" + id })
	body := activityBody(t, x)
	if !strings.Contains(body, "&lt;b&gt;ana&lt;/b&gt;@u1") || strings.Contains(body, "<b>ana</b>") {
		t.Fatalf("the actor's name is missing or unescaped:\n%s", body)
	}

	x = newInvoiceUI(t, trail)
	x.ui = x.ui.WithActorName(func(context.Context, string) string { panic("account store down: secret-dsn") })
	body = activityBody(t, x)
	if !strings.Contains(body, "<strong>u1</strong>") {
		t.Fatalf("a panicking resolver must leave the id:\n%s", body)
	}
	if strings.Contains(body, "secret-dsn") {
		t.Fatalf("SECURITY: the resolver's panic text reached the page:\n%s", body)
	}
}

// SnapshotTitle names a record from stored values with no read; a masked
// title field never shows its stored value.
func TestSnapshotTitle(t *testing.T) {
	x := newInvoiceUI(t)
	ctx := x.userCtx("/rec/invoices", "", "u1")
	if got, ok := x.ui.SnapshotTitle(ctx, "invoices", map[string]any{"number": "INV-9"}); !ok || got != "INV-9" {
		t.Fatalf("SnapshotTitle = %q, %v; want INV-9", got, ok)
	}
	if _, ok := x.ui.SnapshotTitle(ctx, "no_such_entity", map[string]any{}); ok {
		t.Fatal("an unknown entity must answer false")
	}

	entities := invoiceEntities()
	cfg := entities["invoices"]
	d := *cfg.Display
	d.TitleField = "token"
	cfg.Display = &d
	entities["invoices"] = cfg
	x = newTestUI(t, entities, invoiceRows())
	got, ok := x.ui.SnapshotTitle(ctx, "invoices", map[string]any{"token": "tok-secret"})
	if !ok || got != "Invoice" {
		t.Fatalf("a masked title field must read as the singular name, got %q, %v", got, ok)
	}
}

// An update that moved only a stamp or a masked column reads as a save,
// with no change list: "made changes" would claim what it cannot show.
func TestActivityEmptyUpdateReadsAsSave(t *testing.T) {
	x := newInvoiceUI(t, withAudit(fakeAudit{entries: []AuditEntry{{
		At: time.Now(), Actor: "u1", Operation: "update",
		Before: map[string]any{"number": "INV-1", "token": "a", "updatedAt": "2026-01-01T00:00:00Z"},
		After:  map[string]any{"number": "INV-1", "token": "b", "updatedAt": "2026-01-02T00:00:00Z"},
	}}}))
	body := activityBody(t, x)
	if !strings.Contains(body, "<strong>u1</strong> saved this invoice") {
		t.Fatalf("an update with no visible change must read as a save:\n%s", body)
	}
	if strings.Contains(body, "made changes") || strings.Contains(body, "fui-change-list") {
		t.Fatalf("an update with no visible change claims changes:\n%s", body)
	}
}
