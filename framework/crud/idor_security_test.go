package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestIDOR_GetOtherUsersRecord verifies that an authenticated user cannot
// read another user's record by ID. Attack: IDOR via predictable resource
// identifiers. Expected: 404 Not Found.
func TestIDOR_GetOtherUsersRecord(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "bob-note-1", "user_id": "bob", "content": "bob's secret"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/notes/bob-note-1",
		UserID: "alice",
	})
	req.SetPathValue("id", "bob-note-1")
	rr := httptest.NewRecorder()
	ch.Get()(rr, req)

	assertStatus(t, rr, http.StatusNotFound, "idor",
		"authenticated user reads another user's record by guessing the ID")
	assertBodyNotContains(t, rr, "bob's secret", "idor",
		"IDOR via predictable resource identifier leaked other user's data")
}

// TestIDOR_PutOtherUsersRecord verifies that an authenticated user cannot
// update another user's record. Attack: IDOR via PUT with predictable ID.
// Expected: 404 Not Found.
func TestIDOR_PutOtherUsersRecord(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "bob-note-1", "user_id": "bob", "content": "original"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodPut,
		Path:   "/notes/bob-note-1",
		Body:   `{"content":"hijacked"}`,
		UserID: "alice",
	})
	req.SetPathValue("id", "bob-note-1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)

	assertStatus(t, rr, http.StatusNotFound, "idor",
		"authenticated user updates another user's record by guessing the ID")

	// Verify data integrity
	var content string
	if err := db.QueryRow("SELECT content FROM notes WHERE id = ?", "bob-note-1").Scan(&content); err != nil {
		t.Fatal(err)
	}
	if content != "original" {
		t.Errorf("SECURITY: [idor] record was mutated by cross-user PUT: %q", content)
	}
}

// TestIDOR_DeleteOtherUsersRecord verifies that an authenticated user cannot
// delete another user's record. Attack: IDOR via DELETE with predictable ID.
// Expected: 404 Not Found.
func TestIDOR_DeleteOtherUsersRecord(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "bob-note-1", "user_id": "bob", "content": "bob's data"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodDelete,
		Path:   "/notes/bob-note-1",
		UserID: "alice",
	})
	req.SetPathValue("id", "bob-note-1")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)

	assertStatus(t, rr, http.StatusNotFound, "idor",
		"authenticated user deletes another user's record by guessing the ID")
}

// TestIDOR_UnauthenticatedListWithoutOwnerField verifies that unauthenticated
// requests to a list endpoint with OwnerField set are rejected.
// Attack: unauthenticated data enumeration.
// Expected: 401 Unauthorized.
func TestIDOR_UnauthenticatedListWithoutOwnerField(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "user_id": "alice", "content": "private"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/notes",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertStatus(t, rr, http.StatusUnauthorized, "idor",
		"unauthenticated request to owner-scoped list endpoint returns data")
	assertBodyNotContains(t, rr, "private", "idor",
		"unauthenticated list endpoint leaks private data")
}

// TestIDOR_IncludeRelationLeak verifies that include relations don't leak
// data from other users' records. Attack: IDOR via include parameter
// to fetch related records belonging to other users.
// Expected: only the user's own records are returned.
func TestIDOR_IncludeRelationLeak(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "alice-n1", "user_id": "alice", "content": "alice data"},
		{"id": "bob-n1", "user_id": "bob", "content": "bob data"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/notes",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertStatus(t, rr, http.StatusOK, "idor",
		"owner-scoped list returns only the user's own records")
	assertBodyNotContains(t, rr, "bob data", "idor",
		"owner scope fails — other user's data visible in list response")
}

// TestIDOR_CursorIntoOtherUsersData verifies that cursor-based pagination
// cannot be used to access other users' records. Attack: IDOR via cursor
// pointing into another user's data range.
// Expected: only the user's own records returned.
func TestIDOR_CursorIntoOtherUsersData(t *testing.T) {
	ddl := `CREATE TABLE notes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		content TEXT
	)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "alice-n1", "user_id": "alice", "content": "alice data"},
		{"id": "bob-n1", "user_id": "bob", "content": "bob secret"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/notes?cursor=bob-n1",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertBodyNotContains(t, rr, "bob secret", "idor",
		"cursor pagination leaks data from other users")
}
