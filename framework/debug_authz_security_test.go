package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type debugRoleUser struct{ roles []string }

func (u debugRoleUser) GetRoles() []string { return u.roles }

func debugStatus(app *App, path string, ctx context.Context) int {
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	return rec.Code
}

// /.debug/stats and /.debug/goroutineleak carry the pid, memory layout and
// every leaked goroutine's stack. Any signed-in user, a self-registered
// account included, used to get them. They now need the admin role.
func TestDebugEndpointsNeedAdminRole(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()
	bg := context.Background()
	member := handler.SetUser(bg, debugRoleUser{roles: []string{"member"}})
	admin := handler.SetUser(bg, debugRoleUser{roles: []string{"admin"}})
	viaAccess := access.WithRoles(handler.SetUser(bg, "u-9"), []string{"admin"})
	embedded := fembed.WithGrant(admin, fembed.Grant{})

	for _, p := range []string{"/.debug/stats", "/.debug/goroutineleak"} {
		if got := debugStatus(app, p, bg); got != http.StatusUnauthorized {
			t.Errorf("%s anonymous = %d, want 401", p, got)
		}
		if got := debugStatus(app, p, handler.SetUser(bg, nil)); got != http.StatusUnauthorized {
			t.Errorf("%s nil user = %d, want 401", p, got)
		}
		if got := debugStatus(app, p, member); got != http.StatusForbidden {
			t.Errorf("%s signed-in non-admin = %d, want 403", p, got)
		}
		if got := debugStatus(app, p, embedded); got != http.StatusForbidden {
			t.Errorf("%s admin through an embed grant = %d, want 403", p, got)
		}
		for name, ctx := range map[string]context.Context{"GetRoles": admin, "access roles": viaAccess} {
			if got := debugStatus(app, p, ctx); got == http.StatusUnauthorized || got == http.StatusForbidden {
				t.Errorf("%s admin via %s = %d, want served", p, name, got)
			}
		}
	}
}

// WithDebugAuthorize replaces the role check.
func TestDebugAuthorizeOverridesRole(t *testing.T) {
	app := NewApp(WithDebugAuthorize(func(ctx context.Context) bool {
		u, _ := handler.GetUser(ctx)
		return u == "ops"
	}))
	app.registerDebugEndpoints()
	bg := context.Background()
	if got := debugStatus(app, "/.debug/stats", handler.SetUser(bg, "ops")); got != http.StatusOK {
		t.Errorf("custom-authorized caller = %d, want 200", got)
	}
	if got := debugStatus(app, "/.debug/stats", handler.SetUser(bg, debugRoleUser{roles: []string{"admin"}})); got != http.StatusForbidden {
		t.Errorf("admin refused by the custom predicate = %d, want 403", got)
	}
}
