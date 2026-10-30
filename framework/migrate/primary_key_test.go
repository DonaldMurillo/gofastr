package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A replica that read the key before a peer changed it re-reads the key
// under the lock and, finding it already declared, changes nothing: the
// stale read alone would refuse the populated table.
func TestConvergeKeyRereadsLiveKey(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	for _, q := range []string{
		"CREATE TABLE enrollments (student_id TEXT NOT NULL, course_id TEXT NOT NULL, PRIMARY KEY (student_id, course_id))",
		"INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	ent := entity.Define("enrollment", entity.EntityConfig{
		Table: "enrollments",
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"student_id", "course_id"},
	}.WithTimestamps(false))
	stale := liveKey{cols: []string{"id"}}
	if err := convergeKey(ctx, db, ent, map[string]*entity.Entity{"enrollment": ent}, DialectSQLite, stale, nil); err != nil {
		t.Fatalf("convergeKey with a stale pre-lock key: %v", err)
	}
}

// Two first boots of different versions both read the table as absent
// before the lock; the older one creates it. The newer one reaches its
// migrateEntity with that stale read, so CREATE TABLE IF NOT EXISTS was a
// no-op and its new column never landed (2026-10-27 M9). Under the lock a
// table read as absent is read again.
func TestMigrateEntityRereadsAbsentTable(t *testing.T) {
	db := openSQLiteForMigrateTest(t)
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "CREATE TABLE widgets (id TEXT NOT NULL PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("widget", entity.EntityConfig{
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
			{Name: "sku", Type: schema.String, Required: true},
		},
		// The newer version also keys the table differently, so the
		// re-read has to carry the live key into convergeKey too.
		PrimaryKey: []string{"sku"},
	}.WithTimestamps(false))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var deferred []SchemaChange
	// live and lk are the pre-lock read: no table.
	if err := migrateEntity(ctx, tx, ent, map[string]*entity.Entity{"widget": ent}, DialectSQLite, nil, nil, &deferred); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO widgets (id, title, sku) VALUES ('a', 't', 's')"); err != nil {
		t.Fatalf("the newer version's column is missing: %v", err)
	}
	var pk string
	if err := db.QueryRowContext(ctx, "SELECT name FROM pragma_table_info('widgets') WHERE pk > 0").Scan(&pk); err != nil || pk != "sku" {
		t.Fatalf("key after migrate = %q (%v), want sku", pk, err)
	}

	// A failed re-read stops the migration instead of reading as absent:
	// the first query re-reads the columns, the second the key.
	for _, nth := range []int{1, 2} {
		exec := &failExec{DB: db, failQuery: nth}
		if err := migrateEntity(ctx, exec, ent, map[string]*entity.Entity{"widget": ent}, DialectSQLite, nil, nil, &deferred); err == nil || !strings.Contains(err.Error(), "injected query failure") {
			t.Fatalf("query %d failing: err %v, want it reported", nth, err)
		}
	}
}

// failExec fails each statement containing one of match and counts them,
// and fails query number failQuery (from 1) when it is set.
type failExec struct {
	*sql.DB
	match     []string
	hits      int
	failQuery int
	queries   int
}

func (f *failExec) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if f.queries++; f.queries == f.failQuery {
		return nil, errors.New("injected query failure")
	}
	return f.DB.QueryContext(ctx, q, args...)
}

func (f *failExec) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	for _, m := range f.match {
		if strings.Contains(q, m) {
			f.hits++
			return nil, errors.New("injected failure: " + m)
		}
	}
	return f.DB.ExecContext(ctx, q, args...)
}

// Boot's SQLite key rebuild turns legacy_alter_table back off when the
// rebuild fails and still reports the rebuild's error, and it reports a
// failed restore when the rebuild worked. When both fail, the rebuild's
// error is the one reported (2026-10-27 M4).
func TestConvergeKeyRestoresLegacyAlter(t *testing.T) {
	ent := entity.Define("widget", entity.EntityConfig{
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "code", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"code"},
	}.WithTimestamps(false))
	for _, tc := range []struct {
		name  string
		match []string
		want  string
	}{
		{"rebuild fails", []string{"DROP TABLE"}, "injected failure: DROP TABLE"},
		{"restore fails", []string{"legacy_alter_table=OFF"}, "restore legacy_alter_table"},
		{"both fail", []string{"DROP TABLE", "legacy_alter_table=OFF"}, "injected failure: DROP TABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openSQLiteForMigrateTest(t)
			db.SetMaxOpenConns(1)
			ctx := context.Background()
			if _, err := db.ExecContext(ctx, "CREATE TABLE widgets (id TEXT NOT NULL PRIMARY KEY, code TEXT NOT NULL)"); err != nil {
				t.Fatal(err)
			}
			exec := &failExec{DB: db, match: tc.match}
			err := convergeKey(ctx, exec, ent, map[string]*entity.Entity{"widget": ent}, DialectSQLite, liveKey{cols: []string{"id"}}, nil)
			if exec.hits != len(tc.match) {
				t.Fatalf("%d statements matched %q, want %d", exec.hits, tc.match, len(tc.match))
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("convergeKey: %v, want an error containing %q", err, tc.want)
			}
			var legacy int
			if err := db.QueryRowContext(ctx, "PRAGMA legacy_alter_table").Scan(&legacy); err != nil {
				t.Fatal(err)
			}
			if tc.name == "rebuild fails" && legacy != 0 {
				t.Fatalf("legacy_alter_table = %d after a failed rebuild, want 0", legacy)
			}
		})
	}
}
