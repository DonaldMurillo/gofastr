package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestSoftDelete_IncludeHidesDeletedRelatedRows pins that eager-loaded
// (?include=) rows on a SoftDelete related entity hide rows whose
// deleted_at is set — same as direct reads. Attack: a soft-deleted comment
// leaks through `/posts?include=comments` because the include loader never
// appended `deleted_at IS NULL`.
func TestSoftDelete_IncludeHidesDeletedRelatedRows(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id    TEXT PRIMARY KEY,
	title TEXT
);
CREATE TABLE comments (
	id         TEXT PRIMARY KEY,
	post_id    TEXT NOT NULL,
	body       TEXT,
	deleted_at TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "",
		[]schema.Field{{Name: "title", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{entity.HasMany("comments", "comments", "post_id")}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "",
		[]schema.Field{
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		func(c *entity.EntityConfig) { c.SoftDelete = true },
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "title": "post"}})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-live", "post_id": "p1", "body": "live comment", "deleted_at": nil},
		{"id": "c-dead", "post_id": "p1", "body": "deleted comment", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts?include=comments"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "deleted comment") {
		t.Errorf("SECURITY: [softdelete] include=comments leaked a soft-deleted related row. Body: %s", body)
	}
	if !strings.Contains(body, "live comment") {
		t.Errorf("include dropped the live comment — soft-delete filter too aggressive. Body: %s", body)
	}
}

// TestSoftDelete_UnauthenticatedTrashedListRejected verifies that
// unauthenticated requests for trashed records are rejected.
// Attack: anonymous user accesses soft-deleted records via ?trashed=true.
func TestSoftDelete_UnauthenticatedTrashedListRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("tasks", "tasks", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true }),
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, deleted_at TEXT)`)

	seedRows(t, db, "tasks", []map[string]any{
		{"id": "task-1", "user_id": "alice", "title": "deleted task", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/tasks?trashed=true",
		// No UserID — anonymous
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertStatus(t, rr, http.StatusUnauthorized, "softdelete",
		"anonymous user accesses trashed records via ?trashed=true")
	assertBodyNotContains(t, rr, "deleted task", "softdelete",
		"trashed records leaked to unauthenticated user")
}

// TestSoftDelete_CrossUserTrashedAccessRejected verifies that one user
// cannot access another user's soft-deleted records via ?trashed=true.
// Attack: IDOR into another user's trashed records.
func TestSoftDelete_CrossUserTrashedAccessRejected(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("tasks", "tasks", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true }),
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, deleted_at TEXT)`)

	seedRows(t, db, "tasks", []map[string]any{
		{"id": "task-bob", "user_id": "bob", "title": "bob's deleted secret", "deleted_at": "2024-01-01T00:00:00Z"},
		{"id": "task-alice", "user_id": "alice", "title": "alice's active task", "deleted_at": ""},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/tasks?trashed=true",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertBodyNotContains(t, rr, "bob's deleted secret", "softdelete",
		"cross-user access to trashed records via ?trashed=true")
}

// TestSoftDelete_ForceDeleteScopedToOwner verifies that hard-delete
// (force=true) is scoped to the requesting user's records. Attack:
// force-deleting another user's soft-deleted records.
func TestSoftDelete_ForceDeleteScopedToOwner(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("tasks", "tasks", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true }),
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, deleted_at TEXT)`)

	seedRows(t, db, "tasks", []map[string]any{
		{"id": "task-bob", "user_id": "bob", "title": "bob data", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	// Alice tries to force-delete bob's soft-deleted record
	req := makeRequest(t, RequestOpts{
		Method: http.MethodDelete,
		Path:   "/tasks/task-bob?force=true",
		UserID: "alice",
	})
	req.SetPathValue("id", "task-bob")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)

	assertStatus(t, rr, http.StatusNotFound, "softdelete",
		"cross-user force-delete of soft-deleted record")

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks WHERE id = ?", "task-bob").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("SECURITY: [softdelete] cross-user force-delete removed bob's record")
	}
}

// suppress unused import
var _ = schema.String
