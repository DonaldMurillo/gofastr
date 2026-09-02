package migrate

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// This file holds the adversarial (property × surface) tests for the runner's
// integrity machinery: advisory-lock lifecycle edges, checksum-verification
// scoping, and dirty-state recovery. Happy-path ordering is pinned in
// migrate_test.go / lock_test.go; only the security-shaped edges live here.

// Property: the documented checksum-drift recovery path actually recovers.
// Force(version, applied=true) documents "If the version is registered, its
// name and checksum are recorded so future drift checks line up". After a
// post-apply edit, that promise is what lets an operator reconcile and resume
// without re-executing DDL.
//
// Surfaces: both tracking-table shapes (legacy single-column PK and
// group-aware composite PK), because Force emits a different upsert per shape.
//
// RED: on both shapes the ON CONFLICT arm only does `SET dirty = FALSE`, so a
// row that already exists keeps its OLD checksum. The operator follows the
// documented recovery (Force applied=true) and the next Up STILL refuses with
// ChecksumMismatchError — the only remaining exits are reverting the file or
// Force(applied=false), which re-executes the migration's DDL against a
// database that may already be half-converged. FLAG for the owner: either the
// DO UPDATE must also set checksum/name from EXCLUDED, or the Force doc
// comment must stop claiming drift checks "line up" after reconciliation.
func TestForceDoesNotRefreshDriftedChecksum(t *testing.T) {
	for _, shape := range []struct {
		name  string
		group bool
	}{
		{"legacy table", false},
		{"group-aware table", true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			ctx := context.Background()
			m1, db := newSQLiteMigrator(t)
			g := ""
			if shape.group {
				g = "knowledge"
			}
			mustReg(t, m1, Migration{Group: g, Version: 1, Name: "one",
				Up: "CREATE TABLE fr1 (id INTEGER)", Down: "DROP TABLE fr1"})
			if err := m1.Up(ctx); err != nil {
				t.Fatalf("first Up: %v", err)
			}

			// The file is edited after being applied → drift.
			m2 := New(db, WithDialect(DialectSQLite))
			mustReg(t, m2, Migration{Group: g, Version: 1, Name: "one",
				Up: "CREATE TABLE fr1 (id INTEGER); ALTER TABLE fr1 ADD COLUMN note TEXT", Down: "DROP TABLE fr1"})
			err := m2.Up(ctx)
			if err == nil {
				t.Fatal("expected ChecksumMismatchError before Force")
			}
			if _, ok := err.(*ChecksumMismatchError); !ok {
				t.Fatalf("expected *ChecksumMismatchError, got %T: %v", err, err)
			}

			// The documented reconciliation.
			force := func() error { return m2.Force(ctx, 1, true, forceGroups(shape.group, g)...) }
			if err := force(); err != nil {
				t.Fatalf("Force(v,true): %v", err)
			}
			if err := m2.Up(ctx); err != nil {
				t.Errorf("Up still refuses after Force(v,true): %v — Force is the documented drift reconciliation; the recorded checksum must line up with the registered migration", err)
			}

			// Happy-path arm: on a MISSING row (the baseline shape) Force does
			// record the registered checksum. Proves the red above is the
			// conflict arm specifically, not the recording logic as a whole.
			if shape.group {
				if _, err := db.Exec("DELETE FROM _migrations WHERE group_name = ? AND version = 1", g); err != nil {
					t.Fatalf("delete row: %v", err)
				}
			} else {
				if _, err := db.Exec("DELETE FROM _migrations WHERE version = 1"); err != nil {
					t.Fatalf("delete row: %v", err)
				}
			}
			if err := force(); err != nil {
				t.Fatalf("Force(v,true) baseline: %v", err)
			}
			if err := m2.Up(ctx); err != nil {
				t.Errorf("Up after baseline Force: %v", err)
			}
		})
	}
}

// forceGroups returns the group selection Force should target: the named
// group in group-aware shape, nothing (default group) in legacy shape.
func forceGroups(group bool, g string) []string {
	if group {
		return []string{g}
	}
	return nil
}

