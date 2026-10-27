package crud

// Schema-qualified tables (`app.tags`) on live Postgres. query.SafeIdent
// accepts a dotted table, so an entity may live outside the search path;
// every SQL fragment that names such a table as a column qualifier has to
// accept the dot too, or the relation it serves silently matches nothing.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// pgQualifiedWorld builds posts, owner-scoped tags and their pivot in a
// schema that is NOT on the connection's search path, so a fragment that
// drops the qualifier fails instead of resolving to the same table.
func pgQualifiedWorld(t *testing.T) *CrudHandler {
	t.Helper()
	db, s := pgOffPathSchema(t,
		`CREATE TABLE %[1]s.qposts (id TEXT PRIMARY KEY, title TEXT, parent_id TEXT)`,
		`CREATE TABLE %[1]s.qtags (id TEXT PRIMARY KEY, name TEXT, owner_id TEXT)`,
		`CREATE TABLE %[1]s.qpost_tags (post_id TEXT, tag_id TEXT)`,
	)
	posts, tags, pivot := query.MustIdent(s+".qposts"), query.MustIdent(s+".qtags"), query.MustIdent(s+".qpost_tags")
	pgSeed(t, db, posts, []map[string]any{
		{"id": "p1", "title": "root"},
		{"id": "p2", "title": "child", "parent_id": "p1"},
	})
	pgSeed(t, db, tags, []map[string]any{
		{"id": "t1", "name": "go", "owner_id": "u1"},
		{"id": "t2", "name": "db", "owner_id": "u1"},
	})
	pgSeed(t, db, pivot, []map[string]any{
		{"post_id": "p1", "tag_id": "t1"},
		{"post_id": "p1", "tag_id": "t2"},
		{"post_id": "p2", "tag_id": "t2"},
	})
	return qualifiedHandler(t, db, posts, tags, pivot)
}

// pgOffPathSchema creates a schema beside the test's own, off the search
// path, runs each DDL with %[1]s set to its name, and returns both.
func pgOffPathSchema(t *testing.T, ddl ...string) (*sql.DB, string) {
	t.Helper()
	db := pgtest.DB(t)
	var cur string
	if err := db.QueryRow("SELECT current_schema()").Scan(&cur); err != nil {
		t.Fatalf("current_schema: %v", err)
	}
	s := cur + "_q"
	if _, err := db.Exec("CREATE SCHEMA " + s); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + s + " CASCADE") })
	for _, stmt := range ddl {
		if _, err := db.Exec(fmt.Sprintf(stmt, s)); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	return db, s
}

func qualifiedHandler(t *testing.T, db *sql.DB, posts, tags, pivot string) *CrudHandler {
	t.Helper()
	tagsEnt := entity.Define("tags", makeEntityConfig("tags", tags, "owner_id",
		[]schema.Field{{Name: "name", Type: schema.String}, {Name: "owner_id", Type: schema.String}}))
	tagsEnt.SetDB(db)
	postsEnt := entity.Define("posts", makeEntityConfig("posts", posts, "",
		[]schema.Field{{Name: "title", Type: schema.String}, {Name: "parent_id", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.ManyToMany("tags", "tags", pivot, "post_id", "tag_id"),
				entity.BelongsTo("parent", "posts", "parent_id"),
			}
		}))
	postsEnt.SetDB(db)
	installSecurityOwnerExtractor(t)
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"tags": tagsEnt, "posts": postsEnt}}
	return ch
}

func qualifiedList(t *testing.T, ch *CrudHandler, path string) ListResponse {
	t.Helper()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: path, UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", path, rr.Code, rr.Body.String())
	}
	return decodeListResponse(t, rr.Body.String())
}

func tagNamesOf(row map[string]any) []string {
	list, _ := row["tags"].([]any)
	var out []string
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, fmt.Sprint(m["name"]))
		}
	}
	slices.Sort(out)
	return out
}

func rowByID(resp ListResponse, id string) map[string]any {
	for _, row := range resp.Data {
		if row["id"] == id {
			return row
		}
	}
	return nil
}

