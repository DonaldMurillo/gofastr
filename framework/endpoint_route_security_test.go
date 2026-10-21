package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

// An Endpoint's HTTP route carries the gates the entity's CRUD routes
// apply: an identity unless the endpoint is Public, and no forgeable
// cross-site write. Before, the route served whatever reached it, so an
// endpoint on an entity whose CRUD answered 401 was open to anyone.
func TestEndpointRouteGates(t *testing.T) {
	eps := []entity.Endpoint{
		{Method: "POST", Path: "{id}/revoke", Name: "revoke", Handler: http.HandlerFunc(noContent)},
		{Method: "GET", Path: "stats", Name: "stats", Handler: http.HandlerFunc(noContent)},
		{Method: "POST", Path: "{id}/ping", Name: "ping", Public: true, Handler: http.HandlerFunc(noContent)},
	}
	plain := NewApp()
	plain.Entity("licenses", entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}, Endpoints: eps})
	grouped := NewApp()
	grouped.GroupEntity(grouped.Group("/v1", routegroup.WithMCPNamespace("v1")), "licenses", entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}, Endpoints: eps})

	for name, c := range map[string]struct {
		method, path, ctype, site string
		user                      bool
		want                      int
	}{
		"anonymous":           {"POST", "/licenses/a/revoke", "application/json", "", false, 401},
		"anonymous read":      {"GET", "/licenses/stats", "", "", false, 401},
		"signed in":           {"POST", "/licenses/a/revoke", "application/json", "", true, 204},
		"public":              {"POST", "/licenses/a/ping", "application/json", "", false, 204},
		"cross-site form":     {"POST", "/licenses/a/revoke", "text/plain", "cross-site", true, 403},
		"cross-site bodyless": {"POST", "/licenses/a/ping", "", "cross-site", false, 403},
		"cross-site json":     {"POST", "/licenses/a/revoke", "application/json", "cross-site", true, 204},
		"cross-site read":     {"GET", "/licenses/stats", "", "cross-site", true, 204},
		"same-origin form":    {"POST", "/licenses/a/revoke", "text/plain", "same-origin", true, 204},
	} {
		for appName, app := range map[string]*App{"plain": plain, "grouped": grouped} {
			path := c.path
			if appName == "grouped" {
				path = "/v1" + path
			}
			req := httptest.NewRequest(c.method, path, strings.NewReader("{}"))
			if c.ctype != "" {
				req.Header.Set("Content-Type", c.ctype)
			}
			if c.site != "" {
				req.Header.Set("Sec-Fetch-Site", c.site)
			}
			if c.user {
				req = req.WithContext(handler.SetUser(req.Context(), "u1"))
			}
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != c.want {
				t.Errorf("%s %s: %d, want %d: %s", appName, name, rec.Code, c.want, rec.Body)
			}
		}
	}

	// A gated response is per-caller, so no shared cache may store it.
	req := httptest.NewRequest("GET", "/licenses/stats", nil)
	rec := httptest.NewRecorder()
	plain.Router().ServeHTTP(rec, req.WithContext(handler.SetUser(req.Context(), "u1")))
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("gated endpoint Cache-Control = %q, want no-store", got)
	}
}
