package entityui

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// readGatedInvoices is the invoices fixture behind an Access read and
// update permission, with a clerk role that holds neither: only the
// back office's elevation lets the clerk see a row.
func readGatedInvoices(t *testing.T, ext Extensions) (*testUI, *access.RolePolicy) {
	t.Helper()
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read", Update: "invoices:update"}}
	ents["invoices"] = inv
	x := newTestUIExt(t, ents, invoiceRows(), ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	policy.Register("invoices:read", "invoices:update")
	return x, policy
}

// The admin elevates its own reads and writes; the code an app hangs on
// the screens runs as the caller. An action's Run handed an elevated
// context would pass every read gate the caller's roles fail.
func TestActionRunIsNotElevated(t *testing.T) {
	readable, ran := true, false
	x, policy := readGatedInvoices(t, Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "peek", Label: "Peek", Bulk: true,
		Run: func(ctx context.Context, ac ActionContext) error {
			ran = true
			readable = ac.Crud.CanReadRecordScoped(ctx, ac.IDs[0])
			return nil
		},
	}}}}})
	ctx := crud.WithElevation(bulkCtx("u1", policy, "clerk"))
	if code, out := postBulk(t, x, ctx, map[string]any{"action": "run:peek", "scope": "selected", "ids": []string{"inv-1"}}); code != http.StatusOK {
		t.Fatalf("bulk = %d %v", code, out)
	}
	if !ran {
		t.Fatal("setup: the elevated selection never reached Run")
	}
	if readable {
		t.Fatal("SECURITY: Run may read a row its caller's roles cannot read")
	}
}

// A record tab's Build runs as the caller too.
func TestTabBuildIsNotElevated(t *testing.T) {
	titled, ran := true, false
	x, policy := readGatedInvoices(t, Extensions{Entities: map[string]Extension{"invoices": {Tabs: []Tab{{
		Key: "peek", Label: "Peek",
		Build: func(tc TabContext) (component.Component, error) {
			ran = true
			_, titled = tc.UI.RecordTitle(tc.Ctx, "invoices", "inv-1")
			return tc.UI.List("customers").NoLinks(), nil
		},
	}}}}})
	ctx := x.userCtx("/rec/invoices/inv-1", "?tab=peek", "u1")
	ctx = crud.WithElevation(access.WithRoles(access.WithPolicy(ctx, policy), []string{"clerk"}))
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(ctx)
	if !ran {
		t.Fatal("setup: the elevated record never drew the tab")
	}
	if titled {
		t.Fatal("SECURITY: a tab read a record title its caller's roles cannot read")
	}
}
