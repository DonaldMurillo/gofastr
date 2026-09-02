package db

import (
	"database/sql"
	"fmt"
	"log"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Migrate brings the live SQLite schema in sync with the registry.
// framework.AutoMigrate now converges columns itself (additive ADD
// COLUMN on existing tables), so alignColumns below is a belt-and-
// braces second pass that predates that, it stays because it is
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
	// rebuild, once the target entity is added, re-derives it from the
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
	rows, err := d.Query(`PRAGMA table_info(` + query.QuoteIdent(table) + `)`)
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
	// Identifiers are quoted, not trusted: table and column names derive
	// from agent-authored world IR (entity name / table override / field
	// name over the unauthenticated add_entity/add_field surface), and
	// modernc's SQLite driver executes multi-statement strings, so a
	// `; --` tail in a raw interpolation becomes a second statement
	// (arbitrary DDL, ATTACH-based file creation). Quoting makes the
	// whole tail one (weird, harmless) identifier — the exact property
	// sqlDefault already pins for DEFAULT literals.
	col := query.QuoteIdent(f.Name) + " " + sqlType(f)
	if f.Default != nil {
		col += fmt.Sprintf(" DEFAULT %s", sqlDefault(f))
	}
	// SQLite's ADD COLUMN cannot enforce NOT NULL without a default; we
	// honor only Default. Required-without-default added columns become
	// nullable in the live DB; the freeze step emits a proper migration
	// later if the user wants strictness in production.
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", query.QuoteIdent(table), col)
}

// Mirror of framework's sqlType / sqlDefault, kept private to avoid
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

// sqlDefault delegates to the framework's renderer rather than mirroring
// it. The copy that used to live here had the same default arm the
// framework deleted after a verified payload: fmt.Sprintf("'%v'", v)
// splices the rendering raw between unescaped quotes, and
// schema.Field.Default is `any`, so a named string type, a fmt.Stringer,
// or a map/slice decoded from JSON all miss `case string` and take it.
// One quote in the value closes the literal and appends arbitrary DDL to
// ALTER TABLE ... ADD COLUMN -- reached from kiln's add_entity op over
// HTTP. Two copies of an escaping rule is how the fix reached one of them
// and not the other; there is one now.
//
// kiln's local store is SQLite, so the dialect is fixed here.
func sqlDefault(f schema.Field) string {
	return migrate.SQLDefault(f, migrate.DialectSQLite)
}

// DropTable removes one table from the live SQLite database. The name
// comes from the registry (live's delete_entity side effect), but it is
// validated and quoted anyway: the same rule ApplySeeds applies to seed
// tables. IF EXISTS because a rebuild may not have created the table
// yet.
func DropTable(d *sql.DB, table string) error {
	t, err := query.SafeIdent(table)
	if err != nil {
		return fmt.Errorf("kiln/db: drop: %w", err)
	}
	if _, err := d.Exec("DROP TABLE IF EXISTS " + query.QuoteIdent(t)); err != nil {
		return fmt.Errorf("kiln/db: drop %s: %w", table, err)
	}
	return nil
}

// DropAllTables removes every user table from the live SQLite database
// (SQLite's own sqlite_% internals excluded). live uses it to re-derive
// the ephemeral runtime DB from the journal: the DB is derived state,
// so a reset, undo, or reboot rebuilds it rather than carrying rows no
// journal entry authorizes.
func DropAllTables(d *sql.DB) error {
	rows, err := d.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return fmt.Errorf("kiln/db: list tables: %w", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("kiln/db: scan table name: %w", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("kiln/db: list tables: %w", err)
	}
	rows.Close()
	sort.Strings(tables)
	for _, t := range tables {
		if err := DropTable(d, t); err != nil {
			return err
		}
	}
	return nil
}