// Property: the advisory lock is released even when the caller's ctx is
// cancelled WHILE THE LOCK IS HELD (shutdown mid-migration), not only while
// waiting for it. The release must run on a background context so a cancelled
// boot still unlocks instead of leaving the key held until the session is
// reaped — otherwise a rolling-shutdown deploy serializes every later
// replica's DDL behind a dead holder's timeout.
//
// Surface: the Postgres path of WithAdvisoryLockKey (the only path that
// locks). lock_test.go covers cancel-while-WAITING; this is cancel-while-HELD.
func TestUnlockAfterCancelDuringFn(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mock.ExpectQuery("SELECT pg_try_advisory_lock").
		WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectExec("SELECT pg_advisory_unlock").
		WillReturnResult(sqlmock.NewResult(0, 1))

	sentinel := errors.New("migration aborted by shutdown")
	err = WithAdvisoryLock(ctx, db, DialectPostgres, func(_ *sql.Conn) error {
		cancel() // the shutdown lands while this replica HOLDS the lock
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("fn error must propagate, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unlock was not issued after ctx cancel during fn: %v", err)
	}
}

// Property: release targets the key that was acquired. WithAdvisoryLockKey
// exists so hosts can namespace their lock away from other tooling; if the
// unlock ever went back to the fixed default key, the custom-key lock would
// leak for the session's lifetime and the default key would be unlocked
// despite never being held (unlocking another tool's lock).
//
// Surface: acquire (pg_try_advisory_lock) and release (pg_advisory_unlock)
// must both carry the caller-supplied key, verified via bound-arg matching.
func TestCustomLockKeyAcquireAndRelease(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	const custom int64 = -9876543210
	mock.ExpectQuery("SELECT pg_try_advisory_lock").WithArgs(custom).
		WillReturnRows(sqlmock.NewRows([]string{"got"}).AddRow(true))
	mock.ExpectExec("SELECT pg_advisory_unlock").WithArgs(custom).
		WillReturnResult(sqlmock.NewResult(0, 1))

	ran := false
	err = WithAdvisoryLockKey(context.Background(), db, DialectPostgres, custom, func(_ *sql.Conn) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("WithAdvisoryLockKey: %v", err)
	}
	if !ran {
		t.Fatal("fn did not run")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("lock/unlock key mismatch: %v", err)
	}
}

// Property: the DDL phase and the seed phase use DISTINCT advisory-lock keys,
// so a replica that finished its DDL but is still seeding never blocks another
// replica's DDL (and vice versa). Both constants are frozen; if a refactor
// ever unified them, RunSeeds (which holds the seed key across every Seed
// body) would deadlock cross-replica boot against every Up.
func TestDistinctLockKeysForDdlAndSeed(t *testing.T) {
	if AdvisoryLockKey == SeedAdvisoryLockKey {
		t.Fatalf("AdvisoryLockKey == SeedAdvisoryLockKey (%d): the DDL and seed phases share one lock, the exact cross-replica block the split exists to prevent", AdvisoryLockKey)
	}
}

// Property: a dirty row belongs to the group that wrote it. A de-registered
// module's dirty row (that module's property, design: modularity) must not
// brick the DEFAULT group's Up/Down, while a migrator that still registers
// the group must stay blocked — the ignore is scoping, not a blanket skip.
//
// Surfaces: Up (checkIntegrity) and Down (the ownGroup dirty gate) on a real
// SQLite tracking table in group-aware shape.
func TestForeignGroupDirtyDoesNotBlock(t *testing.T) {
	ctx := context.Background()
	m1, db := newSQLiteMigrator(t)
	// Two statements: the first commits outside any tx, the second fails →
	// the no-tx protocol leaves a dirty row behind in group "gone".
	mustReg(t, m1, Migration{Group: "gone", Version: 1, Name: "g1", NoTransaction: true,
		Up: "CREATE TABLE gone_t (id INTEGER); NOT VALID SQL"})
	if err := m1.Up(ctx); err == nil {
		t.Fatal("expected the no-tx migration to fail and leave a dirty row")
	}

	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Version: 1, Name: "core",
		Up: "CREATE TABLE fresh_t (id INTEGER)", Down: "DROP TABLE fresh_t"})
	if err := m2.Up(ctx); err != nil {
		t.Errorf("Up refused over a foreign group's dirty row: %v", err)
	}
	if err := m2.Down(ctx, 1); err != nil {
		t.Errorf("Down refused over a foreign group's dirty row: %v", err)
	}

	// Scoping proof: a migrator that DOES register "gone" stays blocked.
	m3 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m3, Migration{Group: "gone", Version: 1, Name: "g1",
		Up: "SELECT 1", Down: "SELECT 1"})
	if err := m3.Up(ctx); !errors.Is(err, ErrDirty) {
		t.Errorf("owner of the dirty group must stay blocked, got %v", err)
	}
}

