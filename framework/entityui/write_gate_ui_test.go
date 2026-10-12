package entityui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// pageCtx is a request context at path for u's roles under policy.
func pageCtx(path string, policy *access.RolePolicy, u string, roles ...string) context.Context {
	return app.WithRequest(bulkCtx(u, policy, roles...), httptest.NewRequest(http.MethodGet, path, nil))
}

// A builder never draws a write the API would refuse the caller: New,
// Duplicate, Delete, the moves and the edit form follow the entity's
// create, update and delete access, and a caller holding them sees each.
func TestWriteControlsFollowWriteAccess(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{
		Create: "invoices:create", Update: "invoices:update", Delete: "invoices:delete",
	}, 0, Extensions{}, nil)
	grant(t, policy, "writer", "invoices:create", "invoices:update", "invoices:delete")
	policy.Register("viewer")

	draw := func(roles ...string) (list, rec, create string) {
		list = listHTML(t, x.ui.List("invoices").Duplicate().Delete(), pageCtx("/invoices", policy, "u1", roles...))
		rec = string(x.ui.Record("invoices", "inv-1").Duplicate().Delete().RenderCtx(pageCtx("/invoices/inv-1", policy, "u1", roles...)))
		create = string(x.ui.Create("invoices").RenderCtx(pageCtx("/invoices/create", policy, "u1", roles...)))
		return
	}
	writes := map[string]func(list, rec, create string) bool{
		"New":           func(l, _, _ string) bool { return strings.Contains(l, "New Invoice") },
		"row Duplicate": func(l, _, _ string) bool { return strings.Contains(l, "duplicate=inv-1") },
		"row Delete":    func(l, _, _ string) bool { return strings.Contains(l, `data-cui-rpc-method="DELETE"`) },
		"Duplicate":     func(_, r, _ string) bool { return strings.Contains(r, "duplicate=inv-1") },
		"Delete":        func(_, r, _ string) bool { return strings.Contains(r, `data-cui-rpc-method="DELETE"`) },
		"move":          func(_, r, _ string) bool { return strings.Contains(r, "/transitions/send") },
		"edit form":     func(_, r, _ string) bool { return strings.Contains(r, "<form") },
		"create form":   func(_, _, c string) bool { return strings.Contains(c, "<form") },
	}
	l, r, c := draw()
	for name, drawn := range writes {
		if drawn(l, r, c) {
			t.Errorf("a caller with no write access sees %s", name)
		}
	}
	if !strings.Contains(c, AccessDeniedTitle) {
		t.Errorf("the create screen does not say it is not available:\n%s", c)
	}
	l, r, c = draw("writer")
	for name, drawn := range writes {
		if !drawn(l, r, c) {
			t.Errorf("a writer does not see %s", name)
		}
	}
}
