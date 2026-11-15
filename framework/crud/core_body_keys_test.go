package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A JSON body repeating a key inside a nested object is refused, as a
// repeated top-level key is: a decode would keep whichever came last.
func TestBodyRefusesNestedDuplicateKey(t *testing.T) {
	ddl := `CREATE TABLE docs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, doc TEXT);`
	cfg := makeEntityConfig("docs", "docs", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "doc", Type: schema.JSON},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	t.Cleanup(func() { db.Close() })
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/docs", Body: `{"doc":{"role":"user","role":"admin"}}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("nested duplicate key: %d %s, want 400", rr.Code, rr.Body.String())
	}
}
