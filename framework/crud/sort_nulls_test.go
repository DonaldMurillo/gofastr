package crud

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A sort pins the null position only on SQLite and only for a column
// that can hold NULL, so Postgres SQL and never-null index scans stay bare.
func TestSortPinsNullsOnlyOnSQLiteNullableColumns(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "views", Type: schema.Int},
	}})
	pinned := func(db *sql.DB) map[string]bool {
		ch := &CrudHandler{Entity: ent, DB: db}
		out := map[string]bool{}
		sorts := []filter.ParsedSort{{Field: "views", Desc: true}, {Field: "title"}, {Field: "created_at"}}
		for _, s := range ch.sortTerms(context.Background(), sorts) {
			out[s.Field] = s.PinNulls
		}
		return out
	}
	open := func() *sql.DB {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	got := pinned(open())
	for field, want := range map[string]bool{"views": true, "title": false, "created_at": false, "id": false} {
		if pin, ok := got[field]; !ok || pin != want {
			t.Errorf("SQLite: %s pinned=%v (present=%v), want %v", field, pin, ok, want)
		}
	}
	pg := open()
	rememberDialect(pg, true)
	for field, pin := range pinned(pg) {
		if pin {
			t.Errorf("Postgres: %s is pinned, want a bare ORDER BY", field)
		}
	}
}

type probeCountingExecutor struct {
	db     *sql.DB
	n      int
	probes int
}

func (c *probeCountingExecutor) count(q string) {
	c.n++
	if strings.Contains(q, "version()") {
		c.probes++
	}
}

func (c *probeCountingExecutor) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.count(q)
	return c.db.QueryContext(ctx, q, args...)
}

func (c *probeCountingExecutor) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.count(q)
	return c.db.QueryRowContext(ctx, q, args...)
}

func (c *probeCountingExecutor) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.count(q)
	return c.db.ExecContext(ctx, q, args...)
}

// A handler whose DB is a wrapper with no *sql.DB behind it asks for the
// dialect once, so a sorted read costs no statement an unsorted one does not.
func TestSortedReadOnWrappedDBProbesDialectOnce(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, views INTEGER, rank INTEGER)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("posts", entity.EntityConfig{Table: "posts", Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "views", Type: schema.Int},
		{Name: "rank", Type: schema.Int},
	}}.WithTimestamps(false))
	exec := &probeCountingExecutor{db: db}
	ch := NewCrudHandler(ent, exec)
	if ch.dialectHandle() != nil {
		t.Fatal("setup: the wrapped handler has a *sql.DB to remember under")
	}
	ctx := context.Background()
	sorted := ListOptions{Sorts: []filter.ParsedSort{{Field: "views", Desc: true}, {Field: "rank"}}}
	read := func(opts ListOptions) int {
		before := exec.n
		if _, err := ch.ListAll(ctx, opts); err != nil {
			t.Fatal(err)
		}
		return exec.n - before
	}
	read(sorted)
	if plain, again := read(ListOptions{}), read(sorted); again != plain {
		t.Errorf("a sorted ListAll ran %d statements, an unsorted one %d", again, plain)
	}
	if _, err := ch.GroupCountAll(ctx, "views", ListOptions{}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTypedQuery[map[string]any](ch).Order(entity.NewIntColumn("views").Desc()).Find(ctx); err != nil {
		t.Fatal(err)
	}
	if exec.probes != 1 {
		t.Errorf("%d dialect probes over four sorted reads, want 1", exec.probes)
	}
}
