package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the read-scope disclosure on /openapi.json and
// /api/docs/openapi.json, decided Q7 of the 2026-09-05 adversarial pass
// (round 4): the spec inherits the SAME per-request read-scope filter
// /api/llm.md runs. Fixed by Spec.RequestView (core/openapi): the
// framework's entity spec attaches a per-request view that rebuilds the
// document keeping only entities crud's CanReadScoped admits for the
// caller; coreoa.Handler (the auth-gated default, and DocsHandler's
// gated nested spec route) serves that view. WithPublicOpenAPI stays
// the full-disclosure opt-in (PublicHandler ignores RequestView).
// Family: F17 Authorization at derived surfaces
// Property: a derived documentation surface (the OpenAPI spec) must not
// disclose an entity's schema to an authenticated caller who holds no
// read grant for it, the same per-request filter /api/llm.md runs.
// Surfaces: framework/app.go's /openapi.json mount (coreoa.Handler over
// the single EntityOpenAPI spec) and core/openapi DocsHandler's nested
// spec route at /api/docs/openapi.json.
// Threat: an authenticated caller with no `orders:read` grant used to
// get 200 with the complete orders schema (component, paths, filter
// params) from /openapi.json, while List answers 403 and /api/llm.md
// hides the entity.
func TestOpenAPISpecHidesUngrantedEntities(t *testing.T) {
	app := NewApp(WithDB(bannerTestDB(t)))
	app.Entity("orders", entity.EntityConfig{
		Table: "orders",
		Fields: []schema.Field{
			{Name: "total", Type: schema.Int},
			{Name: "internal_note", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "orders:read"}},
	}.WithTimestamps(false))
	app.Entity("products", entity.EntityConfig{
		Table:  "products",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	app, cleanup := startApp(t, app)
	defer cleanup()

	for _, path := range []string{"/openapi.json", "/api/docs/openapi.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		// Signed in, holds roles but no orders:read grant — the exact
		// caller shape crud's TestLLMMDIndexHidesUngrantedEntities uses.
		ctx := handler.SetUser(req.Context(), struct{ ID string }{ID: "u1"})
		ctx = access.WithPolicy(ctx, access.NewRolePolicy())
		ctx = access.WithRoles(ctx, []string{"staff"})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: authenticated caller got %d, want 200 (body %s)", path, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if !strings.Contains(body, "products") {
			t.Errorf("%s: spec hid an entity the caller CAN read (products)", path)
		}
		if strings.Contains(body, "orders") || strings.Contains(body, "internal_note") {
			t.Errorf("SECURITY: [openapi-scope] %s: spec disclosed the orders entity (name/paths/columns) to a caller with no orders:read grant; /api/llm.md hides it for the same caller", path)
		}
	}
}
