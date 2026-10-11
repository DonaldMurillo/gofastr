package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// With InlineEdit a list's plain cells (an enum, a number, a short
// string) are edited in place: a popup form that PUTs the one field to
// the record's write route and returns to the list. The title link,
// long text, relations and NoQuery fields stay as they are, and a
// caller who may not update draws no editor.
func TestListInlineEdit(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "po", Type: schema.String})
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"][0]["po"] = "PO-7"
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	b := x.ui.List("invoices").InlineEdit()
	h := listHTML(t, b.Columns("number", "status", "po", "issued_on", "customer_id", "token"), x.userCtx("/invoices", "?sort=number", "u1"))
	for _, want := range []string{
		`data-cui-comp="ui-inline-edit"`,
		`data-cui-rpc="/api/invoices/inv-1"`,
		`data-cui-rpc-method="PUT"`,
		`data-cui-rpc-navigate="/invoices?sort=number"`,
		`name="po"`,
		`value="PO-7"`,
		// The cell names the field, so its editor's label is hidden from
		// view, and the line under the rows says the values edit in place.
		`fui-field--label-hidden`,
		"Select a value to edit it in place.",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("inline edit misses %q:\n%s", want, h)
		}
	}
	// The status moves only by its transitions, the issue date is its
	// stamp, and the number is the link.
	for _, bad := range []string{`name="number"`, `name="status"`, `name="issued_on"`, `name="customer_id"`, `name="token"`} {
		if strings.Contains(h, bad) {
			t.Errorf("inline edit offered %s", bad)
		}
	}
	// A reader who may list invoices but not update them gets none.
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read", Update: "invoices:write"}}
	ents["invoices"] = inv
	y := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("invoices:read")); err != nil {
		t.Fatal(err)
	}
	reader := access.WithRoles(access.WithPolicy(y.userCtx("/invoices", "", "u1"), policy), []string{"reader"})
	ro := listHTML(t, y.ui.List("invoices").Columns("number", "po").InlineEdit(), reader)
	if !strings.Contains(ro, "PO-7") {
		t.Fatalf("the reader cannot see the list:\n%s", ro)
	}
	if strings.Contains(ro, `ui-inline-edit`) {
		t.Errorf("SECURITY: a caller who may not update got an inline editor")
	}
	if strings.Contains(ro, "edit it in place") {
		t.Errorf("the hint shows on a list with nothing to edit")
	}
	if plain := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1")); strings.Contains(plain, "ui-inline-edit") {
		t.Errorf("a list without InlineEdit drew editors")
	}
}

// SECURITY: a value a read hook masks is never an inline editor's
// prefill: saving it would write the mask over the stored value.
func TestListInlineEditSkipsMaskedValues(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "secret", Type: schema.String}, schema.Field{Name: "po", Type: schema.String})
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"][0]["secret"] = "real-value"
	rows["invoices"][0]["po"] = "PO-7"
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.ListPayload); ok {
			for _, r := range p.Results {
				r["secret"] = "****"
			}
		}
		return nil
	})
	h := listHTML(t, x.ui.List("invoices").Columns("number", "secret", "po").InlineEdit(), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(h, `name="secret"`) {
		t.Fatalf("SECURITY: a masked value became an inline editor:\n%s", h)
	}
	if !strings.Contains(h, `name="po"`) {
		t.Errorf("the unmasked field lost its editor:\n%s", h)
	}
}
