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
// that user's rows, not 401, even when SessionMiddleware sits in the router
// chain. The internal request runToolRequest builds carries no session cookie,
// so a re-running SessionMiddleware would demote the user to anonymous and
// RequireOwner/owner-scoping would 401. The fix copies every field of every
// credential header (Cookie/Authorization/X-API-Key/embed grant) onto the
// internal request so the same session re-resolves.
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

// apiKeyLikeMiddleware mirrors battery/auth's apitoken resolution (identity
// from X-API-Key) without importing the battery. Same anonymous-demotion
// contract as sessionLikeMiddleware: an unrecognized or missing key clears
// the user, so an internal re-dispatch that lost the header is a 401, not a
// silently different caller.
func apiKeyLikeMiddleware(users map[string]*testUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := r.Header.Get("X-API-Key")
			u, ok := users[k]
			if k == "" || !ok {
				next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), nil)))
				return
			}
			next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), u)))
		})
	}
}

// trustHeaderMiddleware models a NON-credential header an ingress sets and a
// caller must never control (X-Internal-Trust): when present it resolves the
// request as a superuser that sees every row. It is the no-extra-authority
// instrument: if runToolRequest ever copies headers beyond the credential
// canon from the stashed original, the re-dispatch resolves as superuser and
// the test fails on row count. Absent the header it passes through
// untouched, so it composes behind a session middleware without erasing the
// session's user.
func trustHeaderMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Internal-Trust") != "" {
				next.ServeHTTP(w, r.WithContext(handler.SetUser(r.Context(), &testUser{id: "superuser"})))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// newMCPAuthEnv builds the shared fixture for the re-dispatch credential
// tests: an owner-scoped notes entity with one row per user behind mw,
// exposed through MCP tools whose calls re-dispatch via the router.
func newMCPAuthEnv(t *testing.T, mw func(http.Handler) http.Handler) (*mcp.Server, *CrudHandler) {
	t.Helper()
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
	r := router.New()
	r.Use(mw)
	RegisterCrudRoutes(r, ch, "/notes")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}
	return srv, ch
}

func callListRows(t *testing.T, srv *mcp.Server, ctx context.Context) []map[string]any {
	t.Helper()
	listed, err := srv.CallTool(ctx, "notes_list", map[string]any{})
	if err != nil {
		t.Fatalf("notes_list returned error (likely 401): %v", err)
	}
	m, ok := listed.(map[string]any)
	if !ok {
		t.Fatalf("notes_list returned %T, want map", listed)
	}
	rows, _ := m["data"].([]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.(map[string]any))
	}
	return out
}

// TestMCPRedispatchReattachesAPIKey pins the X-API-Key half of the
// credential canon. runToolRequest used to copy Cookie, Authorization, and
// the embed grant but not X-API-Key, so an API-key caller (battery/auth
// apitoken) was demoted to anonymous on every entity-MCP re-dispatch —
// issue #360's both-layers gap, this is the crud half of it.
func TestMCPRedispatchReattachesAPIKey(t *testing.T) {
	srv, _ := newMCPAuthEnv(t, apiKeyLikeMiddleware(map[string]*testUser{"gfsk-alice": {id: "alice"}}))

	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	orig.Header.Set("X-API-Key", "gfsk-alice")
	ctx := mcp.WithRequest(context.Background(), orig)

	rows := callListRows(t, srv, ctx)
	if len(rows) != 1 || rows[0]["user_id"] != "alice" {
		t.Fatalf("API-key caller's re-dispatch returned %v, want exactly alice's row", rows)
	}
}

// TestMCPRedispatchPreservesEveryCookieField pins the multi-field half:
// runToolRequest copied the stashed request's Cookie via Get()/Set(), which
// keeps only the FIRST header field. A proxy that prepends its own
// "Cookie: edge=blue" before the browser's session field therefore silenced
// the session on every re-dispatch (same collapse MintDelegation had, and
// the exact scenario app.go:889-896 documents for credentialFingerprint).
func TestMCPRedispatchPreservesEveryCookieField(t *testing.T) {
	const cookieName = "fastr_session"
	srv, _ := newMCPAuthEnv(t, sessionLikeMiddleware(cookieName, map[string]*testUser{"sess-alice": {id: "alice"}}))

	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	orig.Header.Add("Cookie", "edge=blue")              // proxy-prepended field
	orig.Header.Add("Cookie", cookieName+"=sess-alice") // the session, second field
	ctx := mcp.WithRequest(context.Background(), orig)

	rows := callListRows(t, srv, ctx)
	if len(rows) != 1 || rows[0]["user_id"] != "alice" {
		t.Fatalf("multi-field-cookie caller's re-dispatch returned %v, want exactly alice's row", rows)
	}
}

// TestMCPRedispatchCopiesOnlyCredentialHeaders pins the fail-closed
// direction: the original request carried a trust header it should never
// have been able to spend (an ingress-set internal header) alongside its
// session cookie; the re-dispatch must inherit the cookie identity only.
// If runToolRequest ever copies headers wholesale from the stashed original,
// the re-dispatch resolves as the superuser (a different identity: her own
// row set, not alice's) and the test fails.
func TestMCPRedispatchCopiesOnlyCredentialHeaders(t *testing.T) {
	const cookieName = "fastr_session"
	users := map[string]*testUser{"sess-alice": {id: "alice"}}
	mw := func(next http.Handler) http.Handler {
		trust := trustHeaderMiddleware()(next)
		return sessionLikeMiddleware(cookieName, users)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			trust.ServeHTTP(w, r)
		}))
	}
	srv, _ := newMCPAuthEnv(t, mw)

	orig, _ := http.NewRequest(http.MethodPost, "/mcp", nil)
	orig.Header.Add("Cookie", cookieName+"=sess-alice")
	orig.Header.Set("X-Internal-Trust", "1")
	ctx := mcp.WithRequest(context.Background(), orig)

	rows := callListRows(t, srv, ctx)
	if len(rows) != 1 || rows[0]["user_id"] != "alice" {
		t.Fatalf("re-dispatch inherited authority the cookie caller never held: got %v, want exactly alice's row", rows)
	}
}
