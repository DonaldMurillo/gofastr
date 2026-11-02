package appspec

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Each name below passed ValidateAppSpec, generated an app that compiled
// and vetted clean, and then the app died at boot (auto-migrate: unsafe
// SQL identifier "first-name"), served another entity's or a battery's
// rows, or on Postgres silently shared one table between two entities.
// The validator now names them. An SQL keyword is not among them: see
// TestSQLKeywordsWorkAsNames.
func TestAppSpecSQLNamesRefused(t *testing.T) {
	long := strings.Repeat("a", 64)
	field := func(names ...string) []framework.FieldDeclaration {
		out := make([]framework.FieldDeclaration, len(names))
		for i, n := range names {
			out[i] = framework.FieldDeclaration{Name: n, Type: "string"}
		}
		return out
	}
	cases := []struct {
		name     string
		entities []framework.EntityDeclaration
		want     []string
	}{
		{"schema part with a hyphen", []framework.EntityDeclaration{{Name: "orders", Table: "my-app.orders", Fields: field("title")}}, []string{`table "my-app"`}},
		{"field with a hyphen or a space", []framework.EntityDeclaration{{Name: "people", Fields: field("first-name", "last name")}}, []string{`field "first-name"`, `field "last name"`, "letters, digits and underscores"}},
		{"field longer than Postgres keeps", []framework.EntityDeclaration{{Name: "posts", Fields: field(long)}}, []string{"64 bytes", "first 63 bytes"}},
		{"table longer than Postgres keeps", []framework.EntityDeclaration{{Name: long + "one", Fields: field("title")}, {Name: long + "two", Fields: field("title")}}, []string{long + "one", long + "two", "first 63 bytes"}},
		{"index name with a hyphen", []framework.EntityDeclaration{{Name: "posts", Fields: field("title"), Indices: []framework.Index{{Name: "by-title", Columns: []string{"title"}}}}}, []string{`index "by-title"`}},
		{"two entities on one table", []framework.EntityDeclaration{{Name: "posts", Table: "items", Fields: field("title")}, {Name: "items", Fields: field("title")}}, []string{`entities "posts" and "items" both use the table "items"`}},
		{"two entities on one folded table", []framework.EntityDeclaration{{Name: "posts", Table: "Items", Fields: field("title")}, {Name: "items", Fields: field("title")}}, []string{`both use the table "items"`}},
		{"the auth battery's accounts", []framework.EntityDeclaration{{Name: "auth_users", Fields: field("nickname")}}, []string{`"auth_users"`, "auth battery's accounts"}},
		{"the auth battery's sessions", []framework.EntityDeclaration{{Name: "sessions", Table: "auth_sessions", Fields: field("note")}}, []string{`"auth_sessions"`, "auth battery's sessions"}},
		// The engines' default schema names the same table as the bare
		// name: public.auth_users is the auth battery's accounts on
		// Postgres, main.items is entity items' table on SQLite.
		{"the auth accounts in the public schema", []framework.EntityDeclaration{{Name: "mine", Table: "public.auth_users", Fields: field("note")}}, []string{`"public.auth_users"`, "auth battery's accounts"}},
		{"the auth sessions in SQLite's main schema", []framework.EntityDeclaration{{Name: "mine", Table: "MAIN.auth_sessions", Fields: field("note")}}, []string{`"MAIN.auth_sessions"`, "auth battery's sessions"}},
		{"two entities on one table, one schema-qualified", []framework.EntityDeclaration{{Name: "items", Fields: field("title")}, {Name: "other", Table: "public.items", Fields: field("title")}}, []string{`entities "items" and "other" both use the table "public.items"`}},
		{"the admin audit log", []framework.EntityDeclaration{{Name: "audit_log", Fields: field("note")}}, []string{`"audit_log"`, "audit log"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateAppSpec(AppSpec{Entities: tc.entities})
			if err == nil {
				t.Fatal("ValidateAppSpec accepted it")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error does not mention %q:\n%v", w, err)
				}
			}
		})
	}
}

// The check refuses names SQL cannot carry, not keywords, Go words or
// common table names: a field named type, func, range, order or user, an
// entity named select or users, a table named group and an index named
// check migrate on both engines and stay legal.
func TestAppSpecSQLNamesAllowed(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		{Name: "users", Fields: []framework.FieldDeclaration{
			{Name: "type", Type: "string"}, {Name: "func", Type: "string"}, {Name: "range", Type: "string"},
			{Name: "key", Type: "string"}, {Name: "map", Type: "json"}, {Name: "len", Type: "int"},
			{Name: strings.Repeat("b", 63), Type: "string"},
		}},
		{Name: "sessions", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		{Name: "blog-posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		{Name: "select", Fields: []framework.FieldDeclaration{
			{Name: "order", Type: "int"}, {Name: "group", Type: "string"}, {Name: "user", Type: "string"},
			{Name: "desc", Type: "string"}, {Name: "default", Type: "string"}, {Name: "on", Type: "string"},
		}, Indices: []framework.Index{{Name: "check", Columns: []string{"group"}}}},
		{Name: "orders", Table: "group", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
	}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("ValidateAppSpec refused legal names: %v", err)
	}
}

