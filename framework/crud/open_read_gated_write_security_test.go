package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// "Open reads, gated writes" is the shape the framework itself tells people to
// adopt: `gofastr generate` warns that `public: true` grants anonymous DELETE
// and recommends "access: (a blank read: + a real create: permission)" instead.
// A blog's posts, a public feed, a docs site — all land here.
//
// The blank Read must not weaken its siblings. Read is ungated *by
// declaration*; Create/Update/Delete each name a real permission and an
// anonymous caller holds none of them, so every write must 403 while the read
// stays open.
func setupOpenReadGatedWriteHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{
			CRUD: boolPtrTest(true),
			Access: entity.AccessControl{
				Read:   "", // deliberately open
				Create: "posts:write",
				Update: "posts:write",
				Delete: "posts:admin",
			},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func boolPtrTest(b bool) *bool { return &b }

// The read half of the contract: anonymous List must succeed, because that is
// the entire point of declaring a blank Read.
func TestOpenReadGatedWrite_AnonymousListAllowed(t *testing.T) {
	ch, db := setupOpenReadGatedWriteHandler(t)
	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('p1','hello')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous List = %d, want 200 (blank Read means open): %s", rec.Code, rec.Body.String())
	}
}

// The write half — the one that regressed. An anonymous Create against an
// entity requiring "posts:write" must 403, not persist a row.
func TestOpenReadGatedWrite_AnonymousCreateDenied(t *testing.T) {
	ch, db := setupOpenReadGatedWriteHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{"title":"anon"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous Create = %d, want 403 — Access.Create %q was declared and must be enforced regardless of Read being open. body=%s",
			rec.Code, "posts:write", rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("anonymous Create persisted %d row(s) — a refused write must not reach the database", n)
	}
}

func TestOpenReadGatedWrite_AnonymousUpdateDenied(t *testing.T) {
	ch, db := setupOpenReadGatedWriteHandler(t)
	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('p1','original')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/posts/p1", strings.NewReader(`{"title":"overwritten"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous Update = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	var title string
	if err := db.QueryRow(`SELECT title FROM posts WHERE id='p1'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "original" {
		t.Errorf("anonymous Update rewrote the row to %q — a refused write must not reach the database", title)
	}
}

// Delete is the sharpest case: `public: true` granting anonymous DELETE is the
// exact hazard the generator's warning names when it recommends this shape.
func TestOpenReadGatedWrite_AnonymousDeleteDenied(t *testing.T) {
	ch, db := setupOpenReadGatedWriteHandler(t)
	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('p1','keep me')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/posts/p1", nil)
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous Delete = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts WHERE id='p1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("anonymous Delete removed the row — a refused delete must not reach the database")
	}
}

// The app-wired shape: access.Middleware installs a RolePolicy on every
// request, including anonymous ones. The unit cases above run with NO policy in
// context, where access.Can short-circuits to false — so they can pass while a
// real app, which always has a policy, behaves differently. This case pins the
// path a generated app actually takes.
func TestOpenReadGatedWrite_AnonymousCreateDeniedWithPolicyInstalled(t *testing.T) {
	ch, db := setupOpenReadGatedWriteHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{"title":"anon"}`))
	req.Header.Set("Content-Type", "application/json")
	// What access.Middleware(policy, resolver) installs for an anonymous
	// caller: a real policy, and no roles.
	policy := access.NewRolePolicy()
	policy.Grant("admin", access.Wildcard)
	ctx := access.WithPolicy(req.Context(), policy)
	ctx = access.WithRoles(ctx, nil)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("anonymous Create with a policy installed = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM posts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("anonymous Create persisted %d row(s)", n)
	}
}

