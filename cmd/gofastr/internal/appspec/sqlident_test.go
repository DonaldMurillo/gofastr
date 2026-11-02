package appspec

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Each name below passed ValidateAppSpec, generated an app that compiled
// and vetted clean, and then the app died at boot (auto-migrate: near
// "order": syntax error; unsafe SQL identifier "first-name"), served
// another entity's or a battery's rows, or on Postgres silently shared
// one table between two entities. The validator now names them.
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
		{"reserved entity name", []framework.EntityDeclaration{{Name: "select", Fields: field("title")}}, []string{`entity "select" table "select"`, "reserved word"}},
		{"reserved declared table", []framework.EntityDeclaration{{Name: "orders", Table: "order", Fields: field("title")}}, []string{`table "order"`, "reserved word"}},
		{"reserved schema part", []framework.EntityDeclaration{{Name: "orders", Table: "table.orders", Fields: field("title")}}, []string{`table "table"`}},
		{"reserved field (SQLite and Postgres)", []framework.EntityDeclaration{{Name: "posts", Fields: field("order", "group", "on")}}, []string{`field "order"`, `field "group"`, `field "on"`}},
		{"reserved field (Postgres only)", []framework.EntityDeclaration{{Name: "posts", Fields: field("user", "desc")}}, []string{`field "user"`, `field "desc"`}},
		{"field with a hyphen or a space", []framework.EntityDeclaration{{Name: "people", Fields: field("first-name", "last name")}}, []string{`field "first-name"`, `field "last name"`, "letters, digits and underscores"}},
		{"field longer than Postgres keeps", []framework.EntityDeclaration{{Name: "posts", Fields: field(long)}}, []string{"64 bytes", "first 63 bytes"}},
		{"table longer than Postgres keeps", []framework.EntityDeclaration{{Name: long + "one", Fields: field("title")}, {Name: long + "two", Fields: field("title")}}, []string{long + "one", long + "two", "first 63 bytes"}},
		{"reserved index name", []framework.EntityDeclaration{{Name: "posts", Fields: field("title"), Indices: []framework.Index{{Name: "check", Columns: []string{"title"}}}}}, []string{`index "check"`}},
		{"index name with a hyphen", []framework.EntityDeclaration{{Name: "posts", Fields: field("title"), Indices: []framework.Index{{Name: "by-title", Columns: []string{"title"}}}}}, []string{`index "by-title"`}},
		{"two entities on one table", []framework.EntityDeclaration{{Name: "posts", Table: "items", Fields: field("title")}, {Name: "items", Fields: field("title")}}, []string{`entities "posts" and "items" both use the table "items"`}},
		{"two entities on one folded table", []framework.EntityDeclaration{{Name: "posts", Table: "Items", Fields: field("title")}, {Name: "items", Fields: field("title")}}, []string{`both use the table "items"`}},
		{"the auth battery's accounts", []framework.EntityDeclaration{{Name: "auth_users", Fields: field("nickname")}}, []string{`"auth_users"`, "auth battery's accounts"}},
		{"the auth battery's sessions", []framework.EntityDeclaration{{Name: "sessions", Table: "auth_sessions", Fields: field("note")}}, []string{`"auth_sessions"`, "auth battery's sessions"}},
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

// The check refuses SQL words, not Go words or common table names: a field
// named type, func or range, and entities named users or sessions, migrate
// on both engines and stay legal.
func TestAppSpecSQLNamesAllowed(t *testing.T) {
	bp := AppSpec{Entities: []framework.EntityDeclaration{
		{Name: "users", Fields: []framework.FieldDeclaration{
			{Name: "type", Type: "string"}, {Name: "func", Type: "string"}, {Name: "range", Type: "string"},
			{Name: "key", Type: "string"}, {Name: "map", Type: "json"}, {Name: "len", Type: "int"},
			{Name: strings.Repeat("b", 63), Type: "string"},
		}},
		{Name: "sessions", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
		{Name: "blog-posts", Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}}},
	}}
	if err := ValidateAppSpec(bp); err != nil {
		t.Fatalf("ValidateAppSpec refused legal names: %v", err)
	}
}

// TestSQLReservedWordsBreakAutoMigrate proves the list: every word in it,
// as a table name or a column name, fails AutoMigrate or the first create
// and list on SQLite or, with TEST_POSTGRES_DSN, on Postgres, while a
// plain name works on both. A word that works everywhere does not belong
// in the list.
func TestSQLReservedWordsBreakAutoMigrate(t *testing.T) {
	// works reports whether an entity named table with one column named
	// column migrates on db and then takes a create and a list: a word can
	// pass the DDL and still break every query (SQLite's raise).
	works := func(t *testing.T, db *sql.DB, table, column string) bool {
		app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
		cfg, err := framework.EntityDeclaration{Name: table, Fields: []framework.FieldDeclaration{{Name: column, Type: "string"}}}.Config()
		if err != nil {
			t.Fatalf("config %s.%s: %v", table, column, err)
		}
		app.Entity(table, cfg)
		if framework.AutoMigrate(db, app.Registry) != nil {
			return false
		}
		ent, err := app.Registry.Get(table)
		if err != nil {
			t.Fatal(err)
		}
		h := framework.NewCrudHandler(ent, db)
		ctx := context.Background()
		if _, err := h.CreateOne(ctx, map[string]any{column: "x"}); err != nil {
			return false
		}
		rows, err := h.ListAll(ctx, framework.ListOptions{})
		return err == nil && len(rows) == 1
	}
	broke := map[string]bool{}
	probe := func(t *testing.T, db *sql.DB) {
		if !works(t, db, "widgets", "label") {
			t.Fatal("a plain table and column do not work; the probe is broken")
		}
		for _, w := range sqlReservedWordList() {
			if !works(t, db, w, "label") || !works(t, db, "t_"+w, w) {
				broke[w] = true
			}
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
	pg := false
	t.Run("postgres", func(t *testing.T) {
		db := pgtest.DB(t)
		pg = true
		probe(t, db)
	})
	for _, w := range sqlReservedWordList() {
		if broke[w] {
			continue
		}
		if pg {
			t.Errorf("%q works as a table and as a column on SQLite and Postgres: it is not reserved", w)
		} else {
			t.Logf("%q works on SQLite; Postgres did not run", w)
		}
	}
}
