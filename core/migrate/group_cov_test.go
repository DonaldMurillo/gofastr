package migrate

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// This file restores the core/migrate coverage floor for the group-aware
// branches and their error paths. The pre-group sqlmock suite (see
// migrate_test.go) only exercised the legacy (default-group, ga==false) path;
// the group happy paths run against real SQLite in groups_test.go, but the
// Postgres-dialect branches (upgradePKPostgres, the pg_index/pg_attribute
// queries) and every error branch are unreachable without a live Postgres —
// which CI does not have. These tests drive them via sqlmock so CI's
// (Postgres-free) own-package coverage holds at the floor.

// ---- group-aware sqlmock helpers (Postgres dialect) ----

// expectCreateTableGA expects the group-aware CREATE TABLE plus the three
// backfill ALTERs ensureTrackingColumns emits in ga mode (checksum, dirty,
// group_name), in that order.
func expectCreateTableGA(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN IF NOT EXISTS checksum").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN IF NOT EXISTS dirty").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN IF NOT EXISTS group_name").WillReturnResult(sqlmock.NewResult(0, 0))
}

// expectPKComposite makes ensureCompositeKey a no-op: primaryKeyColumns returns
// [group_name, version] on the Postgres pg_index query, so the key is already
// composite and nothing is upgraded.
func expectPKComposite(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("pg_index").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("group_name").AddRow("version"))
}

// expectAppliedGA expects the group-aware applied-versions SELECT. Pass nil for
// an empty (no rows applied) result.
func expectAppliedGA(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	if rows == nil {
		rows = sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"})
	}
	mock.ExpectQuery("checksum, dirty, group_name").WillReturnRows(rows)
}

// expectMigrationUpGA expects a group-aware transactional up: BEGIN, the
// migration SQL, the group-aware bookkeeping INSERT, COMMIT.
func expectMigrationUpGA(mock sqlmock.Sqlmock, group string, version uint64, upSQL string) {
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(upSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("group_name, version, name, applied_at, checksum, dirty").
		WithArgs(group, version, sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

// ---- exported ValidateGroupName (migrate.go) ----

func TestValidateGroupName_Exported(t *testing.T) {
	if err := ValidateGroupName(""); err != nil {
		t.Errorf("empty (default group): unexpected %v", err)
	}
	if err := ValidateGroupName("knowledge"); err != nil {
		t.Errorf("valid name: unexpected %v", err)
	}
	if err := ValidateGroupName("default"); err == nil {
		t.Error(`"default" is reserved and must be rejected`)
	}
	if err := ValidateGroupName("bad name!"); err == nil {
		t.Error("invalid characters must be rejected")
	}
}

// ---- groupAware arg path (runner.go) ----

func TestGroupAware_GroupArgAndRegistered(t *testing.T) {
	m, _ := newTestMigrator(t)
	// No group migrations registered, but a non-empty group arg forces ga.
	if !m.groupAware([]string{"x"}) {
		t.Error(`groupAware(["x"]) with no group migrations = want true`)
	}
	if m.groupAware(nil) {
		t.Error("groupAware(nil) with no group migrations = want false")
	}
	// An empty group arg is not group-aware.
	if m.groupAware([]string{""}) {
		t.Error(`groupAware([""]) = want false`)
	}
	// A registered non-default group forces ga even with no args.
	m2, _ := newTestMigrator(t)
	m2.Register(Migration{Version: 1, Name: "k", Group: "knowledge"})
	if !m2.groupAware(nil) {
		t.Error("groupAware(nil) with a registered group = want true")
	}
}

// ---- group-aware PG Up, transactional (runner.go runMigrationUp ga branch) ----

func TestGroupAwarePGUp_Transactional(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	upSQL := "CREATE TABLE k (id int)"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "DROP TABLE k"})

	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, nil)
	expectMigrationUpGA(mock, "knowledge", 1, upSQL)
	expectUnlock(mock)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware PG Up against a LEGACY single-column table ----
// Drives primaryKeyColumns (pg_index), ensureCompositeKey's Postgres dispatch,
// and upgradePKPostgres's success path (find constraint, ALTER to composite).

func TestGroupAwarePGUp_LegacyTableUpgrade(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	upSQL := "CREATE TABLE k (id int)"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "DROP TABLE k"})

	expectLock(mock)
	expectCreateTableGA(mock)
	// Legacy single-column PK on disk → ensureCompositeKey upgrades it.
	mock.ExpectQuery("pg_index").
		WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("version"))
	mock.ExpectQuery("pg_constraint").
		WillReturnRows(sqlmock.NewRows([]string{"conname"}).AddRow("_migrations_pkey"))
	mock.ExpectExec("DROP CONSTRAINT").WillReturnResult(sqlmock.NewResult(0, 0))
	expectAppliedGA(mock, nil)
	expectMigrationUpGA(mock, "knowledge", 1, upSQL)
	expectUnlock(mock)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware PG Up, no-transaction (runMigrationUpNoTx ga branches) ----

