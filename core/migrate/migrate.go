package migrate

import (
	"bufio"
	"database/sql"
	"fmt"
	"github.com/DonaldMurillo/gofastr/internal/bom"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Migration represents a single versioned database migration.
type Migration struct {
	Version uint64
	Name    string
	Up      string
	Down    string

	// Group names the migration's stream; empty is the default group.
	Group string

	// NoTransaction runs Up and Down outside a transaction, recording the
	// row dirty until the SQL succeeds.
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

// WithDialect sets the SQL dialect; the default is DialectPostgres.
func WithDialect(d Dialect) Option {
	return func(m *Migrator) {
		m.dialect = d
	}
}

// New returns a Migrator on db whose tracking table defaults to _migrations.
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

var groupNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

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

const defaultGroupAlias = "default"

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

// ValidateGroupName reports whether name may name a migration group.
func ValidateGroupName(name string) error {
	return validateGroupName(name)
}

func groupDisplayName(g string) string {
	if g == "" {
		return "default"
	}
	return g
}

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

// Register adds mig, refusing an invalid group, an empty Up and a
// (Group, Version) already registered.
func (m *Migrator) Register(mig Migration) error {
	if err := validateGroupName(mig.Group); err != nil {
		return err
	}
	if strings.TrimSpace(mig.Up) == "" {
		return fmt.Errorf("migrate: migration %q (version %d in group %q) has no Up SQL; a migration that applies nothing must not be recorded applied", mig.Name, mig.Version, groupDisplayName(mig.Group))
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

// RegisterFromReader parses a "-- +migrate" directive file and registers it.
func (m *Migrator) RegisterFromReader(r io.Reader) error {
	mig, err := parseMigration(r)
	if err != nil {
		return err
	}
	return m.Register(*mig)
}

// IsDirectiveLine reports whether the parser reads line as a directive,
// a malformed one included.
func IsDirectiveLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "-- +migrate") || isNearMissDirective(trimmed)
}

func isNearMissDirective(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "--") {
		return false
	}
	rest := strings.TrimLeft(trimmed[2:], " \t")
	return len(rest) >= len("+migrate") && asciicase.Lower(rest[:len("+migrate")]) == "+migrate"
}

func parseMigration(r io.Reader) (*Migration, error) {
	scanner := bufio.NewScanner(r)
	var version uint64
	var name string
	var noTx bool
	var group string
	var upSQL, downSQL strings.Builder
	var section string
	upOpened := false
	orphanedSQL := false

	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			line, first = bom.Trim(line), false
		}
		trimmed := strings.TrimSpace(line)

		if after, ok := strings.CutPrefix(trimmed, "-- +migrate"); ok {
			directive := strings.TrimSpace(after)

			if after, ok := strings.CutPrefix(directive, "Version"); ok {
				vStr := strings.TrimSpace(after)
				v, err := strconv.ParseUint(vStr, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parsing migration version: %w", err)
				}
				version = v
			} else if after, ok := strings.CutPrefix(directive, "Name"); ok {
				name = strings.TrimSpace(after)
			} else if after, ok := strings.CutPrefix(directive, "Group"); ok {
				group = strings.TrimSpace(after)
				if err := validateGroupName(group); err != nil {
					return nil, err
				}
			} else if directive == "Up" {
				section = "up"
				upOpened = true
			} else if directive == "Down" {
				section = "down"
			} else if directive == "NoTransaction" {
				noTx = true
			} else {
				return nil, fmt.Errorf("unknown migration directive %q: the directives are -- +migrate Version, Name, Group, Up, Down and NoTransaction, spelled with that case", trimmed)
			}
			continue
		}
		if isNearMissDirective(trimmed) {
			return nil, fmt.Errorf("malformed migration directive %q: write it as -- +migrate <Directive>, one space after the dashes and a lower-case migrate", trimmed)
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
		default:
			if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
				orphanedSQL = true
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading migration: %w", err)
	}

	if version == 0 {
		return nil, fmt.Errorf("migration version is required (-- +migrate Version <n>)")
	}

	if !upOpened {
		return nil, fmt.Errorf("migration %q (version %d) has no Up section: the -- +migrate Up directive is missing or misspelled (directives are case-sensitive); refusing a migration whose SQL would be silently discarded", name, version)
	}
	if strings.TrimSpace(upSQL.String()) == "" && orphanedSQL {
		return nil, fmt.Errorf("migration %q (version %d) has an empty Up section but carries SQL lines outside any section (they would be silently discarded); move them below the -- +migrate Up directive", name, version)
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
