package migrate_test

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// counters holds an Int column and is keyed by an auto-increment Int.
func counters() *entity.Entity {
	return entity.Define("counters", entity.EntityConfig{
		Table: "counters",
		Fields: []schema.Field{
			{Name: "id", Type: schema.Int, AutoGenerate: schema.AutoIncrement},
			{Name: "hits", Type: schema.Int},
		},
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

// pgColumnType reads a column's type from the Postgres catalog.
func pgColumnType(t *testing.T, db *sql.DB, table, col string) string {
	t.Helper()
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`, table, col).Scan(&typ); err != nil {
		t.Fatalf("type of %s.%s: %v", table, col, err)
	}
	return typ
}

// Int validation accepts any int64, but Postgres stored Int as INTEGER
// (32-bit): 2147483648 passed validation and failed the insert there,
// while SQLite stored it (2026-10-27 D15).
func TestPGIntHoldsInt64(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	bootPlan(t, db, "1", counters())
	fkExec(t, db, "INSERT INTO counters (id, hits) VALUES (2147483648, 9223372036854775807)")
	fkExec(t, db, "INSERT INTO counters (hits) VALUES (1)")
	for _, col := range []string{"id", "hits"} {
		if got := pgColumnType(t, db, "counters", col); got != "bigint" {
			t.Errorf("counters.%s is %s, want bigint", col, got)
		}
	}
	var seq string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.sequences
		WHERE sequence_schema = current_schema() AND sequence_name = 'counters_id_seq'`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	if seq != "bigint" {
		t.Errorf("the key's sequence is %s, want bigint", seq)
	}
}

// A table made by an older build keeps its INTEGER columns: the diff
// takes them as in sync rather than proposing a retype of every Int
// column in the app.
func TestPGLegacyIntegerNoDrift(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	fkExec(t, db, "CREATE TABLE counters (id SERIAL PRIMARY KEY, hits INTEGER)")
	changes, err := migrate.DiffSchema(context.Background(), db, fkReg{counters()})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		t.Errorf("drift on a legacy INTEGER table: %s", c.Summary)
	}
}

// A belongs_to onto an integer key was TEXT on SQLite, so a range filter
// and a sort on the foreign key compared text: "10" < "2" (2026-10-27
// D16). It takes the key's type on both dialects now.
func TestFKTakesIntegerKeyType(t *testing.T) {
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, d migrate.Dialect) {
		child := entity.Define("hits", entity.EntityConfig{
			Table: "hits",
			Fields: []schema.Field{
				{Name: "counter_id", Type: schema.Relation},
			},
			Relations: []entity.Relation{entity.BelongsTo("counter", "counters", "counter_id")},
		}.WithTimestamps(false))
		bootPlan(t, db, "1", counters(), child)
		fkExec(t, db, "INSERT INTO counters (id, hits) VALUES (2, 0), (10, 0)",
			"INSERT INTO hits (id, counter_id) VALUES ('a', 2), ('b', 10)")
		rows, err := db.Query("SELECT id FROM hits WHERE counter_id > $1 ORDER BY counter_id", 1)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got = append(got, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if want := []string{"a", "b"}; !slices.Equal(got, want) {
			t.Errorf("counter_id > 1 ordered by counter_id = %v, want %v", got, want)
		}
		changes, err := migrate.DiffSchema(context.Background(), db, fkReg{counters(), child})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range changes {
			t.Errorf("drift after boot: %s", c.Summary)
		}
	})
}

// A TEXT foreign key an older build made on SQLite diffs as a retype, and
// the rebuild that applies it stores the keys as integers.
func TestSQLiteLegacyTextFKRetypes(t *testing.T) {
	db := testdb.Open(t, migrate.DialectSQLite)
	child := entity.Define("hits", entity.EntityConfig{
		Table:     "hits",
		Fields:    []schema.Field{{Name: "counter_id", Type: schema.Relation}},
		Relations: []entity.Relation{entity.BelongsTo("counter", "counters", "counter_id")},
	}.WithTimestamps(false))
	fkExec(t, db,
		"CREATE TABLE counters (id INTEGER PRIMARY KEY, hits INTEGER)",
		"CREATE TABLE hits (id TEXT PRIMARY KEY, counter_id TEXT REFERENCES counters(id))",
		"INSERT INTO counters (id, hits) VALUES (2, 0), (10, 0)",
		"INSERT INTO hits (id, counter_id) VALUES ('a', 2), ('b', 10)")
	changes, err := migrate.DiffSchema(context.Background(), db, fkReg{counters(), child})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Destructive {
		t.Fatalf("changes = %+v, want one destructive retype of hits", changes)
	}
	fkApply(t, db, changes)
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM hits WHERE counter_id > 2").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d rows past counter 2 after the rebuild, want 1", n)
	}
}
