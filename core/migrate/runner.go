package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
)

// MigrationRecord is a row in the _migrations tracking table.
type MigrationRecord struct {
	Group     string
	Version   uint64
	Name      string
	AppliedAt time.Time
	Checksum  string // SHA-256 of the Up SQL recorded at apply time
	Dirty     bool   // true if a no-transaction migration failed mid-apply
}

// migKey is the composite identity of a tracking-table row: (Group, Version).
// Version uniqueness is per group, so the pair — not version alone — is the
// real key. In the legacy (default-group-only) path every key has Group == "",
// which makes it behave as version alone and keeps that path byte-identical.
type migKey struct {
	Group   string
	Version uint64
}

// Status holds the result of querying migration state.
type Status struct {
	Applied []MigrationRecord
	Pending []Migration
}

// nowFunc returns the dialect-appropriate default timestamp expression.
func (m *Migrator) nowFunc() string {
	if m.dialect == DialectSQLite {
		return "CURRENT_TIMESTAMP"
	}
	return "NOW()"
}

// placeholder returns the dialect-appropriate parameter placeholder for the nth arg.
func (m *Migrator) placeholder(n int) string {
	if m.dialect == DialectSQLite {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// connish is the subset of *sql.DB / *sql.Conn the runner needs. Threading it
// lets Up/Down run every statement — tracking-table DDL, applied-version
// reads, and each migration's transaction — on the single connection that
// holds the advisory lock, which is what keeps the runner correct on a
// MaxOpenConns(1) pool.
type connish interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// qtable validates the tracking table name once and returns it quoted. Every
// public entry point calls this up front and threads the result into the
// helpers, so the table name is checked exactly once per operation (no
// redundant, unreachable re-validation deeper in the call tree).
func (m *Migrator) qtable() (string, error) {
	safe, err := query.SafeIdent(m.tableName)
	if err != nil {
		return "", fmt.Errorf("migrate: invalid table name %q: %w", m.tableName, err)
	}
	return query.QuoteIdent(safe), nil
}

// ---- group selection helpers ----

// hasNonDefaultGroup reports whether any registered migration belongs to a
// non-default group.
func (m *Migrator) hasNonDefaultGroup() bool {
	for _, mig := range m.migrations {
		if mig.Group != "" {
			return true
		}
	}
	return false
}

// groupAware reports whether this operation must use the group-aware SQL path.
// That path is required when any registered migration belongs to a non-default
// group or the caller explicitly named a non-default group. When false the
// runner emits the exact legacy SQL sequence — byte-identical behavior for apps
// that never opt into groups — which is also what lets the pre-group test suite
// pass without modification.
func (m *Migrator) groupAware(groups []string) bool {
	if m.hasNonDefaultGroup() {
		return true
	}
	for _, g := range groups {
		if g != "" {
			return true
		}
	}
	return false
}

// groupSelected reports whether group g is among the requested groups. An empty
// groups slice means "all groups" (the no-args default).
func groupSelected(g string, groups []string) bool {
	if len(groups) == 0 {
		return true
	}
	for _, gg := range groups {
		if gg == g {
			return true
		}
	}
	return false
}

// selectedMigrations returns the registered migrations whose group is among the
// requested groups, preserving the (Version, Group) sort order. An empty groups
// slice returns every registered migration (all groups, including default).
func (m *Migrator) selectedMigrations(groups []string) []Migration {
	if len(groups) == 0 {
		return m.migrations
	}
	var out []Migration
	for _, mig := range m.migrations {
		if groupSelected(mig.Group, groups) {
			out = append(out, mig)
		}
	}
	return out
}

// CreateMigrationsTable ensures the migrations tracking table exists. Public
// entry point (used by Status and tests); runs on the pool.
func (m *Migrator) CreateMigrationsTable(ctx context.Context) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return m.createMigrationsTable(ctx, m.db, tbl, m.groupAware(nil))
}

// createMigrationsTable creates the tracking table. The legacy shape (ga ==
// false) is the single-column (version) PRIMARY KEY table every existing app
// uses. The group-aware shape (ga == true) adds a group_name column and uses a
// composite (group_name, version) PRIMARY KEY so two groups can each own a
// version 1. CREATE TABLE IF NOT EXISTS never rewrites an existing table, so a
// legacy table upgraded to group-aware mode gets its key fixed by
// ensureCompositeKey — called by Up/Down/Force only (they hold the advisory
// lock); Status never performs group-schema DDL.
func (m *Migrator) createMigrationsTable(ctx context.Context, x connish, tbl string, ga bool) error {
	var ddl string
	if ga {
		ddl = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		group_name TEXT    NOT NULL DEFAULT '',
		version    BIGINT  NOT NULL,
		name       TEXT    NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT %s,
		checksum   TEXT    NOT NULL DEFAULT '',
		dirty      BOOLEAN NOT NULL DEFAULT FALSE,
		PRIMARY KEY (group_name, version)
	)`, tbl, m.nowFunc())
	} else {
		ddl = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version BIGINT NOT NULL PRIMARY KEY,
		name    TEXT    NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT %s,
		checksum TEXT NOT NULL DEFAULT '',
		dirty BOOLEAN NOT NULL DEFAULT FALSE
	)`, tbl, m.nowFunc())
	}
	if _, err := x.ExecContext(ctx, ddl); err != nil {
		return err
	}
	// Backfill checksum/dirty (and group_name in group-aware mode) onto tables
	// created by an older gofastr. No-op for the freshly-created table above.
	return m.ensureTrackingColumns(ctx, x, tbl, ga)
}

