package crud

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// sessionLikeMiddleware faithfully reproduces battery/auth.SessionMiddleware's
// auth-resolution contract WITHOUT importing the battery (framework/crud must
// not depend on a battery). The load-bearing behaviour for this test is the
// "no session cookie → clear the user to anonymous" branch: SessionMiddleware
// calls handler.SetUser(ctx, nil) on every request that lacks a valid cookie,
// overwriting any user an earlier layer resolved. That is exactly what
// demotes an authenticated MCP CRUD call to anonymous when the internal
// re-dispatched request carries no cookie.
func sessionLikeMiddleware(cookieName string, users map[string]*testUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err != nil || c.Value == "" {
				// No cookie → anonymous. MUST clear any pre-resolved user.
				next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), nil)))
				return
			}
			u, ok := users[c.Value]
			if !ok {
				next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), nil)))
				return
			}
			next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), u)))
		})
	}
}

// TestMCPAuthenticatedListReturnsOwnerRows pins the end-to-end contract that
// an authenticated MCP _list / _get call against an OwnerField entity returns
// that user's rows — not 401 — even when SessionMiddleware sits in the router
// chain. The internal request runToolRequest builds carries no session cookie,
// so a re-running SessionMiddleware would demote the user to anonymous and
// RequireOwner/owner-scoping would 401. The fix copies the original request's
// auth (Cookie/Authorization) onto the internal request so the same session
// re-resolves.
func TestMCPAuthenticatedListReturnsOwnerRows(t *testing.T) {
	installSecurityOwnerExtractor(t)

	db := setupDB(t, `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, body TEXT)`)
	ent := entity.Define("notes", makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "body", Type: schema.String},
	}))
	ent.SetDB(db)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n-alice", "user_id": "alice", "body": "alice secret"},
		{"id": "n-bob", "user_id": "bob", "body": "bob secret"},
	})

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	const cookieName = "fastr_session"
	users := map[string]*testUser{"sess-alice": {id: "alice"}}

	r := router.New()
	r.Use(sessionLikeMiddleware(cookieName, users))
	RegisterCrudRoutes(r, ch, "/notes")

	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}

	// Simulate the MCP transport: it stashes the ORIGINAL *http.Request
	// (carrying the session cookie) under the mcp context key and resolves
	// the user into ctx via the owner extractor / handler.SetUser.
	orig := newTestRequestWithCookie(cookieName, "sess-alice")
	ctx := mcp.WithRequest(context.Background(), orig)
	ctx = handler.SetUser(ctx, &testUser{id: "alice"})

	listed, err := srv.CallTool(ctx, "notes_list", map[string]any{})
	if err != nil {
		t.Fatalf("notes_list as authenticated alice returned error (likely 401): %v", err)
	}
	m, ok := listed.(map[string]any)
	if !ok {
		t.Fatalf("notes_list returned %T, want map", listed)
	}
	rows, ok := m["data"].([]any)
	if !ok {
		t.Fatalf("notes_list missing data array: %v", m)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly alice's 1 row, got %d: %v", len(rows), rows)
	}
	body := strings.Join([]string{}, "")
	for _, row := range rows {
		rm := row.(map[string]any)
		if rm["user_id"] != "alice" {
			t.Errorf("owner scope leaked a non-alice row: %v", rm)
		}
		body += rm["body"].(string)
	}
	if !strings.Contains(body, "alice secret") {
		t.Errorf("alice's own row was not returned: %v", rows)
	}

	// _get of alice's own row must also resolve, not 401.
	got, err := srv.CallTool(ctx, "notes_get", map[string]any{"id": "n-alice"})
	if err != nil {
		t.Fatalf("notes_get of alice's own row returned error (likely 401): %v", err)
	}
	if got.(map[string]any)["body"] != "alice secret" {
		t.Errorf("notes_get returned wrong row: %v", got)
	}
}

// newTestRequestWithCookie builds a request carrying a session cookie,
// standing in for the original MCP HTTP request.
func newTestRequestWithCookie(cookieName, value string) *http.Request {
	req, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: value})
	return req
}
