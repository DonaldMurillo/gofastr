package migrate

import (
	"context"

	"fmt"
	"sort"
	"time"
)

// MigrationRecord is a row in the _migrations tracking table.
type MigrationRecord struct {
	Version   uint64
	Name      string
	AppliedAt time.Time
}

// Status holds the result of querying migration state.
type Status struct {
	Applied []MigrationRecord
	Pending []Migration
}

// CreateMigrationsTable ensures the migrations tracking table exists.
func (m *Migrator) CreateMigrationsTable(ctx context.Context) error {
	ddl := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		version BIGINT NOT NULL PRIMARY KEY,
		name    TEXT    NOT NULL DEFAULT '',
		applied_at TIMESTAMP NOT NULL DEFAULT NOW()
	)`, m.tableName)
	_, err := m.db.ExecContext(ctx, ddl)
	return err
}

// appliedVersions returns the set of already-applied migration versions.
func (m *Migrator) appliedVersions(ctx context.Context) (map[uint64]MigrationRecord, error) {
	query := fmt.Sprintf("SELECT version, name, applied_at FROM %s ORDER BY version", m.tableName)
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[uint64]MigrationRecord)
	for rows.Next() {
		var rec MigrationRecord
		if err := rows.Scan(&rec.Version, &rec.Name, &rec.AppliedAt); err != nil {
			return nil, err
		}
		applied[rec.Version] = rec
	}
	return applied, rows.Err()
}

// pendingMigrations returns migrations that have not yet been applied,
// sorted by version ascending.
func pendingMigrations(registered []Migration, applied map[uint64]MigrationRecord) []Migration {
	var pending []Migration
	for _, mig := range registered {
		if _, ok := applied[mig.Version]; !ok {
			pending = append(pending, mig)
		}
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Version < pending[j].Version
	})
	return pending
}

// Up runs all pending migrations in version order.
// Each migration executes in its own transaction.
// Already-applied migrations are skipped.
func (m *Migrator) Up(ctx context.Context) error {
	if err := m.CreateMigrationsTable(ctx); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}

	pending := pendingMigrations(m.migrations, applied)
	if len(pending) == 0 {
		return nil
	}

	for _, mig := range pending {
		if err := m.runMigrationUp(ctx, mig); err != nil {
			return fmt.Errorf("migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationUp executes a single migration's Up SQL inside a transaction
// and records it in the tracking table.
func (m *Migrator) runMigrationUp(ctx context.Context, mig Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Up); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec up: %w", err)
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s (version, name, applied_at) VALUES ($1, $2, $3)",
		m.tableName,
	)
	if _, err := tx.ExecContext(ctx, insertSQL, mig.Version, mig.Name, time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}

// Down rolls back the last n applied migrations in reverse version order.
func (m *Migrator) Down(ctx context.Context, n int) error {
	if err := m.CreateMigrationsTable(ctx); err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("querying applied migrations: %w", err)
	}

	// Build sorted list of applied versions in descending order.
	var versions []uint64
	for v := range applied {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j]
	})

	if n > len(versions) {
		n = len(versions)
	}

	// Build lookup from version to Migration.
	lookup := make(map[uint64]Migration, len(m.migrations))
	for _, mig := range m.migrations {
		lookup[mig.Version] = mig
	}

	for i := 0; i < n; i++ {
		v := versions[i]
		mig, ok := lookup[v]
		if !ok {
			return fmt.Errorf("migration version %d is applied but not registered", v)
		}
		if err := m.runMigrationDown(ctx, mig); err != nil {
			return fmt.Errorf("rollback migration %d (%s): %w", mig.Version, mig.Name, err)
		}
	}

	return nil
}

// runMigrationDown executes a single migration's Down SQL inside a transaction
// and removes its record from the tracking table.
func (m *Migrator) runMigrationDown(ctx context.Context, mig Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if _, err := tx.ExecContext(ctx, mig.Down); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("exec down: %w", err)
	}

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE version = $1", m.tableName)
	if _, err := tx.ExecContext(ctx, deleteSQL, mig.Version); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("delete migration record: %w", err)
	}

	return tx.Commit()
}

// Status returns the current migration state: which are applied and which are pending.
func (m *Migrator) Status(ctx context.Context) (*Status, error) {
	if err := m.CreateMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("querying applied migrations: %w", err)
	}

	// Build applied list sorted by version.
	var appliedList []MigrationRecord
	for _, rec := range applied {
		appliedList = append(appliedList, rec)
	}
	sort.Slice(appliedList, func(i, j int) bool {
		return appliedList[i].Version < appliedList[j].Version
	})

	pending := pendingMigrations(m.migrations, applied)

	return &Status{
		Applied: appliedList,
		Pending: pending,
	}, nil
}
