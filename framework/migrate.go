package framework

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
)

// AutoMigrate creates tables for all registered entities. Entities are
// migrated in dependency order so FK targets exist before referencing
// tables. Uses CREATE TABLE IF NOT EXISTS so re-running is safe.
func AutoMigrate(db *sql.DB, registry *Registry) error {
	all := registry.All()
	ordered, err := topoSortEntities(all)
	if err != nil {
		return err
	}
	for _, entity := range ordered {
		if err := migrateEntityWithRegistry(db, entity, all); err != nil {
			return fmt.Errorf("migrate %s: %w", entity.GetName(), err)
		}
	}
	return nil
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
// It does not emit FK constraints since it has no view of the wider registry;
// callers that need foreign keys should call AutoMigrate.
func MigrateEntity(db *sql.DB, entity *Entity) error {
	return migrateEntityWithRegistry(db, entity, nil)
}

// migrateEntityWithRegistry is the shared implementation. When `all` is
// non-nil it is consulted for FK target tables; missing targets return an
// error before any DDL runs.
func migrateEntityWithRegistry(db *sql.DB, entity *Entity, all map[string]*Entity) error {
	fields := entity.GetFields()
	if len(fields) == 0 {
		return nil
	}

	var columns []string
	for _, f := range fields {
		col := fmt.Sprintf("%s %s", f.Name, sqlType(f))
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
			col += fmt.Sprintf(" DEFAULT %v", sqlDefault(f))
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

// sqlType maps a schema FieldType to a SQL column type.
func sqlType(f schema.Field) string {
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
		return "DATETIME"
	case schema.Date:
		return "DATE"
	case schema.JSON:
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

// sqlDefault returns the SQL DEFAULT value for a field.
func sqlDefault(f schema.Field) string {
	switch v := f.Default.(type) {
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case int:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%f", v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("'%v'", v)
	}
}
