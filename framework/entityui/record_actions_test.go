package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A move draws a button only when its From holds the stored value, a
// System move never draws one, and the button posts the transition
// route.
func TestRecordTransitionButtonsFromStoredState(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", nil)

	if !strings.Contains(body, `data-cui-rpc="/api/invoices/inv-1/transitions/send"`) {
		t.Fatalf("the draft's open move must post its route:\n%s", body)
	}
	// mark_paid starts from open, not draft: no button.
	if strings.Contains(body, "transitions/mark_paid") {
		t.Fatalf("a move whose From does not hold the stored value draws no button:\n%s", body)
	}
	// sweep is System: no button, ever.
	if strings.Contains(body, "transitions/sweep") {
		t.Fatalf("a System move draws no button:\n%s", body)
	}
	if !strings.Contains(body, `data-cui-rpc-success-toast="Invoice updated"`) {
		t.Fatalf("the move's success carries the moved toast:\n%s", body)
	}
}

// A transition's Permission gates its button with the same resource
// check the route runs: a caller without it never sees the move.
func TestRecordTransitionPermissionHidesButton(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.States.Transitions = append(inv.States.Transitions, entity.Transition{
		Key: "audit", From: []string{"draft"}, To: "paid", Permission: "invoices:audit",
	})
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, "transitions/audit") {
		t.Fatalf("a caller without the move's Permission sees no button:\n%s", body)
	}
	if !strings.Contains(body, "transitions/send") {
		t.Fatalf("the permission-free move stays:\n%s", body)
	}
}

// A Wildcard role does not hold a move's Permission, the route's own
// exact check, so it sees no button the route would refuse; a role
// granted the capability by name does.
func TestRecordMovePermissionIsExact(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.States.Transitions = append(inv.States.Transitions, entity.Transition{
		Key: "audit", From: []string{"draft"}, To: "paid", Permission: "invoices:audit",
	})
	entities["invoices"] = inv
	policy := access.NewRolePolicy()
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("auditor", "invoices:audit"); err != nil {
		t.Fatal(err)
	}
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	render := func(role string) string {
		ctx := access.WithRoles(access.WithPolicy(x.userCtx("/rec/invoices/inv-1", "", "u1"), policy), []string{role})
		return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(ctx))
	}
	if body := render("root"); strings.Contains(body, "transitions/audit") {
		t.Fatalf("a Wildcard role sees a move the route refuses it:\n%s", body)
	}
	if body := render("auditor"); !strings.Contains(body, "transitions/audit") {
		t.Fatalf("the named grant lost its move:\n%s", body)
	}
}

// An entity with no REST write routes renders read-only: no form RPC,
// no delete, no move buttons — every field a value, nothing submittable.
func TestRecordNoAPIIsReadOnly(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows())
	body := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Delete().Duplicate() })

	if strings.Contains(body, "data-cui-rpc") {
		t.Fatalf("a read-only record carries no RPC:\n%s", body)
	}
	if strings.Contains(body, "transitions/") || strings.Contains(body, ">Delete<") {
		t.Fatalf("a read-only record draws no moves and no delete:\n%s", body)
	}
	if hasSubmittableControl(body, "number") {
		t.Fatalf("a read-only record submits nothing:\n%s", body)
	}
	if !strings.Contains(body, "INV-1") {
		t.Fatalf("the read-only record still shows its values:\n%s", body)
	}
}

// ?duplicate= prefills from that record minus what a create may not
// set: system fields, the state field, stamps, unique fields and
// masked fields.
func TestRecordDuplicateDropsWhatCreateMayNotSet(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))

	if v := attrValue(body, "number", "value"); v != "" {
		t.Fatalf("the unique field must start blank, got %q:\n%s", v, body)
	}
	if hasSubmittableControl(body, "status") || hasSubmittableControl(body, "issued_on") {
		t.Fatalf("guarded fields never prefill:\n%s", body)
	}
}

// ?prefill_<field>= is the convention the Related tab's New link uses:
// a value a create may set lands selected, one it may not (a Locked
// field, the guarded state field) is ignored.
func TestCreatePrefillQueryConvention(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?prefill_customer_id=cus-1&prefill_status=paid&prefill_amount=500.00", "u1")))

	if !strings.Contains(body, `selected="" value="cus-1"`) {
		t.Fatalf("the prefillable foreign key lands selected:\n%s", body)
	}
	if strings.Contains(body, `selected="" value="paid"`) {
		t.Fatalf("the guarded state field ignores its prefill:\n%s", body)
	}
	if strings.Contains(body, "500.00") {
		t.Fatalf("a Locked field ignores its prefill:\n%s", body)
	}
}