// hasGroupColumn reports whether the tracking table physically has a
// group_name column — the TABLE-STATE signal that picks the read/write SQL
// path. Unlike groupAware (which keys off in-memory registrations), this
// reflects what is on disk: a table upgraded by a previous group-aware run
// keeps the column even after the group is de-registered, and reads must
// still scan the real group value to avoid misattributing those rows to the
// default group. One lightweight metadata query per operation, on the same
// connection that holds the advisory lock.
func (m *Migrator) hasGroupColumn(ctx context.Context, x connish, tbl string) (bool, error) {
	var n int
	if m.dialect == DialectPostgres {
		// Resolve via ::regclass (search_path-scoped, quoted-identifier
		// aware) like the PK queries — NOT information_schema.columns,
		// which spans every visible schema: a sibling schema's group-aware
		// table of the same name would false-positive and make the legacy
		// read path fail on a table that has no group_name column.
		q := "SELECT COUNT(*) FROM pg_attribute WHERE attrelid = " + m.placeholder(1) + "::regclass AND attname = 'group_name' AND NOT attisdropped"
		if err := x.QueryRowContext(ctx, q, tbl).Scan(&n); err != nil {
			return false, fmt.Errorf("migrate: detect group_name column: %w", err)
		}
	} else {
		baseName := strings.Trim(tbl, `"`)
		q := "SELECT COUNT(*) FROM pragma_table_info(" + m.placeholder(1) + ") WHERE name = 'group_name'"
		if err := x.QueryRowContext(ctx, q, baseName).Scan(&n); err != nil {
			return false, fmt.Errorf("migrate: detect group_name column: %w", err)
		}
	}
	return n > 0, nil
}

// appliedVersions returns the set of already-applied migrations, keyed by
// (Group, Version). In the legacy path every key has Group == "".
func (m *Migrator) appliedVersions(ctx context.Context, x connish, tbl string, ga bool) (map[migKey]MigrationRecord, error) {
	var q string
	if ga {
		q = fmt.Sprintf("SELECT version, name, applied_at, checksum, dirty, group_name FROM %s ORDER BY group_name, version", tbl)
	} else {
		q = fmt.Sprintf("SELECT version, name, applied_at, checksum, dirty FROM %s ORDER BY version", tbl)
	}
	rows, err := x.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[migKey]MigrationRecord)
	for rows.Next() {
		var rec MigrationRecord
		if ga {
			if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt, &rec.Checksum, &rec.Dirty, &rec.Group); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt, &rec.Checksum, &rec.Dirty); err != nil {
				return nil, err
			}
		}
		applied[migKey{Group: rec.Group, Version: rec.Version}] = rec
	}
	return applied, rows.Err()
}

