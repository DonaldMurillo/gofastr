package auth_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A scoped API token is confined to the entities its scopes name, on every
// route that reaches an entity, not only the one in the path.
// RequireAPIScopes checks the path's entity; ?include=, ?rel.field= filters
// and cascade writes reach a DIFFERENT entity from that route, and each has to
// hold the target to the same scope.

type relScopeWorld struct {
	app      *framework.App
	db       *sql.DB
	readTok  string
	writeTok string
	bothTok  string
}

func newRelScopeWorld(t *testing.T) relScopeWorld {
	t.Helper()
	db := relScopeDB(t)
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware(), framework.WithAPIPrefix("/api"))
	app.Entity("users", auth.UserEntityConfig())
	inv := entity.HasMany("invoices", "invoices", "customer_id")
	inv.CascadeWrite = true
	app.Entity("customers", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Relations: []entity.Relation{inv},
	}.WithTimestamps(false))
	app.Entity("invoices", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "customer_id", Type: schema.String},
			{Name: "memo", Type: schema.String},
		},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	users := auth.NewEntityUserStore(db, "users")
	tokens, err := auth.NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	// Stands in for embeds.Middleware: a verified grant on the context, its
	// subject as the user. The header is test plumbing, not a credential.
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s := r.Header.Get("X-Test-Grant"); s != "" {
				ctx := embed.WithGrant(r.Context(), embed.Grant{Surface: "t", Subject: "u-embed", Scopes: strings.Split(s, ",")})
				r = r.WithContext(handler.SetUser(ctx, &auth.BasicUser{ID: "u-embed"}))
			}
			next.ServeHTTP(w, r)
		})
	})
	app.Use(auth.TokenMiddleware(users, nil, tokens))
	app.Use(auth.RequireAPIScopes("/api"))

	ctx := context.Background()
	u, err := users.CreateUser(ctx, "owner@example.test", "pw-unused-123", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO customers (id, name) VALUES ('c1','Acme')`,
		`INSERT INTO invoices (id, customer_id, memo) VALUES ('i1','c1','SCOPE-INVOICE-MEMO')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(scopes ...string) string {
		tok, _, err := auth.IssueToken(ctx, tokens, auth.TokenSpec{Name: strings.Join(scopes, ","), OwnerKind: "user", OwnerID: u.GetID(), Scopes: scopes})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	return relScopeWorld{
		app: app, db: db,
		readTok:  issue("customers:read"),
		writeTok: issue("customers:write"),
		bothTok:  issue("customers:*", "invoices:*"),
	}
}

func TestScopedTokenIncludeNeedsTargetScope(t *testing.T) {
	w := newRelScopeWorld(t)
	for _, p := range []string{"/api/customers?include=invoices", "/api/customers/c1?include=invoices"} {
		code, body := relScopeDo(w.app, "GET", p, "", nil, w.readTok)
		if code != http.StatusForbidden || strings.Contains(body, "SCOPE-INVOICE-MEMO") {
			t.Errorf("SECURITY: customers:read GET %s = %d %s, want 403", p, code, body)
		}
		code, body = relScopeDo(w.app, "GET", p, "", nil, w.bothTok)
		if code != http.StatusOK || !strings.Contains(body, "SCOPE-INVOICE-MEMO") {
			t.Errorf("customers+invoices token GET %s = %d %s, want 200 with the invoice", p, code, body)
		}
	}
}

func TestScopedTokenNestedFilterNeedsScope(t *testing.T) {
	w := newRelScopeWorld(t)
	for _, p := range []string{"/api/customers?invoices.memo_like=SCOPE-INV", "/api/customers?invoices.memo_like=zzz"} {
		code, body := relScopeDo(w.app, "GET", p, "", nil, w.readTok)
		if code != http.StatusForbidden {
			t.Errorf("SECURITY: customers:read GET %s = %d %s, want 403", p, code, body)
		}
	}
	code, body := relScopeDo(w.app, "GET", "/api/customers?invoices.memo_like=SCOPE-INV", "", nil, w.bothTok)
	if code != http.StatusOK || !strings.Contains(body, `"total":1`) {
		t.Errorf("customers+invoices token nested filter = %d %s, want 200 total 1", code, body)
	}
}

func TestScopedTokenCascadeNeedsTargetScope(t *testing.T) {
	w := newRelScopeWorld(t)
	code, body := relScopeDo(w.app, "POST", "/api/customers", `{"name":"Nested","invoices":[{"memo":"SCOPE-CASCADE"}]}`, nil, w.writeTok)
	var n int
	if err := w.db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE memo = 'SCOPE-CASCADE'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if code < 400 || n != 0 {
		t.Errorf("SECURITY: customers:write cascade create = %d %s, %d invoice rows written", code, body, n)
	}
	code, body = relScopeDo(w.app, "POST", "/api/customers", `{"name":"Nested2","invoices":[{"memo":"SCOPE-CASCADE-OK"}]}`, nil, w.bothTok)
	if code != http.StatusCreated {
		t.Errorf("customers+invoices cascade create = %d %s, want 201", code, body)
	}
}

// An embed grant is scoped authority in the same grammar; the embed middleware
// installs it through embed.WithGrant, which must narrow relations the same way.
func TestEmbedGrantIncludeNeedsTargetScope(t *testing.T) {
	w := newRelScopeWorld(t)
	get := func(scopes, path string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Test-Grant", scopes)
		rec := httptest.NewRecorder()
		w.app.Router().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, p := range []string{"/api/customers?include=invoices", "/api/customers?invoices.memo_like=SCOPE-INV"} {
		if code, body := get("customers:read", p); code != http.StatusForbidden || strings.Contains(body, "SCOPE-INVOICE-MEMO") {
			t.Errorf("SECURITY: grant customers:read GET %s = %d %s, want 403", p, code, body)
		}
		if code, body := get("customers:read,invoices:read", p); code != http.StatusOK {
			t.Errorf("grant customers+invoices GET %s = %d %s, want 200", p, code, body)
		}
	}
}
