package framework

import (
	"database/sql"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// pivotGateApp registers the join table of posts⇄tags as its own entity
// with the given exposure.
func pivotGateApp(t *testing.T, pivotExposure *entity.ExposureConfig) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	app := NewApp(WithConfig(AppConfig{Name: "pivotgate", APIPrefix: "/api"}), WithDB(db))
	app.Entity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id").WithCascadeWrite(true),
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("tags", EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("post_tags", EntityConfig{
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.Relation, To: "posts"},
			{Name: "tag_id", Type: schema.Relation, To: "tags"},
		},
		Exposure: pivotExposure,
	})
	return app
}

// A cascade link writes the pivot through the pivot's own gates: a
// caller its create route refuses cannot add the row through posts.
func TestPivotLinkHonoursPivotGates(t *testing.T) {
	for name, exp := range map[string]*entity.ExposureConfig{
		"access":  {CRUD: new(true), Public: true, Access: entity.AccessControl{Create: "post_tags:write"}},
		"no crud": {CRUD: new(false)},
	} {
		t.Run(name, func(t *testing.T) {
			app := pivotGateApp(t, exp)
			stop := covStartAndStop(t, app)
			defer stop()
			if _, err := app.DB.Exec(`INSERT INTO tags (id, name) VALUES ('t1','go')`); err != nil {
				t.Fatal(err)
			}
			ta := TestHarness(t, app)
			if got := ta.Post("/api/posts", map[string]any{"title": "x", "tags": []any{"t1"}}).Status(); got < http.StatusBadRequest {
				t.Errorf("a cascade link the pivot's gate refuses answered %d", got)
			}
			var n int
			if err := app.DB.QueryRow(`SELECT COUNT(*) FROM post_tags`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("the cascade wrote %d pivot rows past the pivot's gate", n)
			}
		})
	}
}

// A multi-tenant pivot row written by the cascade carries the caller's
// tenant, so ?include= reads the link back.
func TestPivotLinkStampsTenant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		scope := &entity.ScopeConfig{MultiTenant: true, TenantField: "tenant_id"}
		app.Entity("tags_mt", entity.EntityConfig{
			Table: "tags_mt", Scope: scope,
			Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}, {Name: "tenant_id", Type: schema.String}},
		}.WithTimestamps(false))
		app.Entity("posts_mt", entity.EntityConfig{
			Table: "posts_mt", Scope: scope,
			Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "tenant_id", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags_mt", "post_tags_mt", "post_id", "tag_id").WithCascadeWrite(true)},
		}.WithTimestamps(false))
		app.Entity("post_tags_mt", entity.EntityConfig{
			Table: "post_tags_mt", Scope: scope, PrimaryKey: []string{"post_id", "tag_id"},
			Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "tag_id", Type: schema.String}, {Name: "tenant_id", Type: schema.String}},
		}.WithTimestamps(false))
		if _, err := db.Exec(`
			CREATE TABLE posts_mt (id TEXT PRIMARY KEY, title TEXT, tenant_id TEXT);
			CREATE TABLE tags_mt (id TEXT PRIMARY KEY, name TEXT, tenant_id TEXT);
			CREATE TABLE post_tags_mt (post_id TEXT NOT NULL, tag_id TEXT NOT NULL, tenant_id TEXT, PRIMARY KEY (post_id, tag_id));
		`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO tags_mt (id, name, tenant_id) VALUES ('t1', 'go', 'tenant-a')`); err != nil {
			t.Fatal(err)
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"}).AsTenant("tenant-a")
		ta.Post("/posts_mt", map[string]any{"id": "p1", "title": "hello", "tags": []any{"t1"}}).AssertStatus(t, http.StatusCreated)
		var tenantID sql.NullString
		if err := db.QueryRow(`SELECT tenant_id FROM post_tags_mt WHERE post_id = 'p1'`).Scan(&tenantID); err != nil {
			t.Fatal(err)
		}
		if tenantID.String != "tenant-a" {
			t.Fatalf("the pivot row's tenant = %q, want tenant-a", tenantID.String)
		}
	})
}

// With the gate open the link still lands: the gate refuses the
// caller, not the relation.
func TestPivotLinkPassesOpenPivot(t *testing.T) {
	app := pivotGateApp(t, &entity.ExposureConfig{CRUD: new(true), Public: true})
	stop := covStartAndStop(t, app)
	defer stop()
	if _, err := app.DB.Exec(`INSERT INTO tags (id, name) VALUES ('t1','go')`); err != nil {
		t.Fatal(err)
	}
	if got := TestHarness(t, app).Post("/api/posts", map[string]any{"title": "x", "tags": []any{"t1"}}).Status(); got != http.StatusCreated {
		t.Fatalf("POST /api/posts with an open pivot answered %d, want 201", got)
	}
	var n int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM post_tags WHERE tag_id = 't1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the link wrote %d pivot rows, want 1", n)
	}
}
