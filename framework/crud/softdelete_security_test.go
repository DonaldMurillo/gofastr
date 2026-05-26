package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

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