func TestGroupAwarePGUp_NoTransaction(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	upSQL := "CREATE INDEX CONCURRENTLY k_idx"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "DROP INDEX k_idx", NoTransaction: true})

	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, nil)
	// Dirty INSERT (ga) → run DDL → clear dirty (ga).
	mock.ExpectExec("group_name, version, name, applied_at, checksum, dirty").
		WithArgs("knowledge", uint64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(upSQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE").
		WithArgs("knowledge", uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectUnlock(mock)

	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware PG Down, transactional (runMigrationDown ga delete) ----

func TestGroupAwarePGDown_Transactional(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	downSQL := "DROP TABLE k"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "CREATE TABLE k (id int)", Down: downSQL})

	applied := sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
		AddRow(uint64(1), "k1", time.Now().UTC(), "", false, "knowledge")

	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, applied)
	mock.ExpectBegin()
	mock.ExpectExec(downSQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").
		WithArgs("knowledge", uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectUnlock(mock)

	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware PG Down, no-transaction (runMigrationDownNoTx ga branches) ----

func TestGroupAwarePGDown_NoTransaction(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	downSQL := "DROP INDEX CONCURRENTLY k_idx"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "CREATE INDEX CONCURRENTLY k_idx", Down: downSQL, NoTransaction: true})

	applied := sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
		AddRow(uint64(1), "k1", time.Now().UTC(), "", false, "knowledge")

	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, applied)
	// mark dirty (ga) → run Down → delete row (ga).
	mock.ExpectExec("UPDATE").
		WithArgs("knowledge", uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(downSQL).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").
		WithArgs("knowledge", uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectUnlock(mock)

	if err := m.Down(ctx, 1); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware PG Down with a cross-group version tie (down sort tiebreak) ----

func TestGroupAwarePGDown_GroupTiebreak(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "UK", Down: "DK"})
	mustReg(t, m, Migration{Version: 1, Name: "s1", Group: "search", Up: "US", Down: "DS"})

	// Both groups own version 1 and are applied. Down(2) rolls them back
	// most-recent-first; the version tie is broken by group name descending,
	// so "search" precedes "knowledge".
	applied := sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
		AddRow(uint64(1), "k1", time.Now().UTC(), "", false, "knowledge").
		AddRow(uint64(1), "s1", time.Now().UTC(), "", false, "search")

	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, applied)
	// search first (group desc), then knowledge.
	mock.ExpectBegin()
	mock.ExpectExec("DS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").WithArgs("search", uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec("DK").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").WithArgs("knowledge", uint64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectUnlock(mock)

	if err := m.Down(ctx, 2); err != nil {
		t.Fatalf("Down: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- upgradePKPostgres branches beyond the success path ----

func TestUpgradePKPostgres_Branches(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		setUp  func(sqlmock.Sqlmock)
		wantOK bool // true = Up must succeed; false = Up must error
	}{
		{
			// No PK constraint at all → upgrade is a no-op, Up proceeds.
			name: "no_pk_constraint_is_noop",
			setUp: func(mock sqlmock.Sqlmock) {
				expectLock(mock)
				expectCreateTableGA(mock)
				mock.ExpectQuery("pg_index").
					WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("version"))
				mock.ExpectQuery("pg_constraint").WillReturnError(sql.ErrNoRows)
				expectAppliedGA(mock, nil)
				expectMigrationUpGA(mock, "knowledge", 1, "U")
				expectUnlock(mock)
			},
			wantOK: true,
		},
		{
			// pg_constraint query errors → ensureCompositeKey errors → Up errors.
			name: "constraint_query_error",
			setUp: func(mock sqlmock.Sqlmock) {
				expectLock(mock)
				expectCreateTableGA(mock)
				mock.ExpectQuery("pg_index").
					WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("version"))
				mock.ExpectQuery("pg_constraint").WillReturnError(errors.New("pg down"))
				expectUnlock(mock)
			},
			wantOK: false,
		},
		{
			// Constraint name fails SafeIdent → error.
			name: "constraint_name_unsafe",
			setUp: func(mock sqlmock.Sqlmock) {
				expectLock(mock)
				expectCreateTableGA(mock)
				mock.ExpectQuery("pg_index").
					WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("version"))
				mock.ExpectQuery("pg_constraint").
					WillReturnRows(sqlmock.NewRows([]string{"conname"}).AddRow("bad name;"))
				expectUnlock(mock)
			},
			wantOK: false,
		},
		{
			// ALTER fails → error.
			name: "alter_error",
			setUp: func(mock sqlmock.Sqlmock) {
				expectLock(mock)
				expectCreateTableGA(mock)
				mock.ExpectQuery("pg_index").
					WillReturnRows(sqlmock.NewRows([]string{"attname"}).AddRow("version"))
				mock.ExpectQuery("pg_constraint").
					WillReturnRows(sqlmock.NewRows([]string{"conname"}).AddRow("_migrations_pkey"))
				mock.ExpectExec("DROP CONSTRAINT").WillReturnError(errors.New("alter fail"))
				expectUnlock(mock)
			},
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, mock := newTestMigrator(t)
			mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
			c.setUp(mock)
			err := m.Up(ctx)
			switch {
			case c.wantOK && err != nil:
				t.Fatalf("Up: unexpected error %v", err)
			case !c.wantOK && err == nil:
				t.Fatal("Up: expected error, got nil")
			}
		})
	}
}

