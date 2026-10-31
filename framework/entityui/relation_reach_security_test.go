package entityui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// customersCRUDOff is the invoice world with the customers entity's CRUD
// routes off, the auth.UserEntityConfig posture.
func customersCRUDOff(t *testing.T) *testUI {
	t.Helper()
	ents := invoiceEntities()
	c := ents["customers"]
	off := false
	c.Exposure = &entity.ExposureConfig{CRUD: &off}
	ents["customers"] = c
	return newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
}

// pickAs posts an empty picker search for invoices.customer_id as ctx.
func pickAs(t *testing.T, x *testUI, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/_pick?field=customer_id", strings.NewReader(`{"q":""}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.ui.PickerHandler("invoices").ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

// invoicesOnlyToken is u1 holding a token scoped to invoices alone.
func invoicesOnlyToken(x *testUI) context.Context {
	ctx := x.userCtx("/api/invoices/_pick", "", "u1")
	return access.WithHeldScopes(ctx, []string{"invoices:read", "invoices:write"})
}

// SECURITY (2026-10-27 A20): the picker never lists a related entity
// whose CRUD routes are off.
func TestPickerRefusesCRUDOffTarget(t *testing.T) {
	x := customersCRUDOff(t)
	rec := pickAs(t, x, x.userCtx("/api/invoices/_pick", "", "u1"))
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("SECURITY: a CRUD-off target answered %d:\n%s", rec.Code, rec.Body)
	}
}

// SECURITY (A20): a token without <target>:read gets no picker rows.
func TestPickerRefusesTokenWithoutScope(t *testing.T) {
	x := newInvoiceUI(t)
	rec := pickAs(t, x, invoicesOnlyToken(x))
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("SECURITY: an invoices-only token answered %d:\n%s", rec.Code, rec.Body)
	}
	scoped := access.WithHeldScopes(x.userCtx("/api/invoices/_pick", "", "u1"),
		[]string{"invoices:read", "invoices:write", "customers:read"})
	if rec := pickAs(t, x, scoped); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("a token holding customers:read answered %d:\n%s", rec.Code, rec.Body)
	}
}

// SECURITY (2026-10-27 A22): a record's relation label never reads a
// related entity whose CRUD routes are off, or one the token is not
// scoped to.
func TestRelationLabelSkipsUnreachable(t *testing.T) {
	x := customersCRUDOff(t)
	if body := renderRecord(t, x, "inv-1", nil); strings.Contains(body, "Acme") {
		t.Errorf("SECURITY: a CRUD-off customer's name is on the record:\n%s", body)
	}
	y := newInvoiceUI(t)
	body := string(y.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(invoicesOnlyToken(y)))
	if strings.Contains(body, "Acme") {
		t.Errorf("SECURITY: an invoices-only token reads the customer's name:\n%s", body)
	}
	if body := renderRecord(t, y, "inv-1", nil); !strings.Contains(body, "Acme") {
		t.Errorf("a session caller lost the customer's name:\n%s", body)
	}
}

// readGated is the invoice world with invoices and customers reads
// behind a permission, and the reader context that holds both.
func readGated(t *testing.T) (*testUI, context.Context) {
	t.Helper()
	ents := invoiceEntities()
	for name, perm := range map[string]string{"invoices": "invoices:read", "customers": "customers:read"} {
		e := ents[name]
		e.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: perm}}
		ents[name] = e
	}
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", "invoices:read", "customers:read"); err != nil {
		t.Fatal(err)
	}
	return x, bulkCtx("u1", policy, "reader")
}

// SECURITY (A22): a list's relation column and its facet name no
// customer from a CRUD-off entity or to a token without customers:read.
func TestListRelationLabelSkipsUnreachable(t *testing.T) {
	faceted := func(ents map[string]entity.EntityConfig) map[string]entity.EntityConfig {
		inv := ents["invoices"]
		d := *inv.Display
		d.Facets = []string{"customer_id"}
		inv.Display = &d
		ents["invoices"] = inv
		return ents
	}
	open := newTestUI(t, faceted(invoiceEntities()), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	list := func(x *testUI) *ListBuilder { return x.ui.List("invoices") }
	if page := listHTML(t, list(open), open.userCtx("/invoices", "", "u1")); strings.Count(page, "Acme") < 2 {
		t.Fatalf("a session caller lost the customer label or facet:\n%s", page)
	}
	if page := listHTML(t, list(open), invoicesOnlyToken(open)); strings.Contains(page, "Acme") {
		t.Errorf("SECURITY: an invoices-only token reads the customer's name:\n%s", page)
	}
	ents := faceted(invoiceEntities())
	c := ents["customers"]
	off := false
	c.Exposure = &entity.ExposureConfig{CRUD: &off}
	ents["customers"] = c
	shut := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	if page := listHTML(t, list(shut), shut.userCtx("/invoices", "", "u1")); strings.Contains(page, "Acme") {
		t.Errorf("SECURITY: a CRUD-off customer's name is on the list:\n%s", page)
	}
}

// SECURITY (A23): a list's relation column never names a related row
// the Decider refuses.
func TestListRelationLabelSkipsRefusedRow(t *testing.T) {
	x, ctx := readGated(t)
	if page := listHTML(t, x.ui.List("invoices"), ctx); !strings.Contains(page, "Acme") {
		t.Fatalf("an allowed reader lost the customer label:\n%s", page)
	}
	denied := access.WithDecider(ctx, denyRow("customers:read", "cus-1"))
	if page := listHTML(t, x.ui.List("invoices"), denied); strings.Contains(page, "Acme") {
		t.Errorf("SECURITY: a refused customer's name is on the list:\n%s", page)
	}
}

// denyRow refuses read on one record of one table.
func denyRow(perm access.Permission, id string) access.Decider {
	return func(_ context.Context, _ []string, p access.Permission, r access.Ref) access.Decision {
		if p == perm && r.ID == id {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
}

// SECURITY (2026-10-27 A23): naming a record the Decider refuses does
// not export it.
func TestNamedExportSkipsRefusedRow(t *testing.T) {
	x, ctx := readGated(t)
	rec, rows := getExport(t, x, access.WithDecider(ctx, denyRow("invoices:read", "inv-1")), "_id=inv-1")
	if strings.Contains(rec.Body.String(), "INV-1") {
		t.Fatalf("SECURITY: a refused row was exported: %v", rows)
	}
	if _, rows := getExport(t, x, ctx, "_id=inv-1"); len(rows) != 2 {
		t.Fatalf("an allowed named export has %d lines, want header + 1", len(rows))
	}
}

// SECURITY (A23): a relation label never names a related row the
// Decider refuses.
func TestRelationLabelSkipsRefusedRow(t *testing.T) {
	x, ctx := readGated(t)
	rec := x.ui.Record("invoices", "inv-1").Base("/rec/invoices")
	denied := access.WithDecider(ctx, denyRow("customers:read", "cus-1"))
	if body := string(rec.RenderCtx(denied)); strings.Contains(body, "Acme") {
		t.Errorf("SECURITY: a refused customer's name is on the record:\n%s", body)
	}
	if body := string(rec.RenderCtx(ctx)); !strings.Contains(body, "Acme") {
		t.Errorf("an allowed reader lost the customer's name:\n%s", body)
	}
}