func TestPGQualifiedScopedM2MInclude(t *testing.T) {
	ch := pgQualifiedWorld(t)
	resp := qualifiedList(t, ch, "/posts?include=tags(name=go)")
	if got := tagNamesOf(rowByID(resp, "p1")); !slices.Equal(got, []string{"go"}) {
		t.Fatalf("p1 tags(name=go) = %v, want [go]", got)
	}
}

func TestPGQualifiedOwnerScopedM2MInclude(t *testing.T) {
	ch := pgQualifiedWorld(t)
	resp := qualifiedList(t, ch, "/posts?include=tags")
	if got := tagNamesOf(rowByID(resp, "p1")); !slices.Equal(got, []string{"db", "go"}) {
		t.Fatalf("p1 tags = %v, want [db go]", got)
	}
}

func TestPGQualifiedNestedM2MFilter(t *testing.T) {
	ch := pgQualifiedWorld(t)
	resp := qualifiedList(t, ch, "/posts?tags.name=go")
	if len(resp.Data) != 1 || resp.Data[0]["id"] != "p1" {
		t.Fatalf("?tags.name=go = %v, want [p1]", resp.Data)
	}
}

func TestPGQualifiedSelfJoinFilter(t *testing.T) {
	ch := pgQualifiedWorld(t)
	resp := qualifiedList(t, ch, "/posts?parent.title=root")
	if len(resp.Data) != 1 || resp.Data[0]["id"] != "p2" {
		t.Fatalf("?parent.title=root = %v, want [p2]", resp.Data)
	}
}

// Accepting a dot must not accept anything else: every sink that takes a
// table as a qualifier still refuses one carrying SQL.
func TestTableQualifierRefusesInjection(t *testing.T) {
	const bad = "tags; DROP TABLE users"
	f := []filter.ParsedFilter{{Field: "name", Op: filter.OpEq, Value: "go"}}
	if got, _ := renderFilterClause(f, "app.tags", 1); got != " AND app.tags.name = $1" {
		t.Errorf("qualified filter clause = %q", got)
	}
	if got, _ := renderFilterClause(f, bad, 1); got != " AND 1 = 0" {
		t.Errorf("filter clause on %q = %q", bad, got)
	}
	if got, _ := renderReadScope(f, bad, 1); got != "(1 = 0)" {
		t.Errorf("read scope on %q = %q", bad, got)
	}
	nf := nestedFilter{
		Relation: entity.Relation{Type: entity.RelHasMany, Entity: "comments", ForeignKey: []string{"post_id"}},
		Field:    "body", Op: filter.OpEq, Value: "x", table: bad,
	}
	if got, _ := buildExistsSubquery("posts", []string{"id"}, nf); got != "1 = 0" {
		t.Errorf("subquery on relation table %q = %q", bad, got)
	}
	nf.table = "comments"
	if got, _ := buildExistsSubquery(bad, []string{"id"}, nf); got != "1 = 0" {
		t.Errorf("subquery on parent table %q = %q", bad, got)
	}
}

