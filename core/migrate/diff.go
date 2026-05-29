package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Entity ties a table name to a schema.Schema for diff purposes.
type Entity struct {
	Name   string
	Schema schema.Schema
}

// columnInfo describes a column discovered from the live database.
type columnInfo struct {
	Name     string
	DataType string
}

// fieldTypeToSQL maps a schema.FieldType to a column type for the Postgres
// dialect. Kept for backward compatibility; the dialect-aware variant is
// fieldTypeToSQLForDialect.
func fieldTypeToSQL(t schema.FieldType) string {
	return fieldTypeToSQLForDialect(t, DialectPostgres)
}

// fieldTypeToSQLForDialect maps a schema.FieldType to a column type for the
// given SQL dialect. SQLite has a small set of storage classes, so several
// Postgres types collapse onto TEXT/INTEGER/REAL.
func fieldTypeToSQLForDialect(t schema.FieldType, d Dialect) string {
	if d == DialectSQLite {
		switch t {
		case schema.String, schema.Text, schema.Enum:
			return "TEXT"
		case schema.Int:
			return "INTEGER"
		case schema.Float, schema.Decimal:
			return "REAL"
		case schema.Bool:
			return "INTEGER"
		case schema.UUID, schema.Relation:
			return "TEXT"
		case schema.Timestamp, schema.Date:
			return "TEXT"
		case schema.JSON:
			return "TEXT"
		case schema.Image, schema.File:
			return "TEXT"
		default:
			return "TEXT"
		}
	}
	switch t {
	case schema.String:
		return "VARCHAR(255)"
	case schema.Text:
		return "TEXT"
	case schema.Int:
		return "BIGINT"
	case schema.Float:
		return "DOUBLE PRECISION"
	case schema.Decimal:
		return "DECIMAL"
	case schema.Bool:
		return "BOOLEAN"
	case schema.UUID:
		return "UUID"
	case schema.Timestamp:
		return "TIMESTAMP"
	case schema.Date:
		return "DATE"
	case schema.JSON:
		return "JSONB"
	case schema.Enum:
		return "VARCHAR(255)"
	case schema.Relation:
		return "UUID"
	case schema.Image, schema.File:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// Diff compares registered entity schemas against the current database and
// returns migrations needed to bring the database in sync.
// It generates CREATE TABLE for missing tables and ALTER TABLE ... ADD COLUMN
// for missing columns. It does NOT generate ALTER COLUMN or DROP statements.
func (m *Migrator) Diff(ctx context.Context, entities []Entity) ([]Migration, error) {
	if err := m.CreateMigrationsTable(ctx); err != nil {
		return nil, fmt.Errorf("creating migrations table: %w", err)
	}

	// Discover existing tables and their columns.
	existingTables, err := m.discoverTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering tables: %w", err)
	}

	var migrations []Migration
	version := m.nextDiffVersion()

	for _, ent := range entities {
		tableName := strings.ToLower(ent.Name)

		table, err := query.SafeIdent(tableName)
		if err != nil {
			return nil, fmt.Errorf("diff: invalid table name %q: %w", tableName, err)
		}

		columns, exists := existingTables[tableName]
		if !exists {
			// Generate CREATE TABLE.
			upSQL := m.generateCreateTable(table, ent.Schema)
			downSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", query.QuoteIdent(table))
			migrations = append(migrations, Migration{
				Version: version,
				Name:    fmt.Sprintf("auto_create_%s", tableName),
				Up:      upSQL,
				Down:    downSQL,
			})
			version++
			continue
		}

		// Check for missing columns.
		var missingColumns []schema.Field
		for _, f := range ent.Schema.Fields {
			colName := strings.ToLower(f.Name)
			if _, ok := columns[colName]; !ok {
				missingColumns = append(missingColumns, f)
			}
		}

		if len(missingColumns) > 0 {
			safeTable, err := query.SafeIdent(tableName)
			if err != nil {
				return nil, fmt.Errorf("diff: invalid table name %q: %w", tableName, err)
			}
			var alterStmts []string
			for _, f := range missingColumns {
				safeCol, err := query.SafeIdent(f.Name)
				if err != nil {
					return nil, fmt.Errorf("diff: invalid column name %q: %w", f.Name, err)
				}
				colType := fieldTypeToSQLForDialect(f.Type, m.dialect)
				var constraints []string
				if f.Required {
					constraints = append(constraints, "NOT NULL")
				}
				if f.Default != nil {
					constraints = append(constraints, fmt.Sprintf("DEFAULT %v", f.Default))
				}
				extra := ""
				if len(constraints) > 0 {
					extra = " " + strings.Join(constraints, " ")
				}
				alterStmts = append(alterStmts,
					fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s",
						query.QuoteIdent(safeTable), query.QuoteIdent(safeCol), colType, extra))
			}

			upSQL := strings.Join(alterStmts, ";\n") + ";"
			// Generate down: drop the added columns.
			var dropStmts []string
			for _, f := range missingColumns {
				safeCol, err := query.SafeIdent(f.Name)
				if err != nil {
					return nil, fmt.Errorf("diff: invalid column name %q: %w", f.Name, err)
				}
				dropStmts = append(dropStmts,
					fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", query.QuoteIdent(safeTable), query.QuoteIdent(safeCol)))
			}
			downSQL := strings.Join(dropStmts, ";\n") + ";"

			migrations = append(migrations, Migration{
				Version: version,
				Name:    fmt.Sprintf("auto_alter_%s", tableName),
				Up:      upSQL,
				Down:    downSQL,
			})
			version++
		}
	}

	return migrations, nil
}

// nextDiffVersion returns a version number higher than any registered migration.
func (m *Migrator) nextDiffVersion() uint64 {
	var max uint64
	for _, mig := range m.migrations {
		if mig.Version > max {
			max = mig.Version
		}
	}
	return max + 1
}

// discoverTables queries the database for all user tables and their columns.
func (m *Migrator) discoverTables(ctx context.Context) (map[string]map[string]columnInfo, error) {
	if m.dialect == DialectSQLite {
		return m.discoverTablesSQLite(ctx)
	}
	return m.discoverTablesPostgres(ctx)
}

// discoverTablesPostgres introspects via the Postgres information_schema catalog.
func (m *Migrator) discoverTablesPostgres(ctx context.Context) (map[string]map[string]columnInfo, error) {
	// Get all tables in the public schema.
	tableRows, err := m.db.QueryContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'")
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	var tableNames []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}
	if err := tableRows.Err(); err != nil {
		return nil, err
	}

	result := make(map[string]map[string]columnInfo, len(tableNames))

	for _, tbl := range tableNames {
		colRows, err := m.db.QueryContext(ctx,
			"SELECT column_name, data_type FROM information_schema.columns WHERE table_name = $1",
			tbl)
		if err != nil {
			return nil, err
		}

		cols := make(map[string]columnInfo)
		for colRows.Next() {
			var ci columnInfo
			if err := colRows.Scan(&ci.Name, &ci.DataType); err != nil {
				colRows.Close()
				return nil, err
			}
			cols[strings.ToLower(ci.Name)] = ci
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			return nil, err
		}

		result[strings.ToLower(tbl)] = cols
	}

	return result, nil
}

