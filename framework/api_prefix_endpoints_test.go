package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// WithAPIPrefix was applied to an entity's generated CRUD routes but not to
// its EntityConfig.Endpoints, so an app using both had its API silently split
// across two prefixes: /api/licenses for CRUD, /licenses/{id}/revoke for the
// custom endpoint. Endpoint.Path is documented as "relative to the entity
// table path", and under a prefix that table path is prefixed.
//
// Nothing reported the split — the route registered fine, just somewhere the
// author had no reason to look.
func TestAPIPrefixAppliesToRelativeEntityEndpoints(t *testing.T) {
	app := newPrefixEndpointApp(t, "/api")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest("POST", "/api/licenses/abc/revoke", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /api/licenses/abc/revoke = %d, want %d (relative endpoint did not inherit the API prefix)", rec.Code, http.StatusNoContent)
	}

	// The unprefixed path must NOT also answer — a route at both places is
	// the same split bug wearing a friendlier face.
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest("POST", "/licenses/abc/revoke", nil))
	if rec.Code == http.StatusNoContent {
		t.Errorf("POST /licenses/abc/revoke still answers; the endpoint should live only under the API prefix")
	}
}

// An absolute Endpoint.Path is the documented escape hatch for mounting
// outside the entity namespace. It must keep bypassing the prefix.
func TestAPIPrefixSkipsAbsoluteEntityEndpoints(t *testing.T) {
	app := newPrefixEndpointApp(t, "/api")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest("GET", "/health/licenses", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("GET /health/licenses = %d, want %d (absolute endpoint path must bypass the prefix)", rec.Code, http.StatusNoContent)
	}
}

// With no prefix configured, relative endpoints mount exactly where they
// always did.
func TestNoAPIPrefixLeavesEntityEndpointsAtRoot(t *testing.T) {
	app := newPrefixEndpointApp(t, "")

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest("POST", "/licenses/abc/revoke", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST /licenses/abc/revoke = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func newPrefixEndpointApp(t *testing.T, prefix string) *App {
	t.Helper()
	opts := []AppOption{}
	if prefix != "" {
		opts = append(opts, WithAPIPrefix(prefix))
	}
	app := NewApp(opts...)
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }
	app.Entity("licenses", entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "{id}/revoke", Name: "licenses_revoke", Handler: http.HandlerFunc(ok)},
			{Method: "GET", Path: "/health/licenses", Name: "licenses_health", Handler: http.HandlerFunc(ok)},
		},
	})
	return app
}