// sqlKeywordNames are the words the app spec refused as a table, field or
// index name (owner decision D-07) while the CRUD statements named columns
// bare: every word SQLite or Postgres refuses as a bare name, found by
// running CREATE TABLE and SELECT … WHERE … ORDER BY with each SQLite
// keyword and each Postgres reserved, type/function and column-name
// keyword. AutoMigrate and CRUD now quote each (query.QuoteKeyword).
var sqlKeywordNames = strings.Fields(`
	add all alter analyse analyze and any array as asc asymmetric
	authorization autoincrement between binary both case cast check collate
	collation column commit concurrently constraint create cross
	current_catalog current_date current_role current_schema current_time
	current_timestamp current_user default deferrable delete desc distinct
	do drop else end escape except exists false fetch for foreign freeze
	from full grant group having ilike in index initially inner insert
	intersect into is isnull join lateral leading left like limit localtime
	localtimestamp natural not nothing notnull null offset on only or order
	outer overlaps placing primary raise references returning right select
	session_user set similar some symmetric system_user table tablesample
	then to trailing transaction true union unique update user using values
	variadic verbose when where window with if`)

// TestSQLKeywordsWorkAsNames proves the refusal is no longer needed: every
// keyword, as a table name and as a column name, migrates, takes a create
// and a list, and passes ValidateAppSpec, on SQLite and on Postgres. A
// word that breaks on either engine belongs back in the refusal.
func TestSQLKeywordsWorkAsNames(t *testing.T) {
	// works reports whether an entity named table with one column named
	// column migrates on db and then takes a create and a list: a word can
	// pass the DDL and still break every query (SQLite's raise).
	works := func(t *testing.T, db *sql.DB, table, column string) error {
		app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
		cfg, err := framework.EntityDeclaration{Name: table, Fields: []framework.FieldDeclaration{{Name: column, Type: "string"}}}.Config()
		if err != nil {
			t.Fatalf("config %s.%s: %v", table, column, err)
		}
		app.Entity(table, cfg)
		if err := framework.AutoMigrate(db, app.Registry); err != nil {
			return err
		}
		ent, err := app.Registry.Get(table)
		if err != nil {
			t.Fatal(err)
		}
		h := framework.NewCrudHandler(ent, db)
		ctx := context.Background()
		if _, err := h.CreateOne(ctx, map[string]any{column: "x"}); err != nil {
			return err
		}
		rows, err := h.ListAll(ctx, framework.ListOptions{
			Filters: []filter.ParsedFilter{{Field: column, Op: filter.OpEq, Value: "x"}},
			Sorts:   []filter.ParsedSort{{Field: column, Desc: true}},
		})
		if err != nil {
			return err
		}
		// The row is keyed by the column's JSON spelling (current_date
		// reads back as currentDate), so look for the value.
		if len(rows) != 1 || !slices.Contains(slices.Collect(maps.Values(rows[0])), any("x")) {
			return fmt.Errorf("list = %v, want the one row", rows)
		}
		return nil
	}
	probe := func(t *testing.T, db *sql.DB) {
		if err := works(t, db, "widgets", "label"); err != nil {
			t.Fatalf("a plain table and column do not work; the probe is broken: %v", err)
		}
		for _, w := range sqlKeywordNames {
			if err := works(t, db, w, "label"); err != nil {
				t.Errorf("table %q: %v", w, err)
			}
			if err := works(t, db, "t_"+w, w); err != nil {
				t.Errorf("column %q: %v", w, err)
			}
		}
	}
	for _, w := range sqlKeywordNames {
		bp := AppSpec{Entities: []framework.EntityDeclaration{{
			Name:    w,
			Fields:  []framework.FieldDeclaration{{Name: w, Type: "string"}},
			Indices: []framework.Index{{Name: w, Columns: []string{w}}},
		}}}
		if err := validateAppSpecSQLNames(bp); err != nil {
			t.Errorf("app spec refuses the keyword %q: %v", w, err)
		}
	}
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		defer db.Close()
		probe(t, db)
	})
	t.Run("postgres", func(t *testing.T) {
		probe(t, pgtest.DB(t))
	})
}
