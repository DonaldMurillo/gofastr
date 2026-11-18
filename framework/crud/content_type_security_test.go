package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Every JSON write surface must refuse a body whose Content-Type is not
// JSON: text/plain and application/x-www-form-urlencoded are both
// "simple request" content types a cross-site form or fetch can send
// without a CORS preflight, so accepting JSON from them enables
// simple-request CSRF. A missing Content-Type additionally invites
// ambiguous parsing. Each case keeps the original surface, seeding the
// row the update/delete variants target.
func TestJSONSurfacesRejectNonJSONContentType(t *testing.T) {
	surfaces := []struct {
		name   string
		method string
		path   string
		body   string
		seed   bool
		serve  func(ch *CrudHandler) http.HandlerFunc
	}{
		{"create", http.MethodPost, "/posts", `{"title":"hello"}`, false,
			func(ch *CrudHandler) http.HandlerFunc { return ch.Create() }},
		{"update", http.MethodPatch, "/posts/p1", `{"title":"changed"}`, true,
			func(ch *CrudHandler) http.HandlerFunc { return ch.Update() }},
		{"batch create", http.MethodPost, "/posts/_batch", `{"items":[{"title":"hello"}]}`, false,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchCreate() }},
		{"batch update", http.MethodPatch, "/posts/_batch", `{"items":[{"id":"p1","title":"changed"}]}`, true,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchUpdate() }},
		{"batch delete", http.MethodDelete, "/posts/_batch", `{"ids":["p1"]}`, true,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchDelete() }},
	}
	contentTypes := []struct {
		name string
		ct   string // empty means: send no Content-Type header at all
		risk string
	}{
		{"text/plain", "text/plain", "simple-request CSRF without preflight"},
		{"form-urlencoded", "application/x-www-form-urlencoded", "simple-request CSRF without preflight"},
		{"missing", "", "ambiguous parsing and CSRF"},
	}

	for _, s := range surfaces {
		for _, ct := range contentTypes {
			t.Run(s.name+"/"+ct.name, func(t *testing.T) {
				ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
					{Name: "title", Type: schema.String},
				}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
				if s.seed {
					seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})
				}

				req := httptest.NewRequest(s.method, s.path, strings.NewReader(s.body))
				if ct.ct != "" {
					req.Header.Set("Content-Type", ct.ct)
				}
				if s.path == "/posts/p1" {
					req.SetPathValue("id", "p1")
				}
				rec := httptest.NewRecorder()
				s.serve(ch)(rec, req)

				assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
					fmt.Sprintf("JSON %s accepted %s, enabling %s", s.name, ct.name, ct.risk))
			})
		}
	}
}