// dirtyError wraps ErrDirty with a message naming the migration. The legacy
// message shape ("migration N (name): …") is byte-identical to pre-group code.
func dirtyError(rec MigrationRecord, ga bool) error {
	if ga {
		return fmt.Errorf("migration %d (%s) in group %q: %w", rec.Version, rec.Name, groupDisplayName(rec.Group), ErrDirty)
	}
	return fmt.Errorf("migration %d (%s): %w", rec.Version, rec.Name, ErrDirty)
}

// checkIntegrity refuses to proceed when the tracking state is unsafe:
//   - any migration is dirty (a no-transaction migration failed mid-apply), or
//   - an already-applied migration's recorded checksum no longer matches the
//     registered file (it was edited after being applied).
//
// A blank recorded checksum is skipped — those are legacy rows applied before
// checksums existed, and flagging them would be a false positive. Integrity is
// scoped per group: the (Group, Version) key is what matches a registered
// migration to its applied row. Applied rows whose group is not among the
// registered migrations (a de-registered module's rows) are ignored — they are
// another module's property — but are still shown by Status with their real
// group.
func (m *Migrator) checkIntegrity(applied map[migKey]MigrationRecord, ga bool) error {
	registered := make(map[string]bool, len(m.migrations))
	for _, mig := range m.migrations {
		registered[mig.Group] = true
	}
	for _, rec := range applied {
		// A NAMED group with no registered migrations is a disabled module —
		// its rows (dirty or not) are that module's property, ignored here.
		// The default group is never a module: its dirty rows always block,
		// preserving the pre-group safety net.
		if rec.Group != "" && !registered[rec.Group] {
			continue
		}
		if rec.Dirty {
			return dirtyError(rec, ga)
		}
	}
	for _, mig := range m.migrations {
		rec, ok := applied[migKey{Group: mig.Group, Version: mig.Version}]
		if !ok || rec.Checksum == "" {
			continue
		}
		if got := checksumOf(mig); got != rec.Checksum {
			return &ChecksumMismatchError{Version: mig.Version, Name: mig.Name, Recorded: rec.Checksum, Current: got}
		}
	}
	return nil
}

// pendingMigrations returns migrations that have not yet been applied, sorted
// by (Version, Group) ascending — the apply order. Within a group versions
// ascend; when multiple groups run together the tiebreak is group name
// (deterministic, and byte-identical to the old version-only sort when
// everything is in the default group).
func pendingMigrations(registered []Migration, applied map[migKey]MigrationRecord) []Migration {
	var pending []Migration
	for _, mig := range registered {
		if _, ok := applied[migKey{Group: mig.Group, Version: mig.Version}]; !ok {
			pending = append(pending, mig)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		if pending[i].Version != pending[j].Version {
			return pending[i].Version < pending[j].Version
		}
		return pending[i].Group < pending[j].Group
	})
	return pending
}

// Up runs all pending migrations in (Version, Group) order. Each migration
// executes in its own transaction. Already-applied migrations are skipped. The
// whole run is serialized by a database advisory lock so two instances (e.g.
// rolling-deploy replicas) cannot apply migrations concurrently.
//
// With no group arguments every registered group is applied (the existing
// behavior). Passing one or more group names limits the run to those groups'
// pending migrations only.
func (m *Migrator) Up(ctx context.Context, groups ...string) error {
	groups = normalizeGroupSelection(groups)
	if err := m.validateGroupSelection(groups, true); err != nil {
		return err
	}
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	ga := m.groupAware(groups)
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		return m.up(ctx, conn, tbl, ga, groups)
	})
}

