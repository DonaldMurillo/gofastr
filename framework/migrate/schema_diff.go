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
// Type changes are intentionally out of scope: SQLite's ALTER COLUMN
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

	// Destructive marks a change that can lose data, a DROP COLUMN today
	// (DROP TABLE in future). ApplySchemaDiff refuses to run destructive
	// changes unless the caller opts in via ApplySchemaDiffWithOptions, so a
	// routine boot-time convergence never silently deletes a column. This is
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
	dialect, err := detectDialectFailClosed(db)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	all := UnionEntities(registry)

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
// everything back. Destructive changes (DROP COLUMN/TABLE) are refused. Use
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
		// Table missing entirely: emit a CREATE TABLE via the same path
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

	// RENAME COLUMN for explicitly-declared renames (EntityConfig.Renames:
	// old → new). Rename is indistinguishable from drop+add without the hint,
	// so it requires the explicit declaration. A rename only fires when the
	// old column is live and the new name is declared; it is non-destructive
	// (preserves row data). The renamed pair is then skipped by the ADD and
	// DROP loops below so it does not also surface as drop+add.
	renameTargets := map[string]bool{}    // new column names consumed by a rename
	renameSources := map[string]bool{}    // old column names consumed by a rename
	renameOldByNew := map[string]string{} // new (lower) → old (lower), so the type-change loop can compare a renamed column's declared type against its live (old-name) type
	for oldName, newName := range ent.Config.Renames {
		oldLow := strings.ToLower(oldName)
		newLow := strings.ToLower(newName)
		if _, oldLive := liveLower[oldLow]; !oldLive {
			continue // old column already gone, nothing to rename
		}
		if _, newDeclared := declared[newLow]; !newDeclared {
			continue // new name not declared, stale/mis-declared hint, skip
		}
		qOld, err := query.SafeIdent(oldName)
		if err != nil {
			return nil, fmt.Errorf("invalid rename source %q: %w", oldName, err)
		}
		qNew, err := query.SafeIdent(newName)
		if err != nil {
			return nil, fmt.Errorf("invalid rename target %q: %w", newName, err)
		}
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("%s: rename column %s → %s", ent.GetName(), oldName, newName),
			SQL:     fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", qtable, qOld, qNew),
			Down:    fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", qtable, qNew, qOld),
		})
		renameTargets[newLow] = true
		renameSources[oldLow] = true
		renameOldByNew[newLow] = oldLow
	}

	// ADD COLUMN for declared-but-missing fields.
	for _, f := range ent.GetFields() {
		if _, ok := liveLower[strings.ToLower(f.Name)]; ok {
			continue
		}
		if renameTargets[strings.ToLower(f.Name)] {
			continue // handled by RENAME COLUMN above
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

	// TYPE CHANGE for declared-and-present columns whose type drifted. A type
	// change can need a data-specific conversion (a Postgres USING clause; a
	// SQLite table rebuild), so it is flagged Destructive and refused by
	// default, surfaced for review, never silently applied. Types are
	// normalized per dialect (see canonicalType) so PG information_schema
	// names ("character varying", "timestamp with time zone") don't
	for _, f := range ent.GetFields() {
		if f.RawType != "" {
			continue // operator-supplied raw type (domains, arrays, custom types), not reliably diffable against the live DB type, which reports the underlying type
		}
		nameLow := strings.ToLower(f.Name)
		liveType, ok := liveLower[nameLow]
		if !ok {
			// A rename target's live type lives under the OLD column name.
			if old, renamed := renameOldByNew[nameLow]; renamed {
				liveType, ok = liveLower[old]
			}
		}
		if !ok {
			continue // absent from live → handled by ADD COLUMN above
		}
		declaredType := SQLType(f, dialect)
		if typesEquivalent(declaredType, liveType, dialect) {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: change column %s type %s → %s (destructive: data conversion may be required)", ent.GetName(), f.Name, liveType, declaredType),
			SQL:         alterColumnTypeSQL(qtable, qcol, declaredType, dialect),
			Down:        alterColumnTypeSQL(qtable, qcol, liveType, dialect),
			Destructive: true,
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
		if renameSources[strings.ToLower(name)] {
			continue // handled by RENAME COLUMN above
		}
		qcol, err := query.SafeIdent(name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", name, err)
		}
		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", qtable, qcol)
		// Inverse re-adds the column with the type it had in the previous
		// snapshot. Reversible at the schema level: row data is not restored.
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
	// Key the tenant arm off TenantColumn(), not the literal "tenant_id".
	// TenantField exists precisely so the column can be renamed, and
	// tenant.WithMultiTenant sets it AFTER entity.Define has already
	// injected the default-named field, so the entity declares
	// "tenant_id" while CRUD reads and writes the renamed column, and
	// the diff emitted a data-losing DROP COLUMN on the column the app
	// actually uses. Protect both names: the configured one, and the
	// default that Define may have injected before the rename.
	if ent.Config.Scope.MultiTenant && (name == ent.Config.TenantColumn() || name == "tenant_id") {
		return true
	}
	switch name {
	case "created_at", "updated_at":
		return ent.Config.Timestamps != nil && *ent.Config.Timestamps
	case "deleted_at":
		return ent.Config.Scope.SoftDelete
	}
	return false
}

// canonicalType normalizes a SQL type string for equivalence comparison
// across the declared (SQLType-emitted) and live-DB forms. It lowercases,
// strips parenthetical sizes ("VARCHAR(255)" → "varchar",
// "DECIMAL(19,4)" → "decimal"), and maps the dialect-specific aliases
// information_schema reports on Postgres to the canonical names SQLType
// emits. Without this, every PG VARCHAR/TIMESTAMPTZ/DECIMAL column would
// false-positive as a type change.
func canonicalType(s string, dialect Dialect) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if dialect == DialectPostgres {
		switch t {
		case "character varying":
			t = "varchar"
		case "timestamp with time zone":
			t = "timestamptz"
		case "timestamp without time zone":
			t = "timestamp"
		case "numeric":
			t = "decimal"
		case "serial":
			t = "integer" // SERIAL is an integer column + a sequence
		case "bigserial":
			t = "bigint"
		}
	}
	return t
}

// typesEquivalent reports whether a declared type and a live-DB type are the
// same after per-dialect canonicalization. Conservative by design: it only
// asserts equality, never "close enough". A real type change always differs
// canonically, and a dialect-naming variant never does.
func typesEquivalent(declared, live string, dialect Dialect) bool {
	return canonicalType(declared, dialect) == canonicalType(live, dialect)
}

// alterColumnTypeSQL renders the dialect-appropriate DDL for a column type
// change. Postgres supports an in-place ALTER COLUMN TYPE (a data-specific
// USING clause may be required and is left for the reviewer); SQLite has no
// in-place ALTER COLUMN TYPE, so a comment flags the rebuild need rather than
// emitting DDL that would silently no-op or fail.
func alterColumnTypeSQL(qtable, qcol, newType string, dialect Dialect) string {
	if dialect == DialectPostgres {
		return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", qtable, qcol, newType)
	}
	return fmt.Sprintf("-- SQLite has no in-place ALTER COLUMN TYPE; rebuild the table to set %s to %s", qcol, newType)
}

// queryer is the read-only subset of *sql.DB / *sql.Tx the live-schema
// readers need. The *sql.Tx form lets AutoMigrate re-read columns on the
// advisory-lock-holding connection (a separate pool connection would deadlock
// a MaxOpenConns(1) pool).
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ReadLiveColumns returns a map of column_name → data_type from the live
// DB. Empty map means "table doesn't exist".
func ReadLiveColumns(ctx context.Context, db *sql.DB, table string, dialect Dialect) (map[string]string, error) {
	return readLiveColumnsQ(ctx, db, table, dialect)
}

func readLiveColumnsQ(ctx context.Context, q queryer, table string, dialect Dialect) (map[string]string, error) {
	if dialect == DialectPostgres {
		return readLiveColumnsPostgresQ(ctx, q, table)
	}
	return readLiveColumnsSQLiteQ(ctx, q, table)
}

func ReadLiveColumnsPostgres(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	return readLiveColumnsPostgresQ(ctx, db, table)
}

func readLiveColumnsPostgresQ(ctx context.Context, q queryer, table string) (map[string]string, error) {
	// AutoMigrate emits CREATE TABLE with UNQUOTED identifiers, so Postgres
	// folds a mixed-case table name to lowercase in information_schema.
	// Match case-insensitively so a registry table like "MixedAccount" finds
	// its folded live columns instead of reading as "table doesn't exist".
	rows, err := q.QueryContext(ctx, `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND lower(table_name) = lower($1)
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
	return readLiveColumnsSQLiteQ(ctx, db, table)
}

func readLiveColumnsSQLiteQ(ctx context.Context, q queryer, table string) (map[string]string, error) {
	// PRAGMA can't be parameterised, so the identifier must be validated
	// instead. The previous comment here claimed the table name "is taken
	// from our own registry, not user input". That is false: kiln's
	// add_entity / update_entity ops accept a `table` override in their
	// HTTP JSON payload and replay validates only the entity Name. This
	// read also runs at AutoMigratePlanContext BEFORE any other SafeIdent
	// call in the plan path, so it is the FIRST place a hostile name
	// lands. Every sibling validates (the Postgres twin parameterises,
	// tableExistsBulkSQLite parameterises, buildCreateTableSQL /
	// diffEntityFromLive / migrateEntity all call SafeIdent); this was
	// the lone exception, relying on database/sql executing a single
	// statement, a driver property, not ours.
	if _, err := query.SafeIdent(table); err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
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
// the SINGLE source of CREATE TABLE DDL: AutoMigrate (migrateEntity) and the
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
// schema while the unquoted runtime folds it to lowercase, breaking any
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
