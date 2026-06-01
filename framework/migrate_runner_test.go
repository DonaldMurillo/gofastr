package framework

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
)

// These exercise the versioned core/migrate runner against a REAL database on
// both dialects (the package's own tests are SQLite + sqlmock only). They lock
// in that the advisory lock, checksums, dirty-state, and Force all behave on
// live Postgres as well as SQLite.

func newRunner(db *sql.DB, dialect Dialect) *coremig.Migrator {
	return coremig.New(db, coremig.WithDialect(dialect))
}

// TestRunner_UpDownStatusCycle is the end-to-end happy path on both engines.
func TestRunner_UpDownStatusCycle(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ctx := context.Background()
		m := newRunner(db, dialect)
		m.Register(coremig.Migration{Version: 1, Name: "users",
			Up: "CREATE TABLE rt_users (id INTEGER PRIMARY KEY)", Down: "DROP TABLE rt_users"})
		m.Register(coremig.Migration{Version: 2, Name: "email",
			Up: "ALTER TABLE rt_users ADD COLUMN email TEXT", Down: "ALTER TABLE rt_users DROP COLUMN email"})

		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if len(st.Applied) != 2 || len(st.Pending) != 0 {
			t.Fatalf("expected 2 applied / 0 pending, got %d/%d", len(st.Applied), len(st.Pending))
		}
		// Recorded checksums must be non-empty.
		for _, rec := range st.Applied {
			if rec.Checksum == "" {
				t.Errorf("version %d recorded with empty checksum", rec.Version)
			}
		}

		if err := m.Down(ctx, 1); err != nil {
			t.Fatalf("Down(1): %v", err)
		}
		st, _ = m.Status(ctx)
		if len(st.Applied) != 1 || st.Applied[0].Version != 1 {
			t.Fatalf("expected version 1 still applied, got %+v", st.Applied)
		}
	})
}

// TestRunner_ChecksumDrift proves drift detection works on real Postgres too.
func TestRunner_ChecksumDrift(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ctx := context.Background()
		m := newRunner(db, dialect)
		m.Register(coremig.Migration{Version: 1, Name: "t",
			Up: "CREATE TABLE rt_drift (id INTEGER)", Down: "DROP TABLE rt_drift"})
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}

		m2 := newRunner(db, dialect)
		m2.Register(coremig.Migration{Version: 1, Name: "t",
			Up: "CREATE TABLE rt_drift (id INTEGER, more INTEGER)", Down: "DROP TABLE rt_drift"})
		var cm *coremig.ChecksumMismatchError
		if err := m2.Up(ctx); !errors.As(err, &cm) {
			t.Fatalf("expected ChecksumMismatchError, got %v", err)
		}
	})
}

// TestRunner_NoTxDirtyAndForce proves the no-transaction dirty marker and
// Force recovery work on real Postgres too.
func TestRunner_NoTxDirtyAndForce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ctx := context.Background()
		m := newRunner(db, dialect)
		// The dirty row is committed before the Up runs; the failing Up leaves
		// it dirty regardless of whether the engine rolls back the DDL.
		m.Register(coremig.Migration{Version: 1, Name: "halfway", NoTransaction: true,
			Up:   "CREATE TABLE rt_half (id INTEGER); INSERT INTO rt_missing VALUES (1)",
			Down: "DROP TABLE IF EXISTS rt_half"})

		if err := m.Up(ctx); err == nil {
			t.Fatal("expected the no-transaction migration to fail")
		}
		if err := m.Up(ctx); !errors.Is(err, coremig.ErrDirty) {
			t.Fatalf("expected ErrDirty on re-run, got %v", err)
		}
		if err := m.Force(ctx, 1, true); err != nil {
			t.Fatalf("Force: %v", err)
		}
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up after Force should succeed, got %v", err)
		}
	})
}