// Property: the blank-checksum amnesty for legacy rows applies per
// (Group, Version) pair in the group-aware path too — a pre-checksum row in
// a NAMED group must neither trip drift nor count as pending (re-running a
// migration that already applied is the exact half-converged-DDL hazard the
// checksum machinery exists to prevent).
func TestBlankChecksumSkipsDriftInGroup(t *testing.T) {
	ctx := context.Background()
	m1, db := newSQLiteMigrator(t)
	mustReg(t, m1, Migration{Group: "knowledge", Version: 1, Name: "kb",
		Up: "CREATE TABLE kb_blank (id INTEGER)", Down: "DROP TABLE kb_blank"})
	if err := m1.Up(ctx); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Age the row to the pre-checksum era.
	if _, err := db.Exec("UPDATE _migrations SET checksum = '' WHERE group_name = 'knowledge'"); err != nil {
		t.Fatalf("blank checksum: %v", err)
	}

	// Same (group, version), different SQL: no checksum to compare → applied.
	m2 := New(db, WithDialect(DialectSQLite))
	mustReg(t, m2, Migration{Group: "knowledge", Version: 1, Name: "kb",
		Up: "CREATE TABLE kb_blank_v2 (id INTEGER)", Down: "DROP TABLE kb_blank_v2"})
	if err := m2.Up(ctx); err != nil {
		t.Errorf("blank legacy row tripped drift: %v", err)
	}
	if tableExists(t, db, "kb_blank_v2") {
		t.Error("blank-checksum amnesty must not re-run an applied migration (kb_blank_v2 exists)")
	}
	st, err := m2.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st.Pending) != 0 {
		t.Errorf("blank-checksum row counted as pending: %d", len(st.Pending))
	}
}

// Property: Force is the documented way out of a dirty state, on BOTH table
// shapes: after Force(version, applied=true) the row must be clean and Up
// must proceed as a no-op (the version is recorded as applied; its SQL is
// never re-run by Force). pg_integration_more_test.go covers the Postgres
// legacy shape; this pins SQLite and the group-aware shape.
func TestForceClearsDirtyOnBothShapes(t *testing.T) {
	for _, shape := range []struct {
		name  string
		group bool
	}{
		{"legacy table", false},
		{"group-aware table", true},
	} {
		t.Run(shape.name, func(t *testing.T) {
			ctx := context.Background()
			m1, db := newSQLiteMigrator(t)
			g := ""
			if shape.group {
				g = "mods"
			}
			mustReg(t, m1, Migration{Group: g, Version: 1, Name: "nt", NoTransaction: true,
				Up: "CREATE TABLE dirty_t (id INTEGER); NOT VALID SQL"})
			if err := m1.Up(ctx); err == nil {
				t.Fatal("expected the no-tx migration to fail mid-DDL")
			}

			// The operator reconciles WITHOUT editing the file: m2 registers
			// the same Up body, so this test pins the dirty-clearing property
			// alone (the edited-file recovery divergence is pinned by
			// TestForceDoesNotRefreshDriftedChecksum).
			m2 := New(db, WithDialect(DialectSQLite))
			mustReg(t, m2, Migration{Group: g, Version: 1, Name: "nt",
				Up: "CREATE TABLE dirty_t (id INTEGER); NOT VALID SQL", Down: "DROP TABLE dirty_t"})
			// The half-applied state blocks before Force: that is the dirty
			// marker doing its job.
			if err := m2.Up(ctx); !errors.Is(err, ErrDirty) {
				t.Fatalf("expected ErrDirty before Force, got %v", err)
			}
			if err := m2.Force(ctx, 1, true, forceGroups(shape.group, g)...); err != nil {
				t.Fatalf("Force(v,true): %v", err)
			}
			if err := m2.Up(ctx); err != nil {
				t.Errorf("Up after Force must proceed, got %v", err)
			}
			st, err := m2.Status(ctx)
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			for _, rec := range st.Applied {
				if rec.Version == 1 && rec.Group == g && rec.Dirty {
					t.Error("row still dirty after Force(v,true)")
				}
			}
		})
	}
}

// Property: a group-selection typo fails LOUD. Up and Down validate the
// selection against the registered set before touching the database; a
// misspelled group name must return an error naming the known groups, never
// a silent no-op that leaves an operator believing their module migrated.
func TestUpRejectsUnknownGroupLoudly(t *testing.T) {
	ctx := context.Background()
	m, _ := newSQLiteMigrator(t)
	mustReg(t, m, Migration{Group: "alpha", Version: 1, Name: "a",
		Up: "CREATE TABLE typo_t (id INTEGER)", Down: "DROP TABLE typo_t"})

	for name, fn := range map[string]func() error{
		"up":   func() error { return m.Up(ctx, "alpah") },
		"down": func() error { return m.Down(ctx, 1, "alpah") },
	} {
		err := fn()
		if err == nil {
			t.Errorf("%s with misspelled group silently no-op'd", name)
			continue
		}
		if !strings.Contains(err.Error(), "alpah") || !strings.Contains(err.Error(), "alpha") {
			t.Errorf("%s error must name the unknown and the known groups, got: %v", name, err)
		}
	}
}
