package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestBatchCreate_DatabaseErrorDoesNotLeakDriverText(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	_ = db.Close()

	req := httptest.NewRequest(http.MethodPost, "/posts/_batch", strings.NewReader(`{"items":[{"title":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	body := strings.ToLower(rec.Body.String())
	if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
		t.Fatalf("SECURITY: [batch-error] batch create leaked raw driver text: %s", rec.Body.String())
	}
}

func TestBatchUpdate_DatabaseErrorDoesNotLeakDriverText(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	_ = db.Close()

	req := httptest.NewRequest(http.MethodPatch, "/posts/_batch", strings.NewReader(`{"items":[{"id":"p1","title":"changed"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	body := strings.ToLower(rec.Body.String())
	if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
		t.Fatalf("SECURITY: [batch-error] batch update leaked raw driver text: %s", rec.Body.String())
	}
}

func TestBatchDelete_DatabaseErrorDoesNotLeakDriverText(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	_ = db.Close()

	req := httptest.NewRequest(http.MethodDelete, "/posts/_batch", strings.NewReader(`{"ids":["p1"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, req)

	body := strings.ToLower(rec.Body.String())
	if strings.Contains(body, "database is closed") || strings.Contains(body, "sql:") {
		t.Fatalf("SECURITY: [batch-error] batch delete leaked raw driver text: %s", rec.Body.String())
	}
}
