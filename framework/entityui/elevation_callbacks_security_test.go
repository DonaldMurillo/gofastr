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
		Key: "peek", Label: testPhrase("Peek"), Bulk: true,
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

// buildFunc is the Build-callback body the record-screen elevation pins
// share, made seam-agnostic: record that the build ran, try the gated
// record title with the context it was handed, and return the peek
// component that draws the same way.
type buildFunc func(ui *UI, ctx context.Context) (component.Component, error)

func buildPeek(ran, titled, drawn, drawTitled *bool) buildFunc {
	return func(ui *UI, ctx context.Context) (component.Component, error) {
		*ran = true
		_, *titled = ui.RecordTitle(ctx, "invoices", "inv-1")
		return peek{ui, drawn, drawTitled}, nil
	}
}

// tabPeek mounts the peek build as a record tab.
func tabPeek(build buildFunc) Extensions {
	return Extensions{Entities: map[string]Extension{"invoices": {Tabs: []Tab{{
		Key: "peek", Label: testPhrase("Peek"),
		Build: func(tc TabContext) (component.Component, error) { return build(tc.UI, tc.Ctx) },
	}}}}}
}

// sidePeek mounts the peek build as a record side panel.
func sidePeek(build buildFunc) Extensions {
	return Extensions{Entities: map[string]Extension{"invoices": {Side: []SidePanel{{
		Key: "peek", Title: testPhrase("Peek"),
		Build: func(rc RecordContext) (component.Component, error) { return build(rc.UI, rc.Ctx) },
	}}}}}
}

// assertBuildRunsAsCaller pins that a record-screen Build callback runs on
// the caller's context and so does the render of the component it
// returns: mount registers the peek build at its seam, query is what the
// record route needs to draw it, and what names the seam in the failure
// messages.
func assertBuildRunsAsCaller(t *testing.T, what, query string, mount func(buildFunc) Extensions) {
	t.Helper()
	titled, ran := true, false
	var drawn, drawTitled bool
	x, policy := readGatedInvoices(t, mount(buildPeek(&ran, &titled, &drawn, &drawTitled)))
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(elevatedClerk(x, policy, "/rec/invoices/inv-1", query))
	if !ran || !drawn {
		t.Fatalf("setup: the elevated record never drew the %s", what)
	}
	if titled {
		t.Fatalf("SECURITY: a %s read a record title its caller's roles cannot read", what)
	}
	if drawTitled {
		t.Fatalf("SECURITY: a %s's component drew with the back office's elevation", what)
	}
}

// A record tab's Build runs as the caller too, and so does the render of
// the component it returns.
func TestTabBuildIsNotElevated(t *testing.T) {
	assertBuildRunsAsCaller(t, "tab", "?tab=peek", tabPeek)
}

// A record side panel's Build runs as the caller, and so does the render
// of the component it returns.
func TestSidePanelBuildIsNotElevated(t *testing.T) {
	assertBuildRunsAsCaller(t, "side panel", "", sidePeek)
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
