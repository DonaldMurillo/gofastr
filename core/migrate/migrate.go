package migrate

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Migration represents a single versioned database migration.
type Migration struct {
	Version uint64
	Name    string
	Up      string // SQL to apply the migration
	Down    string // SQL to roll back the migration
}

// Dialect represents the SQL dialect to use for migration queries.
type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectSQLite   Dialect = "sqlite3"
)

// Option configures a Migrator.
type Option func(*Migrator)

// WithTableName sets a custom name for the migrations tracking table.
func WithTableName(name string) Option {
	return func(m *Migrator) {
		m.tableName = name
	}
}

// Migrator manages database migrations.
type Migrator struct {
	db         *sql.DB
	migrations []Migration
	tableName  string
	dialect    Dialect
}

// WithDialect sets the SQL dialect for the migrator.
// Defaults to DialectPostgres if not specified.
func WithDialect(d Dialect) Option {
	return func(m *Migrator) {
		m.dialect = d
	}
}

// New creates a new Migrator with the given database connection and options.
// By default the tracking table is "_migrations".
func New(db *sql.DB, opts ...Option) *Migrator {
	m := &Migrator{
		db:      db,
		dialect: DialectPostgres,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.tableName == "" {
		m.tableName = "_migrations"
	}
	return m
}

// Register adds a migration. Migrations are kept sorted by Version.
func (m *Migrator) Register(mig Migration) {
	m.migrations = append(m.migrations, mig)
	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})
}

// RegisterFromReader parses migration SQL from a reader and registers it.
// The expected format uses comment directives:
//
//	-- +migrate Version <n>
//	-- +migrate Name <description>
//	-- +migrate Up
//	<up SQL statements>
//	-- +migrate Down
//	<down SQL statements>
func (m *Migrator) RegisterFromReader(r io.Reader) error {
	mig, err := parseMigration(r)
	if err != nil {
		return err
	}
	m.Register(*mig)
	return nil
}

// parseMigration reads a migration from a reader using comment directives.
func parseMigration(r io.Reader) (*Migration, error) {
	scanner := bufio.NewScanner(r)
	var version uint64
	var name string
	var upSQL, downSQL strings.Builder
	var section string // "up" or "down"

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "-- +migrate") {
			directive := strings.TrimSpace(strings.TrimPrefix(trimmed, "-- +migrate"))

			if strings.HasPrefix(directive, "Version") {
				vStr := strings.TrimSpace(strings.TrimPrefix(directive, "Version"))
				v, err := strconv.ParseUint(vStr, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parsing migration version: %w", err)
				}
				version = v
			} else if strings.HasPrefix(directive, "Name") {
				name = strings.TrimSpace(strings.TrimPrefix(directive, "Name"))
			} else if directive == "Up" {
				section = "up"
			} else if directive == "Down" {
				section = "down"
			}
			continue
		}

		switch section {
		case "up":
			if upSQL.Len() > 0 {
				upSQL.WriteString("\n")
			}
			upSQL.WriteString(line)
		case "down":
			if downSQL.Len() > 0 {
				downSQL.WriteString("\n")
			}
			downSQL.WriteString(line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading migration: %w", err)
	}

	if version == 0 {
		return nil, fmt.Errorf("migration version is required (-- +migrate Version <n>)")
	}

	return &Migration{
		Version: version,
		Name:    name,
		Up:      strings.TrimSpace(upSQL.String()),
		Down:    strings.TrimSpace(downSQL.String()),
	}, nil
}
