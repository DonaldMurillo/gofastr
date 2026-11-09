package entityui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// The owner and tenant columns are the server's: crud stamps them from
// the caller and never writes them from a body, so no form draws an
// input for one, on create or edit, even when the field is declared
// visible.
func TestScopeColumnsNotEditable(t *testing.T) {
	installOwnerExtractor(t)
	cases := []struct {
		name, col string
		scope     entity.ScopeConfig
	}{
		{"owner", "owner_id", entity.ScopeConfig{OwnerField: "owner_id"}},
		{"tenant", "tenant_id", entity.ScopeConfig{MultiTenant: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ents := invoiceEntities()
			inv := ents["invoices"]
			inv.Fields = append(inv.Fields, schema.Field{Name: c.col, Type: schema.String})
			inv.Scope = &c.scope
			ents["invoices"] = inv
			rows := invoiceRows()
			rows["invoices"][0][c.col] = "u1"
			x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
			ctx := tenant.SetTenantID(bulkCtx("u1", nil), "u1")
			create := string(x.ui.Create("invoices").Base("/rec/invoices").
				RenderCtx(app.WithRequest(ctx, httptest.NewRequest(http.MethodGet, "/rec/invoices/create", nil))))
			edit := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
				RenderCtx(app.WithRequest(ctx, httptest.NewRequest(http.MethodGet, "/rec/invoices/inv-1", nil))))
			for name, body := range map[string]string{"create": create, "edit": edit} {
				if !strings.Contains(body, `name="number"`) {
					t.Fatalf("%s: the form drew no number input, so the check below proves nothing:\n%s", name, body)
				}
				if strings.Contains(body, `name="`+c.col+`"`) {
					t.Errorf("%s: the form drew an input for the %s column:\n%s", name, c.col, body)
				}
			}
		})
	}
}
