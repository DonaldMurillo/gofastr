//go:build red

package main

// RED TEST — open finding, 2026-09-03 adversarial pass round 8 (tests-only; no fix applied).
// TIER: EXAMPLE-APP POSTURE — this pins examples/api-tour/main.go's own
// entity declarations, not framework code. The framework contract that
// Exposure{Public: true} grants anonymous writes is separately pinned
// (framework/crud/open_read_gated_write_security_test.go,
// framework/crud/default_auth_security_test.go) and the contracts analyzer
// already warns exactly this shape (framework/contracts/analyzers/entities.go
// RulePublicEntity); those pins are respected, not re-derived. What is red
// here is the example shipping the warned shape for per-user data.
// Property: per-user data must not be world-writable (CWE-862 missing
// authorization). The app's own source states the rule: the profiles
// declaration in this same file carries the comment "per-user data must
// never be world-writable" — users IS per-user data (accounts with avatar
// images), yet it is declared Exposure{Public: true} (main.go:60), the
// framework's full opt-out where "every operation, reads and writes, is open
// to anonymous callers" (framework/docs/content/security.md:550-554).
// Vulnerable path, exercised below against the app exactly as main.go
// declares it (Public users + LocalStorage tmpdir + FK pragma + demo seed):
//   - anonymous POST   /users    → creates an account row;
//   - anonymous PATCH  /users/u1 → rewrites the seeded Alice account;
//   - anonymous DELETE /users/u1 → deletes the seeded account.
// posts/comments are also Public but are demo CONTENT — filter-tagged, not
// counted; users is the account graph the profiles comment protects.
// Severity: MEDIUM — reference-example tier (a tour app users copy as the
// canonical entity wiring), account-graph tampering with no authentication
// at all; includes the avatar upload surface (Image field + LocalStorage).
// Fix direction: replace users' Public:true with the profiles shape —
// open-or-gated reads as the tour needs, Access{Create/Update/Delete:
// "users:write"...} so writes fail closed, matching the file's own stated
// rule for per-user data.

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// newTourApp reproduces main.go's wiring verbatim (DB + PRAGMA, LocalStorage
// upload dir, all four entities with their exact Exposure declarations, demo
// seed) minus the listener, so the tour's real route table is drivable
// in-process.
func newTourApp(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "api-tour.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}

	uploadDir := t.TempDir()
	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "api-tour"}),
		framework.WithFileStorage(upload.NewLocalStorage(uploadDir)),
	)

	// users — exactly main.go:58-70: Public:true, Image avatar, relations.
	app.Entity("users", framework.EntityConfig{Scope:
	// public demo content. See "Default CRUD authentication" in the security docs.
	&framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Public: true}, Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "name", Type: schema.String, Required: true},
			{Name: "avatar", Type: schema.Image},
		},
		Relations: []framework.Relation{
			framework.HasOne("profile", "profiles", "user_id"),
			framework.HasMany("posts", "posts", "author_id"),
		},
	})

	// profiles — exactly main.go:72-91, the file's stated rule.
	app.Entity("profiles", framework.EntityConfig{Table: "profiles",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "bio", Type: schema.Text},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("user", "users", "user_id"),
		}, Exposure: &framework.ExposureConfig{Access: framework.AccessControl{
			Create: "profiles:write",
			Update: "profiles:write",
			Delete: "profiles:write",
		}},
	})

	// posts/comments — exactly main.go:93-120 (demo content, filter-tagged).
	app.Entity("posts", framework.EntityConfig{Scope: &framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Public: true}, Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "users", "author_id"),
			framework.HasMany("comments", "comments", "post_id"),
		},
	})
	app.Entity("comments", framework.EntityConfig{Scope: &framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Public: true}, Table: "comments",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("post", "posts", "post_id"),
		},
	})

	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	seedDemoData(db)
	return app
}

// TestApiTourRedUsersNotWorldWritable drives the users route table with NO
// authentication context at all and asserts every write is refused. RED
// today: Public:true is the framework's anonymous-everything opt-out.
func TestApiTourRedUsersNotWorldWritable(t *testing.T) {
	app := newTourApp(t)
	do := func(method, path, body string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Anonymous create: an account row minted with no authentication. The
	// created id feeds the delete leg — the seeded users carry FK references
	// (profile, posts), and this red must fail on authorization, not on an
	// accidental FK guard.
	code, body := do(http.MethodPost, "/users", `{"name":"anonymous takeover"}`)
	if code < 400 {
		t.Errorf("anonymous POST /users = %d: the account graph is world-writable — Public:true on users grants anonymous create, while the same file gates profiles writes with the comment \"per-user data must never be world-writable\". Want 401/403", code)
	}
	freshID := ""
	if i := strings.Index(body, `"id":"`); i >= 0 {
		rest := body[i+6:]
		if j := strings.Index(rest, `"`); j >= 0 {
			freshID = rest[:j]
		}
	}

	// Anonymous update of the seeded Alice account.
	if code, _ = do(http.MethodPatch, "/users/u1", `{"name":"renamed by stranger"}`); code < 400 {
		t.Errorf("anonymous PATCH /users/u1 = %d: any caller rewrites any account. Want 401/403", code)
	}

	// Anonymous delete of the account this same test just created.
	if freshID != "" {
		if code, _ = do(http.MethodDelete, "/users/"+freshID, ""); code < 400 {
			t.Errorf("anonymous DELETE /users/%s = %d: any caller deletes any account. Want 401/403", freshID, code)
		}
	} else if code < 400 {
		t.Logf("created-row id not found in POST response (%.120s); delete leg skipped", body)
	}
}