// CanReadScoped is the predicate every non-CRUD surface (server-rendered
// screens, island fragments, reports) uses to decide whether the caller may see
// an entity's rows. It has to answer the WHOLE read posture, not just RBAC:
// gating on CanRead alone let a default-posture entity — the shape `gofastr
// init` and most blueprints produce — render every row to an anonymous caller
// while its JSON route answered 401.
func TestCanReadScoped_PostureMatrix(t *testing.T) {
	newHandler := func(t *testing.T, cfg entity.EntityConfig) *CrudHandler {
		t.Helper()
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Skip("sqlite3 driver not available")
		}
		t.Cleanup(func() { db.Close() })
		if _, err := db.Exec(`CREATE TABLE things (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT)`); err != nil {
			t.Fatal(err)
		}
		cfg.Fields = append(cfg.Fields, schema.Field{Name: "title", Type: schema.String})
		ent := entity.Define("things", cfg.WithTimestamps(false))
		ent.SetDB(db)
		return NewCrudHandler(ent, db)
	}

	t.Run("default posture refuses an anonymous caller", func(t *testing.T) {
		// No OwnerField, no Access, no Public: auto-CRUD requires a session for
		// every operation, so a screen must not render rows either.
		ch := newHandler(t, entity.EntityConfig{})
		if ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped allowed an anonymous read of a session-required entity")
		}
		if !ch.CanRead(context.Background()) {
			t.Error("CanRead is RBAC-only and should still pass here — that gap is exactly why CanReadScoped exists")
		}
	})

	t.Run("public entity allows an anonymous read", func(t *testing.T) {
		ch := newHandler(t, entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Public: true},
		})
		if !ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped refused a Public entity")
		}
	})

	t.Run("blank Read beside real write permissions allows an anonymous read", func(t *testing.T) {
		ch := newHandler(t, entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Access: entity.AccessControl{
				Read: "", Create: "things:write", Update: "things:write", Delete: "things:admin",
			}},
		})
		if !ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped refused an entity whose Read is deliberately open")
		}
	})

	t.Run("gated Read refuses without the permission and allows with it", func(t *testing.T) {
		ch := newHandler(t, entity.EntityConfig{
			Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "things:read"}},
		})
		if ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped allowed a read without the declared permission")
		}
		policy := access.NewRolePolicy()
		policy.Grant("member", "things:read")
		ctx := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"member"})
		if !ch.CanReadScoped(ctx) {
			t.Error("CanReadScoped refused a caller holding the declared permission")
		}
	})

	t.Run("owner-scoped entity refuses a caller with no owner in context", func(t *testing.T) {
		ch := newHandler(t, entity.EntityConfig{
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		})
		if ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped allowed an owner-scoped read with no owner in context")
		}
	})

	// The tenant arm the matrix stopped one row short of. Owner and tenant are
	// separate refusal reasons inside the same predicate, so covering only the
	// owner one leaves the tenant check free to disappear: a multi-tenant
	// entity's screen would render rows to a caller the REST route refuses,
	// and the whole suite would stay green.
	t.Run("multi-tenant entity refuses a caller with no tenant in context", func(t *testing.T) {
		// Public, so the baseline session gate cannot answer first — with a
		// default-posture fixture the anonymous refusal below would be the
		// session check, and the tenant rule would never be reached. That is
		// the trap this whole matrix exists to avoid.
		ch := newHandler(t, entity.EntityConfig{
			Scope:    &entity.ScopeConfig{MultiTenant: true},
			Exposure: &entity.ExposureConfig{Public: true},
		})
		if ch.CanReadScoped(context.Background()) {
			t.Error("CanReadScoped allowed a multi-tenant read with no tenant in context")
		}
		// And the allow side, or "always false" would satisfy the check above.
		if !ch.CanReadScoped(tenant.SetTenantID(context.Background(), "acme")) {
			t.Error("CanReadScoped refused a caller who does carry a tenant id")
		}
	})
}

// A resource-aware Decider can allow the listing and deny one row — that seam
// is the reason CanResource takes a Ref with an ID. CanReadScoped asks about
// the collection (empty ID), so a screen using it would render a record the
// read-one route refuses by id. CanReadRecordScoped closes that.
func TestCanReadRecordScoped_ConsultsThePerRecordDecider(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("projects", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "projects:read"}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)

	// Grants the coarse permission, then denies exactly one record.
	policy := access.NewRolePolicy()
	policy.Grant("member", "projects:read")
	ctx := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"member"})
	ctx = access.WithDecider(ctx, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "denied" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})

	if !ch.CanReadScoped(ctx) {
		t.Fatal("the collection read should be allowed — the decider abstains for the empty ref")
	}
	if !ch.CanReadRecordScoped(ctx, "allowed") {
		t.Error("CanReadRecordScoped denied a record the decider abstains on")
	}
	if ch.CanReadRecordScoped(ctx, "denied") {
		t.Error("CanReadRecordScoped allowed a record the decider denies — a detail screen would show a row GET /api/projects/denied refuses")
	}
}

// CanReadScoped must not be MORE permissive than the HTTP read route, because
// rendering surfaces trust it in place of that route. owner.AllowCrossOwner is
// exported, so a host can set it on a request context; the in-process
// requireOwnerContext honours it and the HTTP route's RequireOwner does not.
// Honouring it here would let a screen render rows GET /api/<entity> refuses.
func TestCanReadScoped_IgnoresCrossOwnerMarker(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE docs (id TEXT PRIMARY KEY, owner_id TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("docs", entity.EntityConfig{
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)

	// No owner in context, but the cross-owner marker set.
	ctx := owner.AllowCrossOwner(context.Background())
	if ch.CanReadScoped(ctx) {
		t.Error("CanReadScoped honoured owner.AllowCrossOwner — a screen would render rows the REST route refuses, since RequireOwner ignores that marker")
	}
	// The allow-side of owner scoping is covered by
	// TestCanReadScoped_PostureMatrix; this case exists only to pin that the
	// marker cannot widen the answer.
}
