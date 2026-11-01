package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
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

// An Endpoint answers to its entity's scope, "<table>:<verb>", the one its
// MCP twin checks. Registered on the bare router, a relative endpoint on a
// table named v1 (/v1/refresh) carried no resource, so RequireAPIScopes
// fell back to the path and read the version-shaped v1 as a version,
// asking for refresh:write.
func TestEndpointsNameTheirTable(t *testing.T) {
	eps := []entity.Endpoint{{Method: "POST", Path: "refresh", Name: "refresh", Handler: http.HandlerFunc(noContent)}}
	fields := []schema.Field{{Name: "name", Type: schema.String}}
	plain := NewApp()
	plain.Entity("v1", entity.EntityConfig{Fields: fields, Endpoints: eps})
	grouped := NewApp()
	grouped.GroupEntity(grouped.Group("/v2"), "v3", entity.EntityConfig{Fields: fields, Endpoints: eps})

	for path, app := range map[string]*App{"/v1/refresh": plain, "/v2/v3/refresh": grouped} {
		var got string
		app.Use(func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, _ = router.RouteResource(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
		})
		app.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
		want := strings.Split(path, "/")[len(strings.Split(path, "/"))-2]
		if got != want {
			t.Errorf("POST %s: route resource %q, want %q", path, got, want)
		}
	}
}