func (m *Migrator) up(ctx context.Context, x connish, tbl string, ga bool, groups []string) error {
	if err := m.createMigrationsTable(ctx, x, tbl, ga); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	if ga {
		if err := m.ensureCompositeKey(ctx, x, tbl); err != nil {
			return err
		}
	}

	// Detect the table's actual shape for reads/writes. ga is the
	// registration signal (controls table creation + PK upgrade); tableGa
	// is the disk signal (controls SELECT/INSERT/DELETE shape). When ga is
	// true the table definitely has group_name (just created/upgraded), so
	// no query is needed. When ga is false the table might still have
	// group_name from a previous group-aware run (de-registered group), so
	// we check once.
	tableGa := ga
	if !ga {
		var err error
		tableGa, err = m.hasGroupColumn(ctx, x, tbl)
		if err != nil {
			return err
		}
	}

	applied, err := m.appliedVersions(ctx, x, tbl, tableGa)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	if err := m.checkIntegrity(applied, tableGa); err != nil {
		return err
	}

	pending := pendingMigrations(m.selectedMigrations(groups), applied)
	if len(pending) == 0 {
		return nil
	}

	for _, mig := range pending {
		if err := m.runMigrationUp(ctx, x, tbl, mig, tableGa); err != nil {
			return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationUp executes a single migration's Up SQL and records it in the
// tracking table. Transactional migrations run the DDL and the bookkeeping
// insert in one atomic transaction. No-transaction migrations record a dirty
// row first, run the DDL outside any transaction, then clear the dirty flag —
// so a failure leaves a dirty marker that blocks subsequent runs.
func (m *Migrator) runMigrationUp(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if mig.NoTransaction {
		return m.runMigrationUpNoTx(ctx, x, tbl, mig, ga)
	}

	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Up); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec up: %w", err)
	}

	if ga {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (group_name, version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, %s, FALSE)",
			tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4), m.placeholder(5),
		)
		if _, err := tx.ExecContext(ctx, insertSQL, mig.Group, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration: %w", err)
		}
	} else {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, FALSE)",
			tbl,
			m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
		)
		if _, err := tx.ExecContext(ctx, insertSQL, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration: %w", err)
		}
	}

	return tx.Commit()
}

// runMigrationUpNoTx applies a no-transaction migration. The dirty row is
// committed before the DDL runs so that a crash or failure mid-DDL is
// detectable on the next run.
func (m *Migrator) runMigrationUpNoTx(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if ga {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (group_name, version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, %s, TRUE)",
			tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4), m.placeholder(5),
		)
		if _, err := x.ExecContext(ctx, insertSQL, mig.Group, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
			return fmt.Errorf("record migration (dirty): %w", err)
		}
	} else {
		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, TRUE)",
			tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
		)
		if _, err := x.ExecContext(ctx, insertSQL, mig.Version, mig.Name, time.Now().UTC(), checksumOf(mig)); err != nil {
			return fmt.Errorf("record migration (dirty): %w", err)
		}
	}

	if _, err := x.ExecContext(ctx, mig.Up); err != nil {
		// Leave the dirty row in place — it's the signal that this migration
		// half-applied and the database needs manual reconciliation.
		return fmt.Errorf("exec up (no-transaction, left dirty): %w", err)
	}

	if ga {
		clearSQL := fmt.Sprintf("UPDATE %s SET dirty = FALSE WHERE group_name = %s AND version = %s", tbl, m.placeholder(1), m.placeholder(2))
		if _, err := x.ExecContext(ctx, clearSQL, mig.Group, mig.Version); err != nil {
			return fmt.Errorf("clear dirty flag: %w", err)
		}
	} else {
		clearSQL := fmt.Sprintf("UPDATE %s SET dirty = FALSE WHERE version = %s", tbl, m.placeholder(1))
		if _, err := x.ExecContext(ctx, clearSQL, mig.Version); err != nil {
			return fmt.Errorf("clear dirty flag: %w", err)
		}
	}
	return nil
}

