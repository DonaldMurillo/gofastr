package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestAutoMigrate_Atomic asserts the whole run is one transaction: if a later
// entity's DDL fails, the earlier entities' tables are rolled back too rather
// than leaving a half-migrated schema. We force a failure by declaring an
// entity whose index references a non-existent column, which the engine
// rejects when CREATE INDEX runs.
func TestAutoMigrate_Atomic(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		// "aaa" is valid and (because topo order falls back to name order) is
		// migrated first, so it would commit if the run were not atomic.
		reg.Register(entity.Define("aaa", entity.EntityConfig{
			Table:  "aaa",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
		}.WithTimestamps(false)))
		// "zzz" carries a column whose RawType is syntactically invalid SQL
		// (unbalanced parens), so its generated CREATE TABLE is rejected by both
		// SQLite and Postgres. RawType is emitted verbatim, so this is a
		// deterministic, engine-agnostic mid-run failure that still passes
		// entity-level validation (which does not parse RawType) and therefore
		// reaches the migrate layer after "aaa" has already been created.
		reg.Register(entity.Define("zzz", entity.EntityConfig{
			Table: "zzz",
			Fields: []schema.Field{
				{Name: "bad", Type: schema.String, RawType: "TEXT))"},
			},
		}.WithTimestamps(false)))

		err := AutoMigrate(db, reg)
		if err == nil {
			t.Fatal("expected AutoMigrate to fail on the duplicate column")
		}

		// Atomicity: because the run is one tx, even the "aaa" table (migrated
		// before the failure) must have been rolled back. Neither table exists.
		for _, table := range []string{"aaa", "zzz"} {
			cols, lerr := migrate.ReadLiveColumns(context.Background(), db, table, migrate.DetectDialect(db))
			if lerr != nil {
				t.Fatalf("ReadLiveColumns(%s): %v", table, lerr)
			}
			if len(cols) != 0 {
				t.Errorf("table %q survived a failed AutoMigrate (not atomic): %v", table, keysOf(cols))
			}
		}
	})
}

// TestAutoMigrate_Cancellable asserts AutoMigrateContext honours a cancelled
// context instead of charging ahead — important so a shutdown mid-boot doesn't
// hang on a stuck migration.
func TestAutoMigrate_Cancellable(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	reg := NewRegistry()
	reg.Register(entity.Define("thing", entity.EntityConfig{
		Table:  "thing",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := AutoMigrateContext(ctx, db, reg)
	if err == nil {
		t.Fatal("expected AutoMigrateContext to fail on a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestAutoMigrate_AdvisoryLockExcludes proves the Postgres advisory lock
// actually serializes: while a competing session holds the migration lock,
// AutoMigrateContext blocks and (here) times out rather than running DDL
// concurrently. SQLite has no advisory lock, so this is Postgres-only.
func TestAutoMigrate_AdvisoryLockExcludes(t *testing.T) {
	db := openTestDB(t, DialectPostgres)
	// A SEPARATE db handle to the same Postgres database (advisory locks are
	// database-global, not schema-scoped). Using its own connection means the
	// concurrent AutoMigrate below blocks on the LOCK, not on pool contention.
	holderDB := openTestDB(t, DialectPostgres)

	holder, err := holderDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer holder.Close()
	if _, err := holder.ExecContext(context.Background(),
		"SELECT pg_advisory_lock($1)", coremig.AdvisoryLockKey); err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer holder.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", coremig.AdvisoryLockKey)

	reg := NewRegistry()
	reg.Register(entity.Define("locked_thing", entity.EntityConfig{
		Table:  "locked_thing",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false)))

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = AutoMigrateContext(ctx, db, reg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("AutoMigrate acquired the lock while another session held it — mutual exclusion broken")
	}
	// It must have actually waited on the lock (then bailed on ctx timeout),
	// not returned instantly. The poll interval is 200ms, so allow slack.
	if elapsed < 500*time.Millisecond {
		t.Fatalf("AutoMigrate returned in %v — it did not block on the held lock", elapsed)
	}

	// The table must NOT have been created (the run never got past the lock).
	cols, _ := migrate.ReadLiveColumns(context.Background(), db, "locked_thing", DialectPostgres)
	if len(cols) != 0 {
		t.Errorf("locked_thing was created despite the lock being held: %v", keysOf(cols))
	}
}
