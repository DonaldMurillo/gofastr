package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// With CSRF on every route, a cookie-authenticated MCP write tool got a
// 403 every time: the outer /mcp POST passed the check with its token
// header, and the internal POST runToolRequest re-dispatched carried the
// CSRF cookie but no header (2026-10-27 bug bash P23). The re-dispatch
// now re-presents the token the outer check verified, under the header
// that middleware reads, and the inner check runs in full.
func TestMCPWriteToolPassesCSRF(t *testing.T) {
	installSecurityOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, body TEXT)`)
	ent := entity.Define("notes", makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "body", Type: schema.String},
	}))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	const cookieName = "fastr_session"
	// A header name other than the default, so the test fails if the
	// re-dispatch hard-codes X-CSRF-Token.
	csrf := middleware.CSRF(middleware.CSRFConfig{HeaderName: "X-Csrf-Check", SecretKey: []byte("csrf-test-key-0123456789abcdef")}) // not-a-secret: test-only HMAC key
	r := router.New()
	r.Use(csrf)
	r.Use(sessionLikeMiddleware(cookieName, map[string]*testUser{"sess-alice": {id: "alice"}}))
	RegisterCrudRoutes(r, ch, "/notes")
	srv := mcp.NewServer()
	if err := RegisterEntityMCPTools(srv, ch, r); err != nil {
		t.Fatalf("register mcp: %v", err)
	}

	// A GET mints the CSRF cookie the page's script would read.
	mint := httptest.NewRecorder()
	r.ServeHTTP(mint, httptest.NewRequest(http.MethodGet, "/notes", nil))
	var token *http.Cookie
	for _, c := range mint.Result().Cookies() {
		if c.Name == "csrf_token" {
			token = c
		}
	}
	if token == nil {
		t.Fatalf("no CSRF cookie minted: %v", mint.Result().Cookies())
	}

	call := func(t *testing.T, outer *http.Request) error {
		t.Helper()
		ctx := mcp.WithRequest(context.Background(), outer)
		ctx = handler.SetUser(ctx, &testUser{id: "alice"})
		_, err := srv.CallTool(ctx, "notes_create", map[string]any{"body": "hello"})
		return err
	}
	outer := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: "sess-alice"})
		req.AddCookie(token)
		return req
	}

	t.Run("verified outer request", func(t *testing.T) {
		req := outer()
		req.Header.Set("X-Csrf-Check", token.Value)
		// The /mcp handler sees the request the CSRF check passed.
		var verified *http.Request
		csrf(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { verified = r })).ServeHTTP(httptest.NewRecorder(), req)
		if verified == nil {
			t.Fatal("outer /mcp request refused by CSRF")
		}
		if err := call(t, verified); err != nil {
			t.Fatalf("notes_create through MCP: %v", err)
		}
	})
	t.Run("unverified outer request", func(t *testing.T) {
		// A request no CSRF check passed (a transport that skipped it)
		// has no token to re-present, header or not: the inner check
		// refuses the write.
		req := outer()
		req.Header.Set("X-Csrf-Check", token.Value)
		if err := call(t, req); err == nil {
			t.Fatal("write re-dispatched from an unverified request passed CSRF")
		}
	})
}