// Down rolls back the last n applied migrations in reverse (Version, Group)
// order among the selected groups. Serialized by the same advisory lock as Up.
//
// With no group arguments every group is in scope (the existing behavior).
// Passing group names limits the rollback to applied migrations in those
// groups, preserving the most-recent-first semantics within the selection.
func (m *Migrator) Down(ctx context.Context, n int, groups ...string) error {
	groups = normalizeGroupSelection(groups)
	if err := m.validateGroupSelection(groups, true); err != nil {
		return err
	}
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	ga := m.groupAware(groups)
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		return m.down(ctx, conn, tbl, n, ga, groups)
	})
}

func (m *Migrator) down(ctx context.Context, x connish, tbl string, n int, ga bool, groups []string) error {
	if err := m.createMigrationsTable(ctx, x, tbl, ga); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}
	if ga {
		if err := m.ensureCompositeKey(ctx, x, tbl); err != nil {
			return err
		}
	}

	tableGa := ga
	if !ga {
		var err error
		tableGa, err = m.hasGroupColumn(ctx, x, tbl)
		if err != nil {
			return err
		}
	}

	applied, err := m.appliedVersions(ctx, x, tbl, tableGa)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	// A dirty database must be reconciled before any rollback too — running a
	// Down against a half-applied schema would compound the damage. Scoped to
	// REGISTERED groups, mirroring checkIntegrity: a de-registered (disabled)
	// module's dirty row is that module's property and must not brick another
	// group's rollback — reconcile it via Force(v, …, "<group>"). The DEFAULT
	// group is never a module: its rows always count (see ownGroup).
	registered := make(map[string]bool, len(m.migrations))
	for _, mig := range m.migrations {
		registered[mig.Group] = true
	}
	// ownGroup: the default group is always the app's own property; a named
	// group is ours only while at least one of its migrations is registered.
	ownGroup := func(g string) bool { return g == "" || registered[g] }
	for _, rec := range applied {
		if rec.Dirty && ownGroup(rec.Group) {
			return dirtyError(rec, tableGa)
		}
	}

	// Build sorted list of applied keys in the selected groups, descending by
	// (Version, Group) — most-recent-first within the selection. Rows of a
	// NAMED group with no registered migrations at all (a disabled module)
	// are not rollback candidates — an unscoped Down must not error on, or
	// roll back, another module's rows. The default group and any partially
	// registered group still hit the applied-but-not-registered error below:
	// within a group you own, a missing migration is drift, not modularity.
	var keys []migKey
	for k := range applied {
		if !groupSelected(k.Group, groups) {
			continue
		}
		if !ownGroup(k.Group) {
			continue // disabled module's row — not ours to roll back
		}
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Version != keys[j].Version {
			return keys[i].Version > keys[j].Version
		}
		return keys[i].Group > keys[j].Group
	})

	if n > len(keys) {
		n = len(keys)
	}

	// Build lookup from key to Migration.
	lookup := make(map[migKey]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		lookup[migKey{Group: mig.Group, Version: mig.Version}] = mig
	}

	for i := 0; i < n; i++ {
		mig, ok := lookup[keys[i]]
		if !ok {
			return fmt.Errorf("migration version %d in group %q is applied but not registered", keys[i].Version, groupDisplayName(keys[i].Group))
		}
		if err := m.runMigrationDown(ctx, x, tbl, mig, tableGa); err != nil {
			return fmt.Errorf("rollback migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationDown executes a single migration's Down SQL and removes its
// tracking row. Transactional migrations do both atomically. No-transaction
// migrations (the Down counterpart of CREATE INDEX CONCURRENTLY etc.) run the
// Down outside any transaction — the same protocol as runMigrationUpNoTx — and
// mark the row dirty first so a failed concurrent-DDL rollback is detectable.
func (m *Migrator) runMigrationDown(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if mig.NoTransaction {
		return m.runMigrationDownNoTx(ctx, x, tbl, mig, ga)
	}

	tx, err := x.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Down); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec down: %w", err)
	}

	if ga {
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE group_name = %s AND version = %s", tbl, m.placeholder(1), m.placeholder(2))
		if _, err := tx.ExecContext(ctx, deleteSQL, mig.Group, mig.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete migration record: %w", err)
		}
	} else {
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
		if _, err := tx.ExecContext(ctx, deleteSQL, mig.Version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("delete migration record: %w", err)
		}
	}

	return tx.Commit()
}

// runMigrationDownNoTx rolls back a no-transaction migration. The row is marked
// dirty before the Down runs, so a failure mid-DDL (e.g. a DROP INDEX
// CONCURRENTLY that errors partway) leaves a dirty marker that blocks later
// runs until reconciled; success removes the row.
func (m *Migrator) runMigrationDownNoTx(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if ga {
		markSQL := fmt.Sprintf("UPDATE %s SET dirty = TRUE WHERE group_name = %s AND version = %s", tbl, m.placeholder(1), m.placeholder(2))
		if _, err := x.ExecContext(ctx, markSQL, mig.Group, mig.Version); err != nil {
			return fmt.Errorf("mark dirty: %w", err)
		}
	} else {
		markSQL := fmt.Sprintf("UPDATE %s SET dirty = TRUE WHERE version = %s", tbl, m.placeholder(1))
		if _, err := x.ExecContext(ctx, markSQL, mig.Version); err != nil {
			return fmt.Errorf("mark dirty: %w", err)
		}
	}
	if _, err := x.ExecContext(ctx, mig.Down); err != nil {
		// Leave the dirty row — the rollback half-applied and needs a human.
		return fmt.Errorf("exec down (no-transaction, left dirty): %w", err)
	}
	if ga {
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE group_name = %s AND version = %s", tbl, m.placeholder(1), m.placeholder(2))
		if _, err := x.ExecContext(ctx, deleteSQL, mig.Group, mig.Version); err != nil {
			return fmt.Errorf("delete migration record: %w", err)
		}
	} else {
		deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
		if _, err := x.ExecContext(ctx, deleteSQL, mig.Version); err != nil {
			return fmt.Errorf("delete migration record: %w", err)
		}
	}
	return nil
}

// Force reconciles the tracking table by hand, the recovery path out of a
// dirty state and the way to adopt an existing database (baseline).
//
//   - applied == true: mark `version` as cleanly applied without running its
//     Up SQL. Inserts the row if missing (baseline), and clears any dirty flag
//     on it. If the version is registered, its name and checksum are recorded
//     so future drift checks line up.
//   - applied == false: remove `version` from the tracking table entirely, so
//     it is treated as pending again (e.g. a no-tx migration that did not
//     actually take effect).
//
// Either way the dirty state for that version is cleared, unblocking Up/Down.
//
// With no group argument the default group is targeted. Exactly one group
// argument targets that group; more than one is an error.
func (m *Migrator) Force(ctx context.Context, version uint64, applied bool, groups ...string) error {
	if len(groups) > 1 {
		return fmt.Errorf("migrate: Force accepts at most one group, got %d", len(groups))
	}
	groups = normalizeGroupSelection(groups)
	var group string
	if len(groups) == 1 {
		group = groups[0]
		if err := validateGroupName(group); err != nil {
			return err
		}
	}
	ga := m.groupAware(groups)
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return WithAdvisoryLock(ctx, m.db, m.dialect, func(conn *sql.Conn) error {
		if err := m.createMigrationsTable(ctx, conn, tbl, ga); err != nil {
			return fmt.Errorf("creating migrations table: %w", err)
		}
		if ga {
			if err := m.ensureCompositeKey(ctx, conn, tbl); err != nil {
				return err
			}
		}

		tableGa := ga
		if !ga {
			var derr error
			tableGa, derr = m.hasGroupColumn(ctx, conn, tbl)
			if derr != nil {
				return derr
			}
		}

		if !applied {
			if tableGa {
				del := fmt.Sprintf("DELETE FROM %s WHERE group_name = %s AND version = %s", tbl, m.placeholder(1), m.placeholder(2))
				_, err := conn.ExecContext(ctx, del, group, version)
				return err
			}
			del := fmt.Sprintf("DELETE FROM %s WHERE version = %s", tbl, m.placeholder(1))
			_, err := conn.ExecContext(ctx, del, version)
			return err
		}

		name, checksum := "forced", ""
		for _, mig := range m.migrations {
			if mig.Group == group && mig.Version == version {
				name, checksum = mig.Name, checksumOf(mig)
				break
			}
		}
		if tableGa {
			// Upsert a clean row. ON CONFLICT (group_name, version) works on
			// Postgres and SQLite >=3.24; it clears the dirty flag whether the
			// row exists or not.
			upsert := fmt.Sprintf(
				"INSERT INTO %s (group_name, version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, %s, FALSE) "+
					"ON CONFLICT (group_name, version) DO UPDATE SET dirty = FALSE",
				tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4), m.placeholder(5),
			)
			_, err = conn.ExecContext(ctx, upsert, group, version, name, time.Now().UTC(), checksum)
			return err
		}
		upsert := fmt.Sprintf(
			"INSERT INTO %s (version, name, applied_at, checksum, dirty) VALUES (%s, %s, %s, %s, FALSE) "+
				"ON CONFLICT (version) DO UPDATE SET dirty = FALSE",
			tbl, m.placeholder(1), m.placeholder(2), m.placeholder(3), m.placeholder(4),
		)
		_, err = conn.ExecContext(ctx, upsert, version, name, time.Now().UTC(), checksum)
		return err
	})
}

