package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
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
	Checksum  string
	Dirty     bool
}

type migKey struct {
	Group   string
	Version uint64
}

// Status holds the result of querying migration state.
type Status struct {
	Applied []MigrationRecord
	Pending []Migration
	// Modified lists applied migrations whose Up no longer matches the
	// recorded checksum.
	Modified []MigrationRecord
	// Unregistered lists applied migrations of an owned group that are
	// not registered.
	Unregistered []MigrationRecord
}

func (m *Migrator) nowFunc() string {
	if m.dialect == DialectSQLite {
		return "CURRENT_TIMESTAMP"
	}
	return "NOW()"
}

func (m *Migrator) placeholder(n int) string {
	if m.dialect == DialectSQLite {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func (m *Migrator) trackingRowWhere(mig Migration, ga bool) (string, []any) {
	if ga {
		return "group_name = " + m.placeholder(1) + " AND version = " + m.placeholder(2),
			[]any{mig.Group, mig.Version}
	}
	return "version = " + m.placeholder(1), []any{mig.Version}
}

func recordColumns(mig Migration, checksum string, ga bool) (string, []any) {
	cols, args := "version, name, applied_at, checksum", []any{mig.Version, mig.Name, time.Now().UTC(), checksum}
	if ga {
		return "group_name, " + cols, append([]any{mig.Group}, args...)
	}
	return cols, args
}

func (m *Migrator) placeholders(n int) string {
	ph := make([]string, n)
	for i := range ph {
		ph[i] = m.placeholder(i + 1)
	}
	return strings.Join(ph, ", ")
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (m *Migrator) insertRecord(ctx context.Context, x execer, tbl string, mig Migration, ga, dirty bool) error {
	cols, args := recordColumns(mig, checksumOf(mig), ga)
	flag := "FALSE"
	if dirty {
		flag = "TRUE"
	}
	_, err := x.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s, dirty) VALUES (%s, %s)",
		tbl, cols, m.placeholders(len(args)), flag), args...)
	return err
}

func (m *Migrator) prepareTable(ctx context.Context, x connish, tbl string, ga bool) (bool, error) {
	if err := m.createMigrationsTable(ctx, x, tbl, ga); err != nil {
		return false, fmt.Errorf("creating migrations table: %w", err)
	}
	if !ga {
		return m.hasGroupColumn(ctx, x, tbl)
	}
	return true, m.ensureCompositeKey(ctx, x, tbl)
}

func (m *Migrator) ownsGroup() func(string) bool {
	registered := make(map[string]bool, len(m.migrations))
	for _, mig := range m.migrations {
		registered[mig.Group] = true
	}
	return func(g string) bool { return g == "" || registered[g] }
}

var testHookAfterMigration func(Migration)

type connish interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

func (m *Migrator) sqliteTableParts() (schema, table string) {
	if i := strings.LastIndexByte(m.tableName, '.'); i >= 0 {
		return m.tableName[:i], m.tableName[i+1:]
	}
	return "", m.tableName
}

func (m *Migrator) sqliteTableInfo() (string, []any) {
	schema, table := m.sqliteTableParts()
	if schema == "" {
		return "pragma_table_info(?)", []any{table}
	}
	return "pragma_table_info(?, ?)", []any{table, schema}
}

func (m *Migrator) qtable() (string, error) {
	safe, err := query.SafeIdent(m.tableName)
	if err != nil {
		return "", fmt.Errorf("migrate: invalid table name %q: %w", m.tableName, err)
	}
	return query.QuoteFolded(safe), nil
}

func (m *Migrator) hasNonDefaultGroup() bool {
	for _, mig := range m.migrations {
		if mig.Group != "" {
			return true
		}
	}
	return false
}

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

func groupSelected(g string, groups []string) bool {
	if len(groups) == 0 {
		return true
	}
	return slices.Contains(groups, g)
}

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

// CreateMigrationsTable creates the tracking table when it is missing.
func (m *Migrator) CreateMigrationsTable(ctx context.Context) error {
	tbl, err := m.qtable()
	if err != nil {
		return err
	}
	return m.createMigrationsTable(ctx, m.db, tbl, m.groupAware(nil))
}

