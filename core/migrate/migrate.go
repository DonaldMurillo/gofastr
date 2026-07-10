package migrate

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
	"regexp"
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

	// Group is the migration group this migration belongs to. An empty value
	// means the DEFAULT group — the single flat, version-ordered list every
	// existing app uses. A non-empty group lets an optional feature or module
	// own an independent migration stream that can be applied selectively
	// (e.g. apply the knowledge module's migrations only when that module is
	// enabled). Version uniqueness is scoped PER GROUP: two groups may both
	// have a version 1. Set via the `-- +migrate Group <name>` directive.
	Group string

	// NoTransaction runs Up/Down WITHOUT wrapping them in a transaction. The
	// escape hatch for statements that cannot run inside a transaction —
	// CREATE INDEX CONCURRENTLY, VACUUM, CREATE DATABASE on Postgres. The cost
	// is that a failure mid-statement leaves the database partially migrated:
	// the runner records the migration dirty before running it and only clears
	// that flag on success, so a later run refuses to proceed (ErrDirty) until
	// an operator reconciles and calls Force. Set via the `-- +migrate
	// NoTransaction` directive in a SQL migration file.
	NoTransaction bool
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

// groupNameRe limits group names to identifier-safe characters: letters,
// digits, underscores, and hyphens. Non-empty, capped at 64 runes so a group
// name never overflows an identifier slot.
var groupNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validateGroupName accepts the empty string (the DEFAULT group) or a
// non-empty name matching groupNameRe. It is the single chokepoint for group
// naming at both parse and Register time. The literal name "default" is
// RESERVED: it is how the default group renders in every human-facing message
// and how selections address it (see normalizeGroupSelection), so a group
// registered under that literal name would be unaddressable.
func validateGroupName(name string) error {
	if name == "" {
		return nil
	}
	if name == defaultGroupAlias {
		return fmt.Errorf("migrate: group name %q is reserved — it addresses the default group; leave Group empty instead", defaultGroupAlias)
	}
	if !groupNameRe.MatchString(name) {
		return fmt.Errorf("migrate: invalid group name %q (use letters, digits, '_' or '-', max 64 chars)", name)
	}
	return nil
}

// defaultGroupAlias is the human-facing name of the default ("") group. It
// is accepted in selections (Up/Down/Status/Force group args, the CLI's
// --group flag) and rejected as a registered group name.
const defaultGroupAlias = "default"

// normalizeGroupSelection maps the "default" alias to the empty (default)
// group in a selection, so `--group=default` / Up(ctx, "default") address
// the default group — otherwise the CLI has no syntax for it at all.
func normalizeGroupSelection(groups []string) []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		if g == defaultGroupAlias {
			g = ""
		}
		out[i] = g
	}
	return out
}

// ValidateGroupName is the exported form of validateGroupName, for callers
// outside the package (e.g. the CLI generate command validating --group
// before writing a migration file).
func ValidateGroupName(name string) error {
	return validateGroupName(name)
}

// groupDisplayName renders a group name for human-facing messages, showing
// "default" for the empty (default) group.
func groupDisplayName(g string) string {
	if g == "" {
		return "default"
	}
	return g
}

// validateGroupSelection checks that every group name in the selection is
// syntactically valid and — when mustBeRegistered is true — matches at least
// one registered migration's group. An empty selection (no args) means "all
// groups" and always passes. An unknown group (e.g. a typo) returns a
// descriptive error naming the known groups, instead of silently no-op-ing.
//
// Up and Down require registration: applying or rolling back needs the
// migration's SQL, and a typo silently doing nothing is the failure mode
// this guards. Status passes mustBeRegistered=false — it is a read, and a
// de-registered (disabled) module's rows are still legitimately inspectable
// (an unknown group then simply shows an empty status). Force skips this
// entirely: it is the escape hatch for reconciling exactly the rows no
// registered migration describes.
func (m *Migrator) validateGroupSelection(groups []string, mustBeRegistered bool) error {
	if len(groups) == 0 {
		return nil
	}
	registered := make(map[string]bool, len(m.migrations))
	for _, mig := range m.migrations {
		registered[mig.Group] = true
	}
	for _, g := range groups {
		if err := validateGroupName(g); err != nil {
			return err
		}
		if mustBeRegistered && !registered[g] {
			known := make([]string, 0, len(registered))
			for g := range registered {
				known = append(known, groupDisplayName(g))
			}
			sort.Strings(known)
			return fmt.Errorf("migrate: unknown group %q (known: %s)", groupDisplayName(g), strings.Join(known, ", "))
		}
	}
	return nil
}

// Register adds a migration, rejecting an invalid group name or a duplicate
// (Group, Version) pair. Migrations are kept sorted by (Version, Group) so the
// pending list is already in apply order: within a group versions ascend, and
// when multiple groups run together the tiebreak is group name — deterministic
// and byte-identical to the pre-group sort when everything is in the default
// group. A returned error does not mutate the migrator.
func (m *Migrator) Register(mig Migration) error {
	if err := validateGroupName(mig.Group); err != nil {
		return err
	}
	for _, e := range m.migrations {
		if e.Group == mig.Group && e.Version == mig.Version {
			return fmt.Errorf("migrate: duplicate migration version %d in group %q", mig.Version, groupDisplayName(mig.Group))
		}
	}
	m.migrations = append(m.migrations, mig)
	sort.Slice(m.migrations, func(i, j int) bool {
		if m.migrations[i].Version != m.migrations[j].Version {
			return m.migrations[i].Version < m.migrations[j].Version
		}
		return m.migrations[i].Group < m.migrations[j].Group
	})
	return nil
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
	return m.Register(*mig)
}

// parseMigration reads a migration from a reader using comment directives.
func parseMigration(r io.Reader) (*Migration, error) {
	scanner := bufio.NewScanner(r)
	var version uint64
	var name string
	var noTx bool
	var group string
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
			} else if strings.HasPrefix(directive, "Group") {
				group = strings.TrimSpace(strings.TrimPrefix(directive, "Group"))
				if err := validateGroupName(group); err != nil {
					return nil, err
				}
			} else if directive == "Up" {
				section = "up"
			} else if directive == "Down" {
				section = "down"
			} else if directive == "NoTransaction" {
				noTx = true
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
		Version:       version,
		Name:          name,
		Up:            strings.TrimSpace(upSQL.String()),
		Down:          strings.TrimSpace(downSQL.String()),
		Group:         group,
		NoTransaction: noTx,
	}, nil
}
