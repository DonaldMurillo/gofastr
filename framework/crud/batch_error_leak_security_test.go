package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestBatch_DatabaseErrorDoesNotLeakDriverText drives every batch surface
// against a closed database and requires that raw driver error text
// ("database is closed", "sql: ...") never reaches the response body.
func TestBatch_DatabaseErrorDoesNotLeakDriverText(t *testing.T) {
	cases := []struct {
		name   string
		method string
		body   string
		serve  func(ch *CrudHandler) http.HandlerFunc
	}{
		{"batch create", http.MethodPost, `{"items":[{"title":"hello"}]}`,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchCreate() }},
		{"batch update", http.MethodPatch, `{"items":[{"id":"p1","title":"changed"}]}`,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchUpdate() }},
		{"batch delete", http.MethodDelete, `{"ids":["p1"]}`,
			func(ch *CrudHandler) http.HandlerFunc { return ch.BatchDelete() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
				{Name: "title", Type: schema.String},
			}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			req := makeRequest(t, RequestOpts{
				Method: tc.method,
				Path:   "/posts/_batch",
				Body:   tc.body,
				UserID: "alice",
			})
			rec := httptest.NewRecorder()
			tc.serve(ch)(rec, req)

			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%s: status = %d, want 500: %s", tc.name, rec.Code, rec.Body.String())
			}
			body := strings.ToLower(rec.Body.String())
			if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
				t.Fatalf("SECURITY: [batch-error] %s leaked raw driver text: %s", tc.name, rec.Body.String())
			}
		})
	}
}