func (m *Migrator) createMigrationsTable(ctx context.Context, x connish, tbl string, ga bool) error {
	if m.dialect == DialectPostgres {
		if err := query.RefuseCaseKeptTables(ctx, x, m.tableName); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
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
	return m.ensureTrackingColumns(ctx, x, tbl, ga)
}

func (m *Migrator) hasGroupColumn(ctx context.Context, x connish, tbl string) (bool, error) {
	var n int
	if m.dialect == DialectPostgres {
		q := "SELECT COUNT(*) FROM pg_attribute WHERE attrelid = " + m.placeholder(1) + "::regclass AND attname = 'group_name' AND NOT attisdropped"
		if err := x.QueryRowContext(ctx, q, tbl).Scan(&n); err != nil {
			return false, fmt.Errorf("migrate: detect group_name column: %w", err)
		}
	} else {
		info, args := m.sqliteTableInfo()
		q := "SELECT COUNT(*) FROM " + info + " WHERE name = 'group_name'"
		if err := x.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			return false, fmt.Errorf("migrate: detect group_name column: %w", err)
		}
	}
	return n > 0, nil
}

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

func dirtyError(rec MigrationRecord, ga bool) error {
	if ga {
		return fmt.Errorf("migration %d (%s) in group %q: %w", rec.Version, rec.Name, groupDisplayName(rec.Group), ErrDirty)
	}
	return fmt.Errorf("migration %d (%s): %w", rec.Version, rec.Name, ErrDirty)
}

func noDownError(mig Migration, ga bool) error {
	if ga {
		return fmt.Errorf("migration %d (%s) in group %q: %w", mig.Version, mig.Name, groupDisplayName(mig.Group), ErrNoDown)
	}
	return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, ErrNoDown)
}

func (m *Migrator) checkIntegrity(applied map[migKey]MigrationRecord, ga bool) error {
	owns := m.ownsGroup()
	for _, rec := range applied {
		if rec.Dirty && owns(rec.Group) {
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

func pendingMigrations(registered []Migration, applied map[migKey]MigrationRecord) []Migration {
	var pending []Migration
	for _, mig := range registered {
		if _, ok := applied[migKey{Group: mig.Group, Version: mig.Version}]; !ok {
			pending = append(pending, mig)
		}
	}
	return pending
}

// Up applies the pending migrations of groups (all when none) under the
// migration lock, each in its own transaction.
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
	return withLeasedLock(ctx, m.db, m.dialect, AdvisoryLockKey, func(conn *sql.Conn, keep func(context.Context) error) error {
		return m.up(ctx, conn, tbl, ga, groups, keep)
	})
}

func (m *Migrator) up(ctx context.Context, x connish, tbl string, ga bool, groups []string, keep func(context.Context) error) error {
	tableGa, err := m.prepareTable(ctx, x, tbl, ga)
	if err != nil {
		return err
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
		if err := keep(ctx); err != nil {
			return fmt.Errorf("migrate lock: %w before migration %d (%s)", err, mig.Version, mig.Name)
		}
		if err := m.runMigrationUp(ctx, x, tbl, mig, tableGa); err != nil {
			return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, err)
		}
		if testHookAfterMigration != nil {
			testHookAfterMigration(mig)
		}
	}

	return nil
}

func (m *Migrator) migrationRecorded(ctx context.Context, x connish, tbl string, mig Migration, ga bool) (recorded, dirty bool, err error) {
	where, args := m.trackingRowWhere(mig, ga)
	err = x.QueryRowContext(ctx, "SELECT dirty FROM "+tbl+" WHERE "+where, args...).Scan(&dirty)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, dirty, nil
}

func (m *Migrator) runMigrationUp(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if m.dialect == DialectSQLite {
		recorded, dirty, err := m.migrationRecorded(ctx, x, tbl, mig, ga)
		if err != nil {
			return fmt.Errorf("recheck applied: %w", err)
		}
		if recorded {
			if dirty {
				return dirtyError(MigrationRecord{Version: mig.Version, Name: mig.Name, Group: mig.Group}, ga)
			}
			return nil
		}
	}
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

	if err := m.insertRecord(ctx, tx, tbl, mig, ga, false); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}

func (m *Migrator) runMigrationUpNoTx(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	if err := m.insertRecord(ctx, x, tbl, mig, ga, true); err != nil {
		return fmt.Errorf("record migration (dirty): %w", err)
	}

	if err := m.execNoTx(ctx, x, mig.Up); err != nil {
		return fmt.Errorf("exec up (no-transaction, left dirty): %w", err)
	}

	where, args := m.trackingRowWhere(mig, ga)
	if _, err := x.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET dirty = FALSE WHERE "+where, tbl), args...); err != nil {
		return fmt.Errorf("clear dirty flag: %w", err)
	}
	return nil
}

// Down rolls back the last n applied migrations of groups (all when
// none), newest first, under the migration lock.
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
	return withLeasedLock(ctx, m.db, m.dialect, AdvisoryLockKey, func(conn *sql.Conn, keep func(context.Context) error) error {
		return m.down(ctx, conn, tbl, n, ga, groups, keep)
	})
}

