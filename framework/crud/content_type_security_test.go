package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestCreate_RejectsTextPlainJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON create accepted text/plain, enabling simple-request CSRF without preflight")
}

func TestCreate_RejectsFormURLEncodedJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON create accepted application/x-www-form-urlencoded, enabling simple-request CSRF without preflight")
}

func TestUpdate_RejectsTextPlainJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/p1", strings.NewReader(`{"title":"changed"}`))
	req.Header.Set("Content-Type", "text/plain")
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON update accepted text/plain, enabling simple-request CSRF without preflight")
}

func TestUpdate_RejectsFormURLEncodedJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/p1", strings.NewReader(`{"title":"changed"}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON update accepted application/x-www-form-urlencoded, enabling simple-request CSRF without preflight")
}

func TestCreate_RejectsMissingContentType(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON create accepted a body with no Content-Type, enabling ambiguous parsing and CSRF")
}

func TestUpdate_RejectsMissingContentType(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/p1", strings.NewReader(`{"title":"changed"}`))
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"JSON update accepted a body with no Content-Type, enabling ambiguous parsing and CSRF")
}

func TestBatchCreate_RejectsTextPlainJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts/_batch", strings.NewReader(`{"items":[{"title":"hello"}]}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch create accepted text/plain JSON, enabling simple-request CSRF without preflight")
}

func TestBatchCreate_RejectsFormURLEncodedJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts/_batch", strings.NewReader(`{"items":[{"title":"hello"}]}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch create accepted application/x-www-form-urlencoded, enabling simple-request CSRF without preflight")
}

func TestBatchCreate_RejectsMissingContentType(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	req := httptest.NewRequest(http.MethodPost, "/posts/_batch", strings.NewReader(`{"items":[{"title":"hello"}]}`))
	rec := httptest.NewRecorder()
	ch.BatchCreate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch create accepted a body with no Content-Type, enabling ambiguous parsing and CSRF")
}

func TestBatchUpdate_RejectsTextPlainJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/_batch", strings.NewReader(`{"items":[{"id":"p1","title":"changed"}]}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch update accepted text/plain JSON, enabling simple-request CSRF without preflight")
}

func TestBatchUpdate_RejectsFormURLEncodedJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/_batch", strings.NewReader(`{"items":[{"id":"p1","title":"changed"}]}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch update accepted application/x-www-form-urlencoded, enabling simple-request CSRF without preflight")
}

func TestBatchUpdate_RejectsMissingContentType(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodPatch, "/posts/_batch", strings.NewReader(`{"items":[{"id":"p1","title":"changed"}]}`))
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch update accepted a body with no Content-Type, enabling ambiguous parsing and CSRF")
}

func TestBatchDelete_RejectsTextPlainJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodDelete, "/posts/_batch", strings.NewReader(`{"ids":["p1"]}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch delete accepted text/plain JSON, enabling simple-request CSRF without preflight")
}

func TestBatchDelete_RejectsFormURLEncodedJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodDelete, "/posts/_batch", strings.NewReader(`{"ids":["p1"]}`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch delete accepted application/x-www-form-urlencoded, enabling simple-request CSRF without preflight")
}

func TestBatchDelete_RejectsMissingContentType(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "hello"}})

	req := httptest.NewRequest(http.MethodDelete, "/posts/_batch", strings.NewReader(`{"ids":["p1"]}`))
	rec := httptest.NewRecorder()
	ch.BatchDelete()(rec, req)

	assertStatus(t, rec, http.StatusUnsupportedMediaType, "content-type",
		"batch delete accepted a body with no Content-Type, enabling ambiguous parsing and CSRF")
}
