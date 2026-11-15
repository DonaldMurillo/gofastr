package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Every record route answers Cache-Control: no-store, the refusals of an
// anonymous caller included: the header is set before the scope check.
func TestEveryRecordRouteIsNoStore(t *testing.T) {
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT);`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	t.Cleanup(func() { db.Close() })
	routes := []struct {
		name    string
		method  string
		body    string
		handler http.HandlerFunc
	}{
		{"list", http.MethodGet, "", ch.List()},
		{"get", http.MethodGet, "", ch.Get()},
		{"create", http.MethodPost, `{"title":"x"}`, ch.Create()},
		{"update", http.MethodPut, `{"title":"x"}`, ch.Update()},
		{"delete", http.MethodDelete, "", ch.Delete()},
	}
	for _, route := range routes {
		for _, user := range []string{"", "alice"} {
			req := makeRequest(t, RequestOpts{Method: route.method, Path: "/notes/a1", Body: route.body, UserID: user})
			req.SetPathValue("id", "a1")
			rr := httptest.NewRecorder()
			route.handler(rr, req)
			if cc := rr.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("%s as %q: %d, Cache-Control %q, want no-store", route.name, user, rr.Code, cc)
			}
		}
	}
}
