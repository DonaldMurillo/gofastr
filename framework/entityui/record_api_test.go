package entityui

import (
	"context"
	"html"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The API tab is off unless the builder turns it on: the tab strip names
// no api tab, and ?tab=api falls back to Edit.
func TestAPITabOffByDefault(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, "?tab=api") {
		t.Fatalf("a record without API() drew an api tab link:\n%s", body)
	}
	tabbed := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	// ?tab=api with the tab off falls back to Edit: the record's own
	// fields show, but never a JSON body.
	if strings.Contains(tabbed, "&#34;id&#34;:") {
		t.Fatalf("?tab=api with the tab off drew a JSON body:\n%s", tabbed)
	}
}

// The tab draws the record exactly as the REST GET under the caller's
// context returns it: same fields, same values, pretty-printed with
// deterministic key order. A NoQuery column stays (it is readable); an
// AfterGet mask shows the mask, never the stored value.
func TestAPITabShowsRecordAsTheGETReturnsIt(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	flat := html.UnescapeString(body)
	for _, want := range []string{
		`"id": "inv-1"`,
		`"number": "INV-1"`,
		`"token": "tok-secret"`,
		`"status": "draft"`,
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("the API tab's JSON is missing %s:\n%s", want, flat)
		}
	}
	if i, j := strings.Index(flat, `"amount"`), strings.Index(flat, `"customer_id"`); i < 0 || j < 0 || i > j {
		t.Errorf("JSON keys are not in sorted order (amount at %d, customer_id at %d):\n%s", i, j, flat)
	}

	// A hook's mask is what the GET returns; the stored value never shows.
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["memo"] = "MASKED"
		}
		return nil
	})
	masked := html.UnescapeString(string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1"))))
	if !strings.Contains(masked, `"memo": "MASKED"`) {
		t.Errorf("the API tab's JSON must show the hooked (masked) value:\n%s", masked)
	}
	if strings.Contains(masked, "first note") {
		t.Errorf("SECURITY: the API tab's JSON leaked the stored value a hook masks:\n%s", masked)
	}
}

// A Hidden column never reaches the JSON, the same way the REST GET
// never returns it.
func TestAPITabHidesHiddenColumns(t *testing.T) {
	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "user_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "user_id"}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"][0]["user_id"] = "u1"
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := html.UnescapeString(string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1"))))
	if strings.Contains(body, `"user_id"`) || strings.Contains(body, `"u1"`) {
		t.Fatalf("SECURITY: a Hidden column reached the API tab's JSON:\n%s", body)
	}
	if !strings.Contains(body, `"number": "INV-1"`) {
		t.Fatalf("the visible columns are missing:\n%s", body)
	}
}

// The REST section names the entity's own REST base and the methods the
// exposure allows, never the write base a back office swapped in with
// WithAPIPath.
func TestAPITabNamesRestBaseAndMethods(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	for _, want := range []string{"/api/invoices", "GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !strings.Contains(body, want) {
			t.Errorf("the API tab's REST section is missing %q:\n%s", want, body)
		}
	}

	admin := x.ui.WithAPIPath(func(e *entity.Entity) (string, bool) {
		return "/wr/" + e.GetName(), true
	})
	derived := string(admin.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	if !strings.Contains(derived, "/api/invoices") {
		t.Errorf("the API tab must name the entity's own REST base, not the swapped write base:\n%s", derived)
	}
}

// An entity with no REST routes says so instead of naming a path that
// answers 404.
func TestAPITabRestOffNotice(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("customers", "cus-1").Base("/rec/customers").API().
		RenderCtx(x.userCtx("/rec/customers/cus-1", "?tab=api", "u1")))
	if !strings.Contains(body, "This entity is not exposed over REST.") {
		t.Fatalf("an entity without REST routes must say so:\n%s", body)
	}
	if strings.Contains(body, "/api/customers") {
		t.Fatalf("the API tab named a REST path for an entity with none:\n%s", body)
	}
}

// The MCP section lists the tool names crud registers while
// Exposure.MCP is on, moves included, and says so while it is off.
func TestAPITabNamesMCPTools(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{MCP: true}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	for _, want := range []string{
		"invoices_list", "invoices_get", "invoices_create", "invoices_update", "invoices_delete",
		"invoices_send", "invoices_mark_paid",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the API tab's MCP section is missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "invoices_sweep") {
		t.Errorf("a System move has no MCP tool; the tab must not name one:\n%s", body)
	}

	off := newInvoiceUI(t)
	offBody := string(off.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(off.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	if !strings.Contains(offBody, "This entity is not exposed over MCP.") {
		t.Fatalf("an entity without Exposure.MCP must say so:\n%s", offBody)
	}
	if strings.Contains(offBody, "invoices_list") {
		t.Fatalf("the API tab named MCP tools for an entity MCP does not expose:\n%s", offBody)
	}
}

// The tab links the app's entity index at /api/llm.md.
func TestAPITabLinksLLMIndex(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	if !strings.Contains(body, `href="/api/llm.md"`) {
		t.Fatalf("the API tab does not link /api/llm.md:\n%s", body)
	}
}

// The tab is behind the record's own read gate: an anonymous caller
// gets the refusal, never the JSON body.
func TestAPITabRefusesAnonymous(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.ctx("/rec/invoices/inv-1", "?tab=api")))
	if !strings.Contains(body, AccessDeniedTitle) {
		t.Fatalf("an anonymous caller must see the refusal:\n%s", body)
	}
	if strings.Contains(body, "tok-secret") || strings.Contains(body, "INV-1") {
		t.Fatalf("SECURITY: the refused record leaked into the API tab:\n%s", body)
	}
}

// The tab strip link keeps the record's own path and query shape.
func TestAPITabLinkShape(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").API().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=api", "u1")))
	if !strings.Contains(body, `href="/rec/invoices/inv-1?tab=api"`) {
		t.Fatalf("the tab strip does not link ?tab=api on the record path:\n%s", body)
	}
}

var _ = crud.CaseSnake
