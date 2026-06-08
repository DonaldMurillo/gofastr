package framework

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAudit_StampsTenantID asserts that a write performed under a tenant
// context records that tenant_id on the audit row, so multi-tenant apps
// don't mix every tenant's audit trail in one undifferentiated table.
func TestAudit_StampsTenantID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		// Stamp a fixed tenant into the request context so the audit hook
		// (running inside that ctx) can read it.
		app.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(SetTenantID(r.Context(), "acme")))
			})
		})
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{
			Actor: func(_ context.Context) string { return "alice" },
		})

		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)

		var tenantID sql.NullString
		err := db.QueryRow(`SELECT tenant_id FROM audit_log ORDER BY created_at, id LIMIT 1`).Scan(&tenantID)
		if err != nil {
			t.Fatalf("query tenant_id: %v", err)
		}
		if !tenantID.Valid || tenantID.String != "acme" {
			t.Fatalf("tenant_id: got %v (valid=%v), want acme", tenantID.String, tenantID.Valid)
		}
	})
}