// ---- primaryKeyColumns query/scan error (also drives ensureCompositeKey + up errors) ----

func TestPrimaryKeyColumns_PGErrors(t *testing.T) {
	ctx := context.Background()

	// Query error.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		mock.ExpectQuery("pg_index").WillReturnError(errors.New("catalog down"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected primaryKeyColumns query error")
		}
	}

	// Scan error: a column-count mismatch (2 columns returned, 1 scanned)
	// is the one shape that makes rows.Scan into a *string actually fail.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		mock.ExpectQuery("pg_index").
			WillReturnRows(sqlmock.NewRows([]string{"attname", "extra"}).AddRow("version", "x"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected primaryKeyColumns scan error")
		}
	}
}

// ---- rebuildTableSQLite error branches (SQLite dialect via sqlmock) ----
// SQLite is the dialect-neutral-ish path: WithAdvisoryLock takes no lock, and
// ensureCompositeKey dispatches to rebuildTableSQLite. Each sub-case fails one
// step of the create/copy/drop/rename/commit rebuild and asserts Up errors.

func expectCreateTableGASQLite(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN checksum").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN dirty").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN group_name").WillReturnResult(sqlmock.NewResult(0, 0))
}

// gaSQLiteRebuildPreamble sets up through primaryKeyColumns returning a legacy
// [version] key, so ensureCompositeKey dispatches into rebuildTableSQLite.
func gaSQLiteRebuildPreamble(mock sqlmock.Sqlmock) {
	expectCreateTableGASQLite(mock)
	mock.ExpectQuery("pragma_table_info").
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("version"))
}

