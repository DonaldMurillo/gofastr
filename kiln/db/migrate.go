package db

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Migrate brings the live SQLite schema in sync with the registry.
// framework.AutoMigrate now converges columns itself (additive ADD
// COLUMN on existing tables), so alignColumns below is a belt-and-
// braces second pass that predates that — it stays because it is
// idempotent (a column the framework already added is simply found)
// and keeps kiln's rebuild independent of the framework's diff path.
//
// Kiln's runtime DB is SQLite (build mode); this migrator targets the
// SQLite ALTER TABLE subset.
func Migrate(d *sql.DB, registry *framework.Registry) error {
	// Build mode authoring is free-order: an agent may add `posts`
	// (BelongsTo users) before `users` exists. The framework's AutoMigrate
	// correctly rejects a BelongsTo to an unknown entity, but for the live
	// runtime that would brick the rebuild on a transient forward reference.
	// Defer (drop) any BelongsTo whose target isn't registered yet; a later
	// rebuild — once the target entity is added — re-derives it from the
	// world and includes it. This mutates only the transient rebuild
	// registry, never the durable world, so freeze still emits the relation.
	deferDanglingRelations(registry)
	if err := framework.AutoMigrate(d, registry); err != nil {
		return err
	}
	for _, entity := range registry.All() {
		if err := alignColumns(d, entity); err != nil {
			return fmt.Errorf("align %s: %w", entity.GetName(), err)
		}
	}
	return nil
}

// deferDanglingRelations strips BelongsTo (ManyToOne) relations that point at
// entities not yet in the registry, so a forward-referencing live edit doesn't
// fail the rebuild. It operates in place on the transient rebuild registry.
func deferDanglingRelations(registry *framework.Registry) {
	known := make(map[string]bool)
	for _, e := range registry.All() {
		known[e.GetName()] = true
	}
	for _, e := range registry.All() {
		rels := e.Config.Relations
		kept := rels[:0]
		for _, r := range rels {
			if r.Type == framework.RelManyToOne && !known[r.Entity] {
				log.Printf("kiln/db: deferring relation %q on %q → unknown entity %q (target not added yet)",
					r.Name, e.GetName(), r.Entity)
				continue
			}
			kept = append(kept, r)
		}
		e.Config.Relations = kept
	}
}

func alignColumns(d *sql.DB, entity *framework.Entity) error {
	existing, err := tableColumns(d, entity.GetTable())
	if err != nil {
		return err
	}
	for _, f := range entity.GetFields() {
		if _, ok := existing[f.Name]; ok {
			continue
		}
		stmt := alterAddColumn(entity.GetTable(), f)
		if _, err := d.Exec(stmt); err != nil {
			return fmt.Errorf("alter table %s add %s: %w", entity.GetTable(), f.Name, err)
		}
	}
	return nil
}

func tableColumns(d *sql.DB, table string) (map[string]struct{}, error) {
	rows, err := d.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = struct{}{}
	}
	return cols, rows.Err()
}

func alterAddColumn(table string, f schema.Field) string {
	col := f.Name + " " + sqlType(f)
	if f.Default != nil {
		col += fmt.Sprintf(" DEFAULT %s", sqlDefault(f))
	}
	// SQLite's ADD COLUMN cannot enforce NOT NULL without a default; we
	// honor only Default. Required-without-default added columns become
	// nullable in the live DB; the freeze step emits a proper migration
	// later if the user wants strictness in production.
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, col)
}

// Mirror of framework's sqlType / sqlDefault — kept private to avoid
// importing framework's unexported helpers.
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
	case schema.Enum, schema.UUID, schema.Relation, schema.Image, schema.File, schema.JSON:
		return "TEXT"
	case schema.Timestamp:
		return "DATETIME"
	case schema.Date:
		return "DATE"
	default:
		return "TEXT"
	}
}

func sqlDefault(f schema.Field) string {
	switch v := f.Default.(type) {
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
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
