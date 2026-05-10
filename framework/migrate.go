package framework

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/gofastr/gofastr/core/migrate"
	"github.com/gofastr/gofastr/core/schema"
)

// Dialect identifies the SQL dialect the migrator emits for. It's an alias
// for migrate.Dialect so framework code and the lower-level migration system
// share one source of truth for dialect identity.
type Dialect = migrate.Dialect

// Dialect identifiers re-exported from core/migrate for ergonomic use inside
// the framework package and in tests.
const (
	DialectSQLite   = migrate.DialectSQLite
	DialectPostgres = migrate.DialectPostgres
)

// detectDialect returns the dialect of an open *sql.DB. It probes for
// PostgreSQL via SELECT version() (cheap, no side effects) and falls back to
// SQLite when that fails. The probe runs once per AutoMigrate call.
func detectDialect(db *sql.DB) Dialect {
	var v string
	if err := db.QueryRow("SELECT version()").Scan(&v); err == nil {
		if strings.Contains(strings.ToLower(v), "postgresql") {
			return DialectPostgres
		}
	}
	return DialectSQLite
}

// AutoMigrate creates tables for all registered entities. Entities are
// migrated in dependency order so FK targets exist before referencing
// tables. Uses CREATE TABLE IF NOT EXISTS so re-running is safe.
func AutoMigrate(db *sql.DB, registry *Registry) error {
	all := registry.All()
	ordered, err := topoSortEntities(all)
	if err != nil {
		return err
	}
	dialect := detectDialect(db)
	for _, entity := range ordered {
		if err := migrateEntityWithRegistry(db, entity, all, dialect); err != nil {
			return fmt.Errorf("migrate %s: %w", entity.GetName(), err)
		}
	}
	return nil
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
// It does not emit FK constraints since it has no view of the wider registry;
// callers that need foreign keys should call AutoMigrate. The dialect is
// auto-detected from db.
func MigrateEntity(db *sql.DB, entity *Entity) error {
	return migrateEntityWithRegistry(db, entity, nil, detectDialect(db))
}

// MigrateEntityDialect is the explicit-dialect variant used by callers that
// already know the target (e.g. CLI codegen, tests).
func MigrateEntityDialect(db *sql.DB, entity *Entity, dialect Dialect) error {
	return migrateEntityWithRegistry(db, entity, nil, dialect)
}

// migrateEntityWithRegistry is the shared implementation. When `all` is
// non-nil it is consulted for FK target tables; missing targets return an
// error before any DDL runs.
func migrateEntityWithRegistry(db *sql.DB, entity *Entity, all map[string]*Entity, dialect Dialect) error {
	fields := entity.GetFields()
	if len(fields) == 0 {
		return nil
	}

	var columns []string
	for _, f := range fields {
		col := fmt.Sprintf("%s %s", f.Name, sqlType(f, dialect))
		if f.Name == entity.PrimaryKey {
			col += " PRIMARY KEY"
		}
		if f.Unique {
			col += " UNIQUE"
		}
		if f.Required && f.AutoGenerate == schema.AutoNone {
			col += " NOT NULL"
		}
		if f.Default != nil {
			col += fmt.Sprintf(" DEFAULT %v", sqlDefault(f, dialect))
		}
		columns = append(columns, col)
	}

	if all != nil {
		fks, err := foreignKeyClauses(entity, all)
		if err != nil {
			return err
		}
		columns = append(columns, fks...)
	}

	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		entity.GetTable(),
		strings.Join(columns, ",\n\t"),
	)

	_, err := db.Exec(stmt)
	return err
}

// foreignKeyClauses produces "FOREIGN KEY (col) REFERENCES target(id)"
// fragments for every BelongsTo relation declared on the entity. Targets
// must exist in `all` or the function returns an error.
func foreignKeyClauses(entity *Entity, all map[string]*Entity) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	for _, rel := range entity.Config.Relations {
		if rel.Type != RelManyToOne || rel.ForeignKey == "" {
			continue
		}
		if seen[rel.ForeignKey] {
			continue
		}
		seen[rel.ForeignKey] = true
		target, ok := all[rel.Entity]
		if !ok {
			return nil, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
		}
		out = append(out, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)",
			rel.ForeignKey, target.GetTable(), target.PrimaryKey))
	}
	return out, nil
}

// topoSortEntities orders entities so referenced tables come before their
// referencers. Cycles are broken by name-sorted insertion at the cycle
// detection point — this is conservative; SQLite tolerates forward refs in
// CREATE TABLE because FK enforcement is per-statement, not at create time.
func topoSortEntities(all map[string]*Entity) ([]*Entity, error) {
	// Stable input order
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	out := make([]*Entity, 0, len(all))

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if tempMark[name] {
			return nil // cycle — break it; safe for IF NOT EXISTS DDL
		}
		ent, ok := all[name]
		if !ok {
			return nil
		}
		tempMark[name] = true
		for _, rel := range ent.Config.Relations {
			if rel.Type == RelManyToOne {
				if _, ok := all[rel.Entity]; !ok {
					return fmt.Errorf("entity %q has BelongsTo to unknown entity %q", name, rel.Entity)
				}
				if err := visit(rel.Entity); err != nil {
					return err
				}
			}
		}
		tempMark[name] = false
		visited[name] = true
		out = append(out, ent)
		return nil
	}

	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sqlType maps a schema FieldType to a SQL column type for the given dialect.
// Postgres needs concrete types (TIMESTAMPTZ, REAL, BOOLEAN); SQLite is more
// permissive but still benefits from explicit declarations.
func sqlType(f schema.Field, dialect Dialect) string {
	switch f.Type {
	case schema.String:
		if f.Max != nil && *f.Max > 0 {
			return fmt.Sprintf("VARCHAR(%d)", int(*f.Max))
		}
		return "TEXT"
	case schema.Text:
		return "TEXT"
	case schema.Int:
		return "INTEGER"
	case schema.Float:
		if dialect == DialectPostgres {
			return "DOUBLE PRECISION"
		}
		return "REAL"
	case schema.Bool:
		return "BOOLEAN"
	case schema.Decimal:
		return "DECIMAL(19,4)"
	case schema.Enum:
		return "TEXT"
	case schema.UUID:
		return "TEXT"
	case schema.Timestamp:
		if dialect == DialectPostgres {
			return "TIMESTAMPTZ"
		}
		return "DATETIME"
	case schema.Date:
		return "DATE"
	case schema.JSON:
		if dialect == DialectPostgres {
			return "JSONB"
		}
		return "TEXT"
	case schema.Relation:
		return "TEXT"
	case schema.Image:
		return "TEXT"
	case schema.File:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// sqlDefault returns the SQL DEFAULT value for a field. Booleans render as
// TRUE/FALSE for Postgres and 1/0 for SQLite (both engines accept either,
// but emitting the native form keeps DDL idiomatic and avoids surprises in
// pg_dump output).
func sqlDefault(f schema.Field, dialect Dialect) string {
	switch v := f.Default.(type) {
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case bool:
		if dialect == DialectPostgres {
			if v {
				return "TRUE"
			}
			return "FALSE"
		}
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("'%v'", v)
	}
}
