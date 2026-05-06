package framework

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/core/schema"
)

// AutoMigrate creates tables for all registered entities that have a DB connection.
// It uses CREATE TABLE IF NOT EXISTS so it's safe to run on every startup.
func AutoMigrate(db *sql.DB, registry *Registry) error {
	for _, entity := range registry.All() {
		if err := MigrateEntity(db, entity); err != nil {
			return fmt.Errorf("migrate %s: %w", entity.GetName(), err)
		}
	}
	return nil
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
func MigrateEntity(db *sql.DB, entity *Entity) error {
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

	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		entity.GetTable(),
		strings.Join(columns, ",\n\t"),
	)

	_, err := db.Exec(stmt)
	return err
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
		return fmt.Sprintf("'%s'", v)
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