// Status returns the current migration state: which are applied and which are
// pending. With no group arguments every group is in scope (the existing
// behavior); passing group names scopes both lists to those groups.
//
// Status is read-only: it never upgrades the tracking-table PK (that is an
// unlocked check-then-act left to Up/Down/Force, which hold the advisory
// lock). It detects the table's actual shape and reads accordingly — legacy
// SELECT when the group_name column is absent, group-aware SELECT when it is
// present — so a de-registered group's rows are still shown with their real
// group.
func (m *Migrator) Status(ctx context.Context, groups ...string) (*Status, error) {
	groups = normalizeGroupSelection(groups)
	// Status is a read: selection is validated for syntax only, NOT against
	// the registered set — a de-registered (disabled) module's applied rows
	// are legitimately inspectable, and an unknown group simply reports an
	// empty status.
	if err := m.validateGroupSelection(groups, false); err != nil {
		return nil, err
	}
	tbl, err := m.qtable()
	if err != nil {
		return nil, err
	}
	// Status performs NO group-schema DDL: it keeps the pre-existing
	// lazy-create contract (legacy table shape when missing) but never adds
	// group_name or upgrades the PK — those are check-then-act operations
	// reserved for Up/Down/Force, which hold the advisory lock. Reads key
	// off the table's ACTUAL shape, so a table upgraded by a previous
	// group-aware run is read group-aware even here.
	if err := m.createMigrationsTable(ctx, m.db, tbl, false); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}
	tableGa, err := m.hasGroupColumn(ctx, m.db, tbl)
	if err != nil {
		return nil, err
	}

	applied, err := m.appliedVersions(ctx, m.db, tbl, tableGa)
	if err != nil {
		return nil, fmt.Errorf("querying applied migrations: %w", err)
	}

	// Build applied list sorted by (Version, Group), scoped to selected groups.
	var appliedList []MigrationRecord
	for _, rec := range applied {
		if !groupSelected(rec.Group, groups) {
			continue
		}
		appliedList = append(appliedList, rec)
	}
	sort.Slice(appliedList, func(i, j int) bool {
		if appliedList[i].Version != appliedList[j].Version {
			return appliedList[i].Version < appliedList[j].Version
		}
		return appliedList[i].Group < appliedList[j].Group
	})

	pending := pendingMigrations(m.selectedMigrations(groups), applied)

	return &Status{
		Applied: appliedList,
		Pending: pending,
	}, nil
}
