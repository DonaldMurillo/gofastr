package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
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

	// Down is the inverse DDL that rolls this change back, used when
	// generating a reversible versioned migration file. CREATE TABLE → DROP
	// TABLE, ADD COLUMN → DROP COLUMN, DROP COLUMN → ADD COLUMN (recreates the
	// column from its previous type; row data is NOT restored). Empty when no
	// safe inverse is known.
	Down string

	// Destructive marks a change that can lose data — a DROP COLUMN today
	// (DROP TABLE in future). ApplySchemaDiff refuses to run destructive
	// changes unless the caller opts in via ApplySchemaDiffWithOptions, so a
	// routine `migrate diff --apply` never silently deletes a column. This is
	// the GORM-style "never drop by default" safety posture.
	Destructive bool
}

// DestructiveChangeError is returned by ApplySchemaDiff when the change set
// contains destructive changes and the caller did not opt in to them. The
// Summaries list the blocked changes for a human-readable message.
type DestructiveChangeError struct {
	Summaries []string
}

func (e *DestructiveChangeError) Error() string {
	return fmt.Sprintf("refusing %d destructive change(s) without explicit opt-in: %s",
		len(e.Summaries), strings.Join(e.Summaries, "; "))
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
	tables := make([]string, 0, len(ordered))
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue
		}
		tables = append(tables, ent.GetTable())
	}
	liveByTable, err := ReadLiveColumnsBulk(ctx, db, tables, dialect)
	if err != nil {
		return nil, err
	}
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue
		}
		changes, err := diffEntityFromLive(ent, all, dialect, liveByTable[ent.GetTable()])
		if err != nil {
			return nil, fmt.Errorf("diff %s: %w", ent.GetName(), err)
		}
		out = append(out, changes...)
	}
	return out, nil
}

// ApplyOptions tunes ApplySchemaDiffWithOptions.
type ApplyOptions struct {
	// AllowDestructive permits DROP COLUMN / DROP TABLE changes. When false
	// (the default), a change set containing any destructive change is
	// rejected with a *DestructiveChangeError before any DDL runs.
	AllowDestructive bool
}

// ApplySchemaDiff applies every change in sequence inside a single
// transaction and returns the count applied. Aborts on first error, rolling
// everything back. Destructive changes (DROP COLUMN/TABLE) are refused — use
// ApplySchemaDiffWithOptions with AllowDestructive to opt in.
func ApplySchemaDiff(ctx context.Context, db *sql.DB, changes []SchemaChange) (int, error) {
	return ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{})
}

// ApplySchemaDiffWithOptions is ApplySchemaDiff with a destructive-change
// opt-in. Everything still runs in a single transaction.
func ApplySchemaDiffWithOptions(ctx context.Context, db *sql.DB, changes []SchemaChange, opts ApplyOptions) (int, error) {
	if len(changes) == 0 {
		return 0, nil
	}
	if !opts.AllowDestructive {
		var blocked []string
		for _, c := range changes {
			if c.Destructive {
				blocked = append(blocked, c.Summary)
			}
		}
		if len(blocked) > 0 {
			return 0, &DestructiveChangeError{Summaries: blocked}
		}
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

func diffEntityFromLive(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string) ([]SchemaChange, error) {
	qtable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return nil, fmt.Errorf("invalid table name %q: %w", ent.GetTable(), err)
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
			Down:    fmt.Sprintf("DROP TABLE IF EXISTS %s", qtable),
		}}, nil
	}

	// Postgres lowercases unquoted identifiers in information_schema.
	// Normalize both sides so the comparison is case-insensitive on PG.
	liveLower := make(map[string]string, len(live))
	for k, v := range live {
		liveLower[strings.ToLower(k)] = v
	}

	declared := make(map[string]schema.Field, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		declared[strings.ToLower(f.Name)] = f
	}

	var changes []SchemaChange

	// ADD COLUMN for declared-but-missing fields.
	for _, f := range ent.GetFields() {
		if _, ok := liveLower[strings.ToLower(f.Name)]; ok {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		colType := SQLType(f, dialect)
		defaultClause := ColumnDefaultClause(f, dialect)
		// A NOT NULL ADD COLUMN with no default fails on a populated table
		// (every existing row would violate the constraint) on both Postgres
		// and older SQLite. When the field is Required but has no default, omit
		// NOT NULL so the column is added nullable; the constraint can be
		// tightened later once the rows are backfilled. Matches kiln/db/migrate.
		summary := fmt.Sprintf("%s: add column %s %s", ent.GetName(), f.Name, colType)
		nullable := ""
		if f.Required && f.AutoGenerate == schema.AutoNone {
			if defaultClause != "" {
				nullable = " NOT NULL"
			} else {
				summary += " (NOT NULL deferred: no default for existing rows)"
			}
		}
		ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s%s%s",
			qtable, qcol, colType, nullable, defaultClause)
		changes = append(changes, SchemaChange{
			Summary: summary,
			SQL:     ddl,
			Down:    fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qtable, qcol),
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
		qcol, err := query.SafeIdent(name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", name, err)
		}
		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qtable, qcol)
		// Inverse re-adds the column with the type it had in the previous
		// snapshot. Reversible at the schema level — row data is not restored.
		downType := live[name]
		if downType == "" {
			downType = "TEXT"
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: drop column %s", ent.GetName(), name),
			SQL:         ddl,
			Down:        fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", qtable, qcol, downType),
			Destructive: true,
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

// buildCreateTableSQL renders the CREATE TABLE statement for an entity. This is
// the SINGLE source of CREATE TABLE DDL — AutoMigrate (migrateEntity) and the
// diff/generate paths both call it, so what gets generated is byte-identical to
// what auto-migrate applies. Every identifier is validated and quoted.
func buildCreateTableSQL(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect) (string, error) {
	if len(ent.GetFields()) == 0 {
		return "", fmt.Errorf("entity %s has no fields", ent.GetName())
	}
	columns, err := columnDefs(ent, all, dialect)
	if err != nil {
		return "", fmt.Errorf("entity %s: %w", ent.GetName(), err)
	}
	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return "", fmt.Errorf("entity %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		safeTable, strings.Join(columns, ",\n\t")), nil
}

// columnDefs builds the per-column DDL fragments (validated name + type +
// column constraints) plus the FK clauses (when all is non-nil). Shared by
// buildCreateTableSQL so there is exactly one column-rendering path.
//
// Identifiers are VALIDATED (SafeIdent rejects anything but [A-Za-z0-9_.]) but
// NOT quoted, to match the runtime SQL the query builder / crud emit, which is
// also unquoted. Quoting only the DDL would make Postgres preserve case in the
// schema while the unquoted runtime folds it to lowercase — breaking any
// mixed-case identifier. Validation alone provides the injection safety.
func columnDefs(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect) ([]string, error) {
	var columns []string
	for _, f := range ent.GetFields() {
		safeCol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		col := fmt.Sprintf("%s %s", safeCol, SQLType(f, dialect))
		if f.Name == ent.PrimaryKey {
			col += " PRIMARY KEY"
		}
		if f.Unique {
			col += " UNIQUE"
		}
		if f.Required && f.AutoGenerate == schema.AutoNone {
			col += " NOT NULL"
		}
		col += ColumnDefaultClause(f, dialect)
		columns = append(columns, col)
	}
	if all != nil {
		fks, err := foreignKeyClauses(ent, all)
		if err != nil {
			return nil, err
		}
		columns = append(columns, fks...)
	}
	return columns, nil
}