func (m *Migrator) down(ctx context.Context, x connish, tbl string, n int, ga bool, groups []string, keep func(context.Context) error) error {
	tableGa, err := m.prepareTable(ctx, x, tbl, ga)
	if err != nil {
		return err
	}

	applied, err := m.appliedVersions(ctx, x, tbl, tableGa)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}
	ownGroup := m.ownsGroup()
	for _, rec := range applied {
		if rec.Dirty && ownGroup(rec.Group) {
			return dirtyError(rec, tableGa)
		}
	}

	var keys []migKey
	for k := range applied {
		if !groupSelected(k.Group, groups) {
			continue
		}
		if !ownGroup(k.Group) {
			continue
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

	lookup := make(map[migKey]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		lookup[migKey{Group: mig.Group, Version: mig.Version}] = mig
	}

	for i := 0; i < n; i++ {
		if mig, ok := lookup[keys[i]]; ok && strings.TrimSpace(mig.Down) == "" {
			return noDownError(mig, tableGa)
		}
	}

	for i := 0; i < n; i++ {
		mig, ok := lookup[keys[i]]
		if !ok {
			return fmt.Errorf("migration version %d in group %q is applied but not registered", keys[i].Version, groupDisplayName(keys[i].Group))
		}
		if err := keep(ctx); err != nil {
			return fmt.Errorf("migrate lock: %w before rollback %d (%s)", err, mig.Version, mig.Name)
		}
		if err := m.runMigrationDown(ctx, x, tbl, mig, tableGa); err != nil {
			return fmt.Errorf("rollback migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

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

	where, args := m.trackingRowWhere(mig, ga)
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE "+where, args...); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete migration record: %w", err)
	}

	return tx.Commit()
}

func (m *Migrator) runMigrationDownNoTx(ctx context.Context, x connish, tbl string, mig Migration, ga bool) error {
	where, args := m.trackingRowWhere(mig, ga)
	if _, err := x.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET dirty = TRUE WHERE "+where, tbl), args...); err != nil {
		return fmt.Errorf("mark dirty: %w", err)
	}
	if err := m.execNoTx(ctx, x, mig.Down); err != nil {
		return fmt.Errorf("exec down (no-transaction, left dirty): %w", err)
	}
	if _, err := x.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE "+where, args...); err != nil {
		return fmt.Errorf("delete migration record: %w", err)
	}
	return nil
}

// Force marks version of the group (default when none) cleanly applied
// with its registered name and checksum, or removes its row when applied
// is false. It errors on more than one group.
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
		tableGa, err := m.prepareTable(ctx, conn, tbl, ga)
		if err != nil {
			return err
		}

		if !applied {
			where, args := m.trackingRowWhere(Migration{Group: group, Version: version}, tableGa)
			_, err := conn.ExecContext(ctx, "DELETE FROM "+tbl+" WHERE "+where, args...)
			return err
		}

		name, checksum := "forced", ""
		for _, mig := range m.migrations {
			if mig.Group == group && mig.Version == version {
				name, checksum = mig.Name, checksumOf(mig)
				break
			}
		}
		cols, args := recordColumns(Migration{Group: group, Version: version, Name: name}, checksum, tableGa)
		key := "version"
		if tableGa {
			key = "group_name, version"
		}
		upsert := fmt.Sprintf("INSERT INTO %s (%s, dirty) VALUES (%s, FALSE) "+
			"ON CONFLICT (%s) DO UPDATE SET dirty = FALSE, name = EXCLUDED.name, checksum = EXCLUDED.checksum",
			tbl, cols, m.placeholders(len(args)), key)
		_, err = conn.ExecContext(ctx, upsert, args...)
		return err
	})
}

// Status reports the applied, pending, modified and unregistered
// migrations of groups (all when none) without changing the table's shape.
func (m *Migrator) Status(ctx context.Context, groups ...string) (*Status, error) {
	groups = normalizeGroupSelection(groups)
	if err := m.validateGroupSelection(groups, false); err != nil {
		return nil, err
	}
	tbl, err := m.qtable()
	if err != nil {
		return nil, err
	}
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

	registered := make(map[migKey]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		registered[migKey{Group: mig.Group, Version: mig.Version}] = mig
	}
	owns := m.ownsGroup()
	var modified, unregistered []MigrationRecord
	for _, rec := range appliedList {
		mig, ok := registered[migKey{Group: rec.Group, Version: rec.Version}]
		switch {
		case ok && rec.Checksum != "" && checksumOf(mig) != rec.Checksum:
			modified = append(modified, rec)
		case !ok && owns(rec.Group):
			unregistered = append(unregistered, rec)
		}
	}

	return &Status{
		Applied:      appliedList,
		Pending:      pending,
		Modified:     modified,
		Unregistered: unregistered,
	}, nil
}
