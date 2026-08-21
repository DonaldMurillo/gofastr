package framework

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// "Open reads, gated writes", a blank Access.Read beside real Create/Update/
// Delete permissions, is the shape `gofastr generate` recommends in place of
// `public: true`, precisely because Public also grants anonymous writes.
//
// framework/crud enforces this correctly when a CrudHandler is exercised
// directly (see crud/open_read_gated_write_security_test.go). This test drives
// the same declaration through App, registration, route mounting, and the
// real middleware chain, which is what every generated app uses and what a
// user actually deploys.
func openReadGatedWriteApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "gated", APIPrefix: "/api"}), WithDB(db))
	app.Entity("users", EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
		Exposure: &entity.ExposureConfig{
			CRUD:   boolPtrGated(true),
			Access: entity.AccessControl{Read: "users:read", Create: "users:write", Update: "users:write", Delete: "users:admin"},
		},
	})
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.Relation, To: "users"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "author", Entity: "users", ForeignKey: "author_id"},
		},
		Scope: &entity.ScopeConfig{SoftDelete: true},
		Exposure: &entity.ExposureConfig{
			MCP:  true,
			CRUD: boolPtrGated(true),
			Access: entity.AccessControl{
				Read:   "", // open by declaration
				Create: "posts:write",
				Update: "posts:write",
				Delete: "posts:admin",
			},
		},
	})
	// What a generated app installs on every request, including anonymous ones.
	policy := access.NewRolePolicy()
	policy.Grant("admin", access.Wildcard)
	app.Use(access.Middleware(policy, func(ctx context.Context) []string {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			if rh, ok := u.(interface{ GetRoles() []string }); ok {
				return rh.GetRoles()
			}
		}
		return nil
	}))
	return app
}

func boolPtrGated(b bool) *bool { return &b }

func TestApp_OpenReadGatedWrite_AnonymousWritesRefused(t *testing.T) {
	app := openReadGatedWriteApp(t)
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	// The read half must stay open, that is the reason to declare a blank Read.
	if resp := ta.Get("/api/posts"); resp.Status() != http.StatusOK {
		t.Fatalf("anonymous GET /api/posts = %d, want 200 (blank Read is open): %s", resp.Status(), resp.Body())
	}

	// Every write names a permission an anonymous caller cannot hold.
	create := ta.Post("/api/posts", map[string]any{"title": "anon"})
	if create.Status() != http.StatusForbidden {
		t.Errorf("anonymous POST /api/posts = %d, want 403 — Access.Create \"posts:write\" is declared and must be enforced. body=%s",
			create.Status(), create.Body())
	}

	// Whatever the write status was, nothing may have been persisted.
	list := ta.Get("/api/posts")
	if got := list.Body(); strings.Contains(got, "anon") {
		t.Errorf("a refused anonymous create reached the database — list now contains it:\n%s", got)
	}
}
