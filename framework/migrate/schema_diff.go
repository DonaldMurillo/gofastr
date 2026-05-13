package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Schema diff
//
// DiffSchema compares each registered entity's declared fields against the
// live DB schema and emits the ALTER TABLE statements that would bring the
// DB in line. Today the diff covers ADD COLUMN (entity has a field the DB
// doesn't) and DROP COLUMN (DB has a column the entity no longer declares).
// Type changes are intentionally out of scope — SQLite's ALTER COLUMN
// support is too limited to do safely in-place, and Postgres type changes
// often need data conversion which the diff can't infer.

// SchemaChange is one DDL fragment plus a human-friendly summary. Callers
// can apply directly via db.Exec or stitch them into a migration file.
type SchemaChange struct {
	Summary string // e.g. "posts: add column views INTEGER"
	SQL     string // executable DDL statement
}

// DiffSchema returns the changes needed to bring db in line with every
// entity in the registry. Auto-detects dialect from the open DB; tables
// missing entirely from the DB are reported as CREATE TABLE statements
// (delegates to the same builder AutoMigrate uses).
func DiffSchema(ctx context.Context, db *sql.DB, registry entity.Registry) ([]SchemaChange, error) {
	dialect := DetectDialect(db)
	all := registry.All()

	// Walk entities in topo order so referenced tables get diffed first.
	ordered, err := topoSortEntities(all)
	if err != nil {
		return nil, err
	}

	var out []SchemaChange
	for _, ent := range ordered {
		changes, err := diffEntity(ctx, db, ent, all, dialect)
		if err != nil {
			return nil, fmt.Errorf("diff %s: %w", ent.GetName(), err)
		}
		out = append(out, changes...)
	}
	return out, nil
}

// ApplySchemaDiff applies every change in sequence inside a single
// transaction and returns the count applied. Aborts on first error,
// rolling everything back.
func ApplySchemaDiff(ctx context.Context, db *sql.DB, changes []SchemaChange) (int, error) {
	if len(changes) == 0 {
		return 0, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	for i, c := range changes {
		if _, err := tx.ExecContext(ctx, c.SQL); err != nil {
			_ = tx.Rollback()
			return i, fmt.Errorf("apply %q: %w", c.Summary, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(changes), nil
}

// diffEntity diffs one entity against the live schema. If the table doesn't
// exist at all, returns a single CREATE TABLE change. Otherwise compares
// columns and emits ADD/DROP fragments.
func diffEntity(ctx context.Context, db *sql.DB, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect) ([]SchemaChange, error) {
	live, err := ReadLiveColumns(ctx, db, ent.GetTable(), dialect)
	if err != nil {
		return nil, err
	}
	if len(live) == 0 {
		// Table missing entirely — emit a CREATE TABLE via the same path
		// AutoMigrate uses, captured as SQL string.
		ddl, err := buildCreateTableSQL(ent, all, dialect)
		if err != nil {
			return nil, err
		}
		return []SchemaChange{{
			Summary: fmt.Sprintf("%s: create table", ent.GetName()),
			SQL:     ddl,
		}}, nil
	}

	declared := make(map[string]schema.Field, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		declared[f.Name] = f
	}

	var changes []SchemaChange

	// ADD COLUMN for declared-but-missing fields.
	for _, f := range ent.GetFields() {
		if _, ok := live[f.Name]; ok {
			continue
		}
		colType := SQLType(f, dialect)
		nullable := ""
		if f.Required && f.AutoGenerate == schema.AutoNone {
			nullable = " NOT NULL"
		}
		defaultClause := ""
		if f.Default != nil {
			defaultClause = fmt.Sprintf(" DEFAULT %s", SQLDefault(f, dialect))
		}
		ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s%s",
			ent.GetTable(), f.Name, colType, nullable, defaultClause)
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("%s: add column %s %s", ent.GetName(), f.Name, colType),
			SQL:     ddl,
		})
	}

	// DROP COLUMN for live-but-undeclared (skip framework-managed columns).
	// Sorted for stable output.
	liveNames := make([]string, 0, len(live))
	for name := range live {
		liveNames = append(liveNames, name)
	}
	sort.Strings(liveNames)
	for _, name := range liveNames {
		if _, ok := declared[name]; ok {
			continue
		}
		if isFrameworkManagedColumn(name, ent) {
			continue
		}
		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", ent.GetTable(), name)
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("%s: drop column %s", ent.GetName(), name),
			SQL:     ddl,
		})
	}

	return changes, nil
}

// isFrameworkManagedColumn reports whether a column is auto-managed by the
// framework (timestamps, tenant_id, deleted_at) and should NOT be dropped
// just because it isn't declared on the entity.
func isFrameworkManagedColumn(name string, ent *entity.Entity) bool {
	switch name {
	case "created_at", "updated_at":
		return ent.Config.Timestamps
	case "deleted_at":
		return ent.Config.SoftDelete
	case "tenant_id":
		return ent.Config.MultiTenant
	}
	return false
}

// ReadLiveColumns returns a map of column_name → data_type from the live
// DB. Empty map means "table doesn't exist".
func ReadLiveColumns(ctx context.Context, db *sql.DB, table string, dialect Dialect) (map[string]string, error) {
	if dialect == DialectPostgres {
		return ReadLiveColumnsPostgres(ctx, db, table)
	}
	return ReadLiveColumnsSQLite(ctx, db, table)
}

func ReadLiveColumnsPostgres(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			return nil, err
		}
		out[name] = typ
	}
	return out, rows.Err()
}

func ReadLiveColumnsSQLite(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	// PRAGMA can't be parameterised; the table name is taken from our own
	// registry, not user input, so injection isn't a concern.
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var (
			cid          int
			name, typ    string
			notnull, pk  int
			defaultValue sql.NullString
		)
		if err := rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		out[name] = typ
	}
	return out, rows.Err()
}

// buildCreateTableSQL renders the CREATE TABLE statement for an entity,
// identical to what AutoMigrate would emit. Used when the table is missing
// entirely.
func buildCreateTableSQL(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect) (string, error) {
	fields := ent.GetFields()
	if len(fields) == 0 {
		return "", fmt.Errorf("entity %s has no fields", ent.GetName())
	}
	var columns []string
	for _, f := range fields {
		col := fmt.Sprintf("%s %s", f.Name, SQLType(f, dialect))
		if f.Name == ent.PrimaryKey {
			col += " PRIMARY KEY"
		}
		if f.Unique {
			col += " UNIQUE"
		}
		if f.Required && f.AutoGenerate == schema.AutoNone {
			col += " NOT NULL"
		}
		if f.Default != nil {
			col += fmt.Sprintf(" DEFAULT %v", SQLDefault(f, dialect))
		}
		columns = append(columns, col)
	}
	if all != nil {
		fks, err := foreignKeyClauses(ent, all)
		if err != nil {
			return "", err
		}
		columns = append(columns, fks...)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		ent.GetTable(), strings.Join(columns, ",\n\t")), nil
}
