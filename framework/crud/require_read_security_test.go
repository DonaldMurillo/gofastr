package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// RequireRead is the gate entityui's export, bulk, picker, trash and
// saved-view routes share with GET /api/<entity>. Each refusal it runs
// (owner, tenant, the baseline session, the read permission) answers the
// list route's status and body for the same request, and a caller every
// gate admits passes with nothing written. Each case is built so that one
// gate alone refuses it: a removed gate lets its case through.
func TestRequireReadRefusesAsListRoute(t *testing.T) {
	reader := access.NewRolePolicy()
	reader.Grant("member", "things:read")
	cases := []struct {
		name   string
		cfg    entity.EntityConfig
		ctx    context.Context
		status int
		reason string
	}{
		{"missing owner", entity.EntityConfig{Scope: &entity.ScopeConfig{OwnerField: "owner_id"}},
			context.Background(), http.StatusUnauthorized, ""},
		// Public, so the session gate cannot answer first.
		{"missing tenant", entity.EntityConfig{Scope: &entity.ScopeConfig{MultiTenant: true}, Exposure: &entity.ExposureConfig{Public: true}},
			context.Background(), http.StatusUnauthorized, "tenant"},
		{"missing session", entity.EntityConfig{},
			context.Background(), http.StatusUnauthorized, ""},
		// A Read permission, so the session gate stands aside and only RBAC refuses.
		{"missing permission", entity.EntityConfig{Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "things:read"}}},
			context.Background(), http.StatusForbidden, "things:read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ch := thingsHandler(t, c.cfg)
			req := httptest.NewRequest(http.MethodGet, "/api/things", nil).WithContext(c.ctx)
			gate := httptest.NewRecorder()
			if ch.RequireRead(gate, req) {
				t.Fatal("RequireRead admitted a caller the list route refuses")
			}
			if gate.Code != c.status {
				t.Errorf("RequireRead status = %d, want %d", gate.Code, c.status)
			}
			if !strings.Contains(gate.Body.String(), c.reason) {
				t.Errorf("RequireRead body %s does not name %q", gate.Body.String(), c.reason)
			}
			list := httptest.NewRecorder()
			ch.List()(list, req)
			if list.Code != gate.Code || list.Body.String() != gate.Body.String() {
				t.Errorf("RequireRead answered %d %s; the list route answered %d %s",
					gate.Code, gate.Body.String(), list.Code, list.Body.String())
			}
		})
	}
	t.Run("admitted caller", func(t *testing.T) {
		ch := thingsHandler(t, entity.EntityConfig{
			Scope:    &entity.ScopeConfig{MultiTenant: true},
			Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "things:read"}},
		})
		ctx := access.WithRoles(access.WithPolicy(tenant.SetTenantID(context.Background(), "acme"), reader), []string{"member"})
		rec := httptest.NewRecorder()
		if !ch.RequireRead(rec, httptest.NewRequest(http.MethodGet, "/api/things", nil).WithContext(ctx)) {
			t.Fatalf("RequireRead refused a caller every gate admits: %d %s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Errorf("an admitted caller got a body: %s", rec.Body.String())
		}
	})
}

// thingsHandler is a CrudHandler over an in-memory "things" table with
// owner_id, tenant_id and title columns, for posture tests that vary only
// the entity config.
func thingsHandler(t *testing.T, cfg entity.EntityConfig) *CrudHandler {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE things (id TEXT PRIMARY KEY, owner_id TEXT, tenant_id TEXT, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "title", Type: schema.String})
	ent := entity.Define("things", cfg.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db)
}
