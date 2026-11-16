package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
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
// dialect once, whichever dialect-dependent operations run on it.
func TestWrappedDBHandlerProbesDialectOnce(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, views INTEGER, price TEXT, seen_at TEXT, doc TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("posts", entity.EntityConfig{Table: "posts", Unmanaged: true, Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "views", Type: schema.Int},
		{Name: "price", Type: schema.Decimal},
		{Name: "seen_at", Type: schema.Timestamp},
		{Name: "doc", Type: schema.File},
	}}.WithTimestamps(false))
	exec := &probeCountingExecutor{db: db}
	ch := NewCrudHandler(ent, exec)
	ch.Storage = teststorage.New(t, t.TempDir())
	if ch.dialectHandle() != nil {
		t.Fatal("setup: the wrapped handler has a *sql.DB to remember under")
	}
	ctx := context.Background()
	sorted := ListOptions{Sorts: []filter.ParsedSort{{Field: "views", Desc: true}, {Field: "price"}}}
	typed := func() *TypedQuery[map[string]any] { return NewTypedQuery[map[string]any](ch) }
	var id string
	ops := []struct {
		name string
		run  func() error
	}{
		{"sorted ListAll", func() error { _, err := ch.ListAll(ctx, sorted); return err }},
		{"timestamp filter", func() error {
			_, err := ch.ListAll(ctx, ListOptions{Filters: []filter.ParsedFilter{{Field: "seen_at", Op: filter.OpGt, Value: "2026-01-01T00:00:00Z"}}})
			return err
		}},
		{"GroupCountAll", func() error { _, err := ch.GroupCountAll(ctx, "views", ListOptions{}, 0); return err }},
		{"SumAll decimal", func() error { _, err := ch.SumAll(ctx, "price", ListOptions{}); return err }},
		{"TypedQuery.Order", func() error { _, err := typed().Order(entity.NewIntColumn("views").Desc()).Find(ctx); return err }},
		{"create", func() error {
			row, err := ch.CreateOne(ctx, map[string]any{"id": "a", "title": "t", "price": "1.50"})
			id, _ = row["id"].(string)
			return err
		}},
		{"decimal refusal", func() error {
			if _, err := ch.CreateOne(ctx, map[string]any{"id": "b", "title": "t", "price": "123456789012345.1234"}); err == nil {
				return errors.New("SQLite stored a decimal it would round")
			}
			return nil
		}},
		{"update by key", func() error { _, err := ch.UpdateOne(ctx, id, map[string]any{"title": "u"}); return err }},
		{"media UpdateAll", func() error { _, err := typed().UpdateAll(ctx, map[string]any{"doc": nil}); return err }},
		{"delete by key", func() error { return ch.DeleteOne(ctx, id) }},
	}
	for _, op := range ops {
		before := exec.probes
		if err := op.run(); err != nil {
			t.Fatalf("%s: %v", op.name, err)
		}
		t.Logf("%s: %d probes", op.name, exec.probes-before)
	}
	if exec.probes != 1 {
		t.Errorf("%d dialect probes over %d operations on one handler, want 1", exec.probes, len(ops))
	}
	before := exec.n
	if _, err := ch.ListAll(ctx, sorted); err != nil {
		t.Fatal(err)
	}
	sortedN := exec.n - before
	if _, err := ch.ListAll(ctx, ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if plain := exec.n - before - sortedN; sortedN != plain {
		t.Errorf("a sorted ListAll ran %d statements, an unsorted one %d", sortedN, plain)
	}
}

// A handler built as a literal over a DB with no *sql.DB behind it has
// nowhere to remember its dialect, so it is refused, not re-asked.
func TestLiteralHandlerOnWrappedDBRefusesDialectCheck(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := &probeCountingExecutor{db: db}
	built := NewCrudHandler(entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}), exec)
	if built.forEntity(built.Entity).onPostgres(context.Background()) || built.onPostgres(context.Background()) || exec.probes != 1 {
		t.Errorf("a handler and its forEntity sibling made %d probes, want 1 shared", exec.probes)
	}
	defer func() {
		if recover() == nil {
			t.Error("a literal handler on a wrapped DB answered a dialect check")
		}
		if exec.probes != 1 {
			t.Errorf("the refused handler still probed: %d probes", exec.probes)
		}
	}()
	(&CrudHandler{DB: exec}).onPostgres(context.Background())
}

// isPostgres has one caller, onPostgres, so a handler learns its dialect one way.
func TestHandlerLearnsDialectOnlyThroughOnPostgres(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(line, "func ") && (strings.Contains(line, "isPostgres(") || strings.Contains(line, "probeDialect(")) {
				calls = append(calls, fmt.Sprintf("%s:%d", f, i+1))
			}
		}
	}
	if len(calls) != 3 {
		t.Errorf("isPostgres and probeDialect are called at %v; want onPostgres's two calls and isPostgres's one, all in key.go", calls)
	}
	for _, c := range calls {
		if !strings.HasPrefix(c, "key.go:") {
			t.Errorf("%s asks for the dialect outside onPostgres", c)
		}
	}
}
