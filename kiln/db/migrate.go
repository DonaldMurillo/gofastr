package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Migrate brings the live SQLite schema in sync with the registry. It
// covers the live-edit path the framework's CREATE-only AutoMigrate
// doesn't: when the agent adds a field to an existing entity, this
// emits ALTER TABLE ADD COLUMN for it. New tables fall through to
// framework.AutoMigrate.
//
// Kiln's runtime DB is SQLite (build mode); this migrator targets the
// SQLite ALTER TABLE subset.
func Migrate(d *sql.DB, registry *framework.Registry) error {
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