func TestRebuildTableSQLite_ErrorBranches(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name  string
		setUp func(sqlmock.Sqlmock)
	}{
		{
			name: "begin_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin().WillReturnError(errors.New("no tx"))
			},
		},
		{
			name: "create_temp_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin()
				mock.ExpectExec("_grp_upg").WillReturnError(errors.New("create fail"))
				mock.ExpectRollback()
			},
		},
		{
			name: "copy_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin()
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("_grp_upg").WillReturnError(errors.New("copy fail"))
				mock.ExpectRollback()
			},
		},
		{
			name: "drop_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin()
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("DROP TABLE").WillReturnError(errors.New("drop fail"))
				mock.ExpectRollback()
			},
		},
		{
			name: "rename_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin()
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("DROP TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("RENAME TO").WillReturnError(errors.New("rename fail"))
				mock.ExpectRollback()
			},
		},
		{
			name: "commit_error",
			setUp: func(mock sqlmock.Sqlmock) {
				gaSQLiteRebuildPreamble(mock)
				mock.ExpectBegin()
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("_grp_upg").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("DROP TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec("RENAME TO").WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectCommit().WillReturnError(errors.New("commit fail"))
				mock.ExpectRollback()
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, mock := newTestMigratorWithDialect(t, DialectSQLite)
			mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
			c.setUp(mock)
			if err := m.Up(ctx); err == nil {
				t.Fatal("expected rebuildTableSQLite error")
			}
		})
	}
}

// ---- hasGroupColumn SQLite branch + error (runner.go) ----

func TestHasGroupColumn_SQLiteBranch(t *testing.T) {
	// Success: legacy table, no group_name column.
	{
		m, mock := newTestMigratorWithDialect(t, DialectSQLite)
		ctx := context.Background()
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN checksum").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN dirty").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("pragma_table_info").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT version, name, applied_at, checksum, dirty FROM").
			WillReturnRows(sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty"}))
		st, err := m.Status(ctx)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if len(st.Applied) != 0 || len(st.Pending) != 0 {
			t.Errorf("expected empty status, got applied=%d pending=%d", len(st.Applied), len(st.Pending))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet: %v", err)
		}
	}

	// Scan error from the pragma_table_info metadata query.
	{
		m, mock := newTestMigratorWithDialect(t, DialectSQLite)
		ctx := context.Background()
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN checksum").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN dirty").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("pragma_table_info").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow("not-a-number"))
		if _, err := m.Status(ctx); err == nil {
			t.Fatal("expected hasGroupColumn scan error")
		}
	}
}

// ---- appliedVersions error branches (runner.go) ----

func TestAppliedVersions_QueryAndScanErrors(t *testing.T) {
	ctx := context.Background()

	// Legacy query error (after hasGroupColumn succeeds): up() path.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTable(mock)
		mock.ExpectQuery("pg_attribute").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT version, name, applied_at, checksum, dirty FROM").
			WillReturnError(errors.New("rows fail"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected appliedVersions query error")
		}
	}

	// Group-aware scan error: ga SELECT returns a row that won't scan into 6 cols.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		expectAppliedGA(mock, sqlmock.NewRows([]string{"version"}).AddRow("bogus"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected group-aware appliedVersions scan error")
		}
	}

	// Legacy scan error: SELECT returns a value that won't scan into a uint64.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTable(mock)
		mock.ExpectQuery("pg_attribute").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT version, name, applied_at, checksum, dirty FROM").
			WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("not-a-number"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected legacy appliedVersions scan error")
		}
	}
}

// ---- down()/Status() appliedVersions error wraps (runner.go) ----

func TestDown_Status_AppliedVersionsErrors(t *testing.T) {
	ctx := context.Background()

	// down(): legacy appliedVersions query error.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTable(mock)
		mock.ExpectQuery("pg_attribute").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT version, name, applied_at, checksum, dirty FROM").
			WillReturnError(errors.New("rows fail"))
		expectUnlock(mock)
		if err := m.Down(ctx, 1); err == nil {
			t.Fatal("expected down appliedVersions query error")
		}
	}

	// Status(): legacy appliedVersions query error.
	{
		m, mock := newTestMigrator(t)
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN IF NOT EXISTS checksum").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("ADD COLUMN IF NOT EXISTS dirty").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("pg_attribute").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectQuery("SELECT version, name, applied_at, checksum, dirty FROM").
			WillReturnError(errors.New("rows fail"))
		if _, err := m.Status(ctx); err == nil {
			t.Fatal("expected status appliedVersions query error")
		}
	}
}