// discoverTablesSQLite introspects via sqlite_master and pragma_table_info,
// which are the SQLite equivalents of information_schema (which SQLite lacks).
func (m *Migrator) discoverTablesSQLite(ctx context.Context) (map[string]map[string]columnInfo, error) {
	tableRows, err := m.db.QueryContext(ctx,
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	var tableNames []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}
	if err := tableRows.Err(); err != nil {
		return nil, err
	}

	result := make(map[string]map[string]columnInfo, len(tableNames))

	for _, tbl := range tableNames {
		// pragma_table_info does not accept a bound parameter for the table
		// name in all driver/SQLite versions, so validate and inline it.
		safeTbl, err := query.SafeIdent(tbl)
		if err != nil {
			// Skip tables whose names we cannot safely reference.
			continue
		}
		// safeTbl has already passed SafeIdent (only [a-zA-Z0-9_.]), so it
		// cannot contain a quote or terminate the string literal.
		colRows, err := m.db.QueryContext(ctx,
			fmt.Sprintf("SELECT name, type FROM pragma_table_info('%s')", safeTbl))
		if err != nil {
			return nil, err
		}

		cols := make(map[string]columnInfo)
		for colRows.Next() {
			var ci columnInfo
			if err := colRows.Scan(&ci.Name, &ci.DataType); err != nil {
				colRows.Close()
				return nil, err
			}
			cols[strings.ToLower(ci.Name)] = ci
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			return nil, err
		}

		result[strings.ToLower(tbl)] = cols
	}

	return result, nil
}

// generateCreateTable builds a CREATE TABLE statement from a schema.
// The tableName must already be validated via SafeIdent.
func (m *Migrator) generateCreateTable(tableName string, s schema.Schema) string {
	var cols []string

	sqlite := m.dialect == DialectSQLite

	// Add an id column if not present.
	if _, hasID := s.FieldByName("id"); !hasID {
		if sqlite {
			cols = append(cols, "id INTEGER PRIMARY KEY AUTOINCREMENT")
		} else {
			cols = append(cols, "id BIGSERIAL PRIMARY KEY")
		}
	}

	for _, f := range s.Fields {
		colType := fieldTypeToSQLForDialect(f.Type, m.dialect)
		safeCol := query.MustIdent(f.Name)
		var constraints []string
		if f.Required {
			constraints = append(constraints, "NOT NULL")
		}
		if f.Unique {
			constraints = append(constraints, "UNIQUE")
		}
		if f.Default != nil {
			constraints = append(constraints, fmt.Sprintf("DEFAULT %v", f.Default))
		}
		extra := ""
		if len(constraints) > 0 {
			extra = " " + strings.Join(constraints, " ")
		}
		cols = append(cols, fmt.Sprintf("%s %s%s", query.QuoteIdent(safeCol), colType, extra))
	}

	// Add timestamps if not present.
	tsType := "TIMESTAMP"
	tsDefault := "NOW()"
	if sqlite {
		tsType = "TEXT"
		tsDefault = "CURRENT_TIMESTAMP"
	}
	if _, hasCreated := s.FieldByName("created_at"); !hasCreated {
		cols = append(cols, fmt.Sprintf("created_at %s NOT NULL DEFAULT %s", tsType, tsDefault))
	}
	if _, hasUpdated := s.FieldByName("updated_at"); !hasUpdated {
		cols = append(cols, fmt.Sprintf("updated_at %s NOT NULL DEFAULT %s", tsType, tsDefault))
	}

	return fmt.Sprintf("CREATE TABLE %s (\n\t%s\n)", query.QuoteIdent(tableName), strings.Join(cols, ",\n\t"))
}

// SortedEntities returns entities sorted by name for deterministic output.
func SortedEntities(entities []Entity) []Entity {
	sorted := make([]Entity, len(entities))
	copy(sorted, entities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})
	return sorted
}

// Compile-time check that Migrator uses *sql.DB.
var _ = (*sql.DB)(nil)
