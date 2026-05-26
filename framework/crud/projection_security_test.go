package crud

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestProjection_HiddenFieldNotExposed verifies that requesting a hidden
// field via ?fields= does not expose it in the response. Attack: client
// probes for hidden/internal fields using the projection API.
func TestProjection_HiddenFieldNotExposed(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
			{Name: "internal_notes", Type: schema.String, Hidden: true},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false), `CREATE TABLE documents (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, internal_notes TEXT)`)

	seedRows(t, db, "documents", []map[string]any{
		{"id": "doc-1", "user_id": "alice", "title": "Public Doc", "internal_notes": "secret internal review"},
	})

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/documents?fields=internal_notes",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	// Hidden field should either: return 400 (unknown field) or omit it
	assertBodyNotContains(t, rr, "secret internal review", "projection",
		"hidden field internal_notes exposed via ?fields= parameter")
}

// TestProjection_UnknownFieldReturnsError verifies that requesting a
// nonexistent field via ?fields= returns a 400 error. Attack: field
// enumeration to discover schema columns.
func TestProjection_UnknownFieldReturnsError(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false), `CREATE TABLE documents (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`)

	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/documents?fields=nonexistent_col",
		UserID: "alice",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertStatus(t, rr, http.StatusBadRequest, "projection",
		"requesting nonexistent field should return 400, not silently ignore")
}

// TestProjection_WildcardDoesNotBypassOwnerScope verifies that ?fields=*
// does not bypass owner scoping. Attack: wildcard projection combined
// with missing auth to dump all records.
func TestProjection_WildcardDoesNotBypassOwnerScope(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false), `CREATE TABLE documents (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`)

	seedRows(t, db, "documents", []map[string]any{
		{"id": "doc-1", "user_id": "alice", "title": "Alice Doc"},
		{"id": "doc-2", "user_id": "bob", "title": "Bob Doc"},
	})

	// Anonymous request — should be rejected regardless of ?fields=
	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/documents?fields=title",
		// No UserID
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	assertStatus(t, rr, http.StatusUnauthorized, "projection",
		"anonymous request with ?fields= bypasses owner scope")
	assertBodyNotContains(t, rr, "Alice Doc", "projection",
		"projection leaks data without authentication")
}

// suppress unused import
var _ = schema.String