// ---- Force group-aware branches (runner.go) ----

func TestForce_GroupBranches(t *testing.T) {
	ctx := context.Background()

	// Invalid group name → validateGroupName error.
	{
		m, mock := newTestMigrator(t)
		_ = mock
		if err := m.Force(ctx, 1, true, "bad name;"); err == nil {
			t.Fatal("expected Force to reject an invalid group name")
		}
	}

	// group-aware Force applied=true: upsert a group row (tableGa path).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		mock.ExpectExec("DO UPDATE SET dirty").
			WithArgs("knowledge", uint64(1), "k1", sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
		expectUnlock(mock)
		if err := m.Force(ctx, 1, true, "knowledge"); err != nil {
			t.Fatalf("Force applied group: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet: %v", err)
		}
	}

	// group-aware Force applied=false: delete a group row (tableGa path).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		mock.ExpectExec("DELETE FROM").
			WithArgs("knowledge", uint64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		expectUnlock(mock)
		if err := m.Force(ctx, 1, false, "knowledge"); err != nil {
			t.Fatalf("Force delete group: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet: %v", err)
		}
	}

	// Default-group Force applied=false on a table whose disk shape HAS a
	// group_name column (tableGa=true, ga=false): the group-aware DELETE runs.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "x", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTable(mock)
		mock.ExpectQuery("pg_attribute").
			WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		mock.ExpectExec("DELETE FROM").
			WithArgs("", uint64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		expectUnlock(mock)
		if err := m.Force(ctx, 1, false); err != nil {
			t.Fatalf("Force tableGa delete: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet: %v", err)
		}
	}

	// group-aware Force where ensureCompositeKey errors.
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
		expectLock(mock)
		expectCreateTableGA(mock)
		mock.ExpectQuery("pg_index").WillReturnError(errors.New("catalog down"))
		expectUnlock(mock)
		if err := m.Force(ctx, 1, true, "knowledge"); err == nil {
			t.Fatal("expected Force ensureCompositeKey error")
		}
	}
}

// ---- Status group-aware read (tableGa=true, runner.go Status) ----

func TestStatus_GroupAwareRead(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "d1", Up: "U", Down: "D"})
	mustReg(t, m, Migration{Version: 2, Name: "k2", Group: "knowledge", Up: "U", Down: "D"})

	// Status always creates with ga=false (read-only), then reads the table's
	// real shape: here the disk HAS a group_name column, so the group-aware
	// SELECT runs and a knowledge row is reported with its real group.
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN IF NOT EXISTS checksum").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD COLUMN IF NOT EXISTS dirty").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("pg_attribute").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery("checksum, dirty, group_name").
		WillReturnRows(sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
			AddRow(uint64(2), "k2", time.Now().UTC(), "", false, "knowledge"))

	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(st.Applied) != 1 || st.Applied[0].Group != "knowledge" {
		t.Fatalf("expected one knowledge applied row, got %+v", st.Applied)
	}
	// default v1 is registered but not applied → pending; knowledge v2 is applied.
	if len(st.Pending) != 1 || st.Pending[0].Version != 1 {
		t.Fatalf("expected only default v1 pending, got %+v", st.Pending)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet: %v", err)
	}
}

// ---- group-aware migration error branches (runner.go) ----
// Each fails one group-aware SQL statement and asserts the runner surfaces the
// error (with rollback/early-return as appropriate). These are the defensive
// error returns the happy-path group tests never reach.

