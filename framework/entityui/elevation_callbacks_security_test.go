package entityui

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
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
	ctx := crud.WithElevation(bulkCtx("u1", policy, "clerk"), "invoices")
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

// peek is an extension component that reads inv-1's title with the
// context it is drawn with, the way a returned tc.UI.List would.
type peek struct {
	ui     *UI
	drawn  *bool
	titled *bool
}

func (p peek) Render() render.HTML { return "" }

func (p peek) RenderCtx(ctx context.Context) render.HTML {
	*p.drawn = true
	_, *p.titled = p.ui.RecordTitle(ctx, "invoices", "inv-1")
	return ""
}

// elevatedClerk is a clerk's context at path, elevated for invoices.
func elevatedClerk(x *testUI, policy *access.RolePolicy, path, query string) context.Context {
	ctx := x.userCtx(path, query, "u1")
	return crud.WithElevation(access.WithRoles(access.WithPolicy(ctx, policy), []string{"clerk"}), "invoices")
}

// A record tab's Build runs as the caller too, and so does the render of
// the component it returns.
func TestTabBuildIsNotElevated(t *testing.T) {
	titled, ran := true, false
	var drawn, drawTitled bool
	x, policy := readGatedInvoices(t, Extensions{Entities: map[string]Extension{"invoices": {Tabs: []Tab{{
		Key: "peek", Label: "Peek",
		Build: func(tc TabContext) (component.Component, error) {
			ran = true
			_, titled = tc.UI.RecordTitle(tc.Ctx, "invoices", "inv-1")
			return peek{tc.UI, &drawn, &drawTitled}, nil
		},
	}}}}})
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(elevatedClerk(x, policy, "/rec/invoices/inv-1", "?tab=peek"))
	if !ran || !drawn {
		t.Fatal("setup: the elevated record never drew the tab")
	}
	if titled {
		t.Fatal("SECURITY: a tab read a record title its caller's roles cannot read")
	}
	if drawTitled {
		t.Fatal("SECURITY: a tab's component drew with the back office's elevation")
	}
}

// A replaced record body and a replaced list body draw as the caller.
func TestReplacedBodiesAreNotElevated(t *testing.T) {
	var recDrawn, recTitled, listDrawn, listTitled bool
	x, policy := readGatedInvoices(t, Extensions{Entities: map[string]Extension{"invoices": {
		Record: func(rc RecordContext) (component.Component, error) {
			return peek{rc.UI, &recDrawn, &recTitled}, nil
		},
		List: func(lc ListContext) (component.Component, error) {
			return peek{lc.UI, &listDrawn, &listTitled}, nil
		},
	}}})
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(elevatedClerk(x, policy, "/rec/invoices/inv-1", ""))
	x.ui.List("invoices").RenderCtx(elevatedClerk(x, policy, "/rec/invoices", ""))
	if !recDrawn || !listDrawn {
		t.Fatalf("setup: record drawn %v, list drawn %v", recDrawn, listDrawn)
	}
	if recTitled || listTitled {
		t.Fatalf("SECURITY: a replaced body drew with the back office's elevation (record %v, list %v)", recTitled, listTitled)
	}
}