// Postgres names a constraint on app.inv without the schema (inv_email_key,
// inv_customer_id_fkey) and reports the relation as inv, so each refusal on a
// qualified table still names the field the caller sent.
func TestPGQualifiedConflictFields(t *testing.T) {
	db, s := pgOffPathSchema(t,
		`CREATE TABLE %[1]s.qcust (id TEXT PRIMARY KEY)`,
		`CREATE TABLE %[1]s.inv (id TEXT PRIMARY KEY, email TEXT UNIQUE, title TEXT NOT NULL,
			customer_id TEXT REFERENCES %[1]s.qcust (id))`,
	)
	pgSeed(t, db, query.MustIdent(s+".qcust"), []map[string]any{{"id": "c1"}})
	ent := entity.Define("inv", entity.EntityConfig{
		Name: "inv", Table: s + ".inv",
		Fields: []schema.Field{
			{Name: "email", Type: schema.String, Unique: true},
			{Name: "title", Type: schema.String},
			{Name: "customer_id", Type: schema.Relation, To: "qcust"},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for _, c := range []struct {
		body   string
		code   int
		fields map[string]any
	}{
		{`{"email":"a@b","title":"t","customer_id":"c1"}`, http.StatusCreated, nil},
		{`{"email":"a@b","title":"t","customer_id":"c1"}`, http.StatusConflict,
			map[string]any{"email": []any{"is already in use"}}},
		{`{"email":"x@y","title":"t","customer_id":"ghost"}`, http.StatusConflict,
			map[string]any{"customer_id": []any{"refers to a record that does not exist"}}},
		{`{"email":"z@y","customer_id":"c1"}`, http.StatusBadRequest,
			map[string]any{"title": []any{"is required"}}},
	} {
		req := withTestUser(httptest.NewRequest("POST", "/inv", strings.NewReader(c.body)), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		var env map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		got, _ := env["fields"].(map[string]any)
		if rec.Code != c.code || (c.fields != nil && !reflect.DeepEqual(got, c.fields)) {
			t.Errorf("POST %s = %d %s, want %d with %v", c.body, rec.Code, rec.Body.String(), c.code, c.fields)
		}
	}
}

// The drivers name app.inv's constraints and relation without the schema.
func TestQualifiedConflictColumns(t *testing.T) {
	ent := entity.Define("inv", entity.EntityConfig{
		Name: "inv", Table: "app.inv",
		Fields: []schema.Field{
			{Name: "email", Type: schema.String, Unique: true},
			{Name: "customer_id", Type: schema.Relation, To: "customers"},
		},
	}.WithTimestamps(false))
	for msg, want := range map[string][]string{
		"UNIQUE constraint failed: inv.email":                                                        {"email"},
		`pq: duplicate key value violates unique constraint "inv_email_key"`:                         {"email"},
		"Error 1062: Duplicate entry 'a@b' for key 'inv.email'":                                      {"email"},
		`pq: insert or update on table "inv" violates foreign key constraint "inv_customer_id_fkey"`: {"customer_id"},
		"Error 1452: Cannot add or update a child row: a foreign key constraint fails (`app`.`inv`, CONSTRAINT `inv_ibfk_1` FOREIGN KEY (`customer_id`) REFERENCES `customers` (`id`))": {"customer_id"},
	} {
		if got := conflictColumns(ent, msg); !reflect.DeepEqual(got, want) {
			t.Errorf("%s\n got %v, want %v", msg, got, want)
		}
	}
}

// Postgres names the proposed row EXCLUDED, so a target table called excluded
// made every reference to it ambiguous. The plain arm writes EXCLUDED in SET;
// the soft-delete arm also qualifies its guard with the table.
func TestUpsertTableNamedExcluded(t *testing.T) {
	const ddl = `CREATE TABLE excluded (id TEXT PRIMARY KEY, name TEXT, deleted_at TIMESTAMP)`
	for _, dialect := range []string{"sqlite", "postgres"} {
		for _, softDelete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/softdelete=%v", dialect, softDelete), func(t *testing.T) {
				var db *sql.DB
				if dialect == "postgres" {
					db = pgtest.DB(t)
					if _, err := db.Exec(ddl); err != nil {
						t.Fatalf("ddl: %v", err)
					}
				} else {
					db = setupDB(t, ddl)
				}
				fields := []schema.Field{{Name: "name", Type: schema.String}}
				if softDelete {
					fields = append(fields, schema.Field{Name: "deleted_at", Type: schema.Timestamp})
				}
				ent := entity.Define("excluded", entity.EntityConfig{
					Name: "excluded", Table: "excluded", Fields: fields,
					Scope: &entity.ScopeConfig{SoftDelete: softDelete},
				}.WithTimestamps(false))
				ent.SetDB(db)
				ch := NewCrudHandler(ent, db)
				ctx := context.Background()
				if _, err := ch.UpsertOne(ctx, map[string]any{"id": "k1", "name": "first"}); err != nil {
					t.Fatalf("upsert insert: %v", err)
				}
				res, err := ch.UpsertOne(ctx, map[string]any{"id": "k1", "name": "second"})
				if err != nil {
					t.Fatalf("upsert update: %v", err)
				}
				if got := fmt.Sprint(res["name"]); got != "second" {
					t.Errorf("upsert returned name=%q, want second", got)
				}
			})
		}
	}
}