func TestRunMigrationUp_GAInsertError(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	upSQL := "CREATE TABLE k (id int)"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "D"})
	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, nil)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(upSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("group_name, version, name, applied_at, checksum, dirty").
		WithArgs("knowledge", uint64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(errors.New("insert fail"))
	mock.ExpectRollback()
	expectUnlock(mock)
	if err := m.Up(ctx); err == nil {
		t.Fatal("expected group-aware insert error")
	}
}

func TestRunMigrationUpNoTx_GAErrors(t *testing.T) {
	ctx := context.Background()
	upSQL := "CREATE INDEX CONCURRENTLY k_idx"

	// Dirty-insert error (group-aware).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "D", NoTransaction: true})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		expectAppliedGA(mock, nil)
		mock.ExpectExec("group_name, version, name, applied_at, checksum, dirty").
			WithArgs("knowledge", uint64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnError(errors.New("dirty insert fail"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected group-aware dirty-insert error")
		}
	}

	// Clear-dirty error (group-aware).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: upSQL, Down: "D", NoTransaction: true})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		expectAppliedGA(mock, nil)
		mock.ExpectExec("group_name, version, name, applied_at, checksum, dirty").
			WithArgs("knowledge", uint64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(regexp.QuoteMeta(upSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("UPDATE").
			WithArgs("knowledge", uint64(1)).
			WillReturnError(errors.New("clear fail"))
		expectUnlock(mock)
		if err := m.Up(ctx); err == nil {
			t.Fatal("expected group-aware clear-dirty error")
		}
	}
}

func TestRunMigrationDown_GADeleteError(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	downSQL := "DROP TABLE k"
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: downSQL})
	applied := sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
		AddRow(uint64(1), "k1", time.Now().UTC(), "", false, "knowledge")
	expectLock(mock)
	expectCreateTableGA(mock)
	expectPKComposite(mock)
	expectAppliedGA(mock, applied)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(downSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DELETE FROM").
		WithArgs("knowledge", uint64(1)).
		WillReturnError(errors.New("delete fail"))
	mock.ExpectRollback()
	expectUnlock(mock)
	if err := m.Down(ctx, 1); err == nil {
		t.Fatal("expected group-aware delete error")
	}
}

func TestRunMigrationDownNoTx_GAErrors(t *testing.T) {
	ctx := context.Background()
	downSQL := "DROP INDEX CONCURRENTLY k_idx"
	applied := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"version", "name", "applied_at", "checksum", "dirty", "group_name"}).
			AddRow(uint64(1), "k1", time.Now().UTC(), "", false, "knowledge")
	}

	// Mark-dirty error (group-aware).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: downSQL, NoTransaction: true})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		expectAppliedGA(mock, applied())
		mock.ExpectExec("UPDATE").
			WithArgs("knowledge", uint64(1)).
			WillReturnError(errors.New("mark fail"))
		expectUnlock(mock)
		if err := m.Down(ctx, 1); err == nil {
			t.Fatal("expected group-aware mark-dirty error")
		}
	}

	// Delete error after a successful Down (group-aware).
	{
		m, mock := newTestMigrator(t)
		mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: downSQL, NoTransaction: true})
		expectLock(mock)
		expectCreateTableGA(mock)
		expectPKComposite(mock)
		expectAppliedGA(mock, applied())
		mock.ExpectExec("UPDATE").
			WithArgs("knowledge", uint64(1)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(regexp.QuoteMeta(downSQL)).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("DELETE FROM").
			WithArgs("knowledge", uint64(1)).
			WillReturnError(errors.New("delete fail"))
		expectUnlock(mock)
		if err := m.Down(ctx, 1); err == nil {
			t.Fatal("expected group-aware delete error")
		}
	}
}

func TestDown_GroupAwareEnsureCompositeKeyError(t *testing.T) {
	m, mock := newTestMigrator(t)
	ctx := context.Background()
	mustReg(t, m, Migration{Version: 1, Name: "k1", Group: "knowledge", Up: "U", Down: "D"})
	expectLock(mock)
	expectCreateTableGA(mock)
	mock.ExpectQuery("pg_index").WillReturnError(errors.New("catalog down"))
	expectUnlock(mock)
	if err := m.Down(ctx, 1); err == nil {
		t.Fatal("expected down ensureCompositeKey error")
	}
}
