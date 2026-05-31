package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Dialect identifies the SQL dialect the migrator emits for. It's an alias
// for coremig.Dialect so framework code and the lower-level migration system
// share one source of truth for dialect identity.
type Dialect = coremig.Dialect

// Dialect identifiers re-exported from core/migrate for ergonomic use inside
// the framework package and in tests.
const (
	DialectSQLite   = coremig.DialectSQLite
	DialectPostgres = coremig.DialectPostgres
)

// DetectDialect returns the dialect of an open *sql.DB. It probes for
// PostgreSQL via SELECT version() (cheap, no side effects) and falls back to
// SQLite when that fails. The probe runs once per AutoMigrate call.
func DetectDialect(db *sql.DB) Dialect {
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
func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	all := registry.All()
	ordered, err := topoSortEntities(all)
	if err != nil {
		return err
	}
	dialect := DetectDialect(db)
	existing := map[string]bool{}
	if dialect == DialectPostgres {
		tableNames := make([]string, 0, len(ordered))
		for _, ent := range ordered {
			tableNames = append(tableNames, ent.GetTable())
		}
		var err error
		existing, err = TableExistsBulk(context.Background(), db, tableNames, dialect)
		if err != nil {
			return err
		}
	}
	for _, ent := range ordered {
		if err := migrateEntityWithRegistry(db, ent, all, dialect, existing[ent.GetTable()]); err != nil {
			return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
		}
	}
	return nil
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
// It does not emit FK constraints since it has no view of the wider registry;
// callers that need foreign keys should call AutoMigrate. The dialect is
// auto-detected from db.
func MigrateEntity(db *sql.DB, ent *entity.Entity) error {
	return migrateEntityWithRegistry(db, ent, nil, DetectDialect(db), false)
}

// MigrateEntityDialect is the explicit-dialect variant used by callers that
// already know the target (e.g. CLI codegen, tests).
func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect Dialect) error {
	return migrateEntityWithRegistry(db, ent, nil, dialect, false)
}

// migrateEntityWithRegistry is the shared implementation. When `all` is
// non-nil it is consulted for FK target tables; missing targets return an
// error before any DDL runs.
func migrateEntityWithRegistry(db *sql.DB, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, tableExists bool) error {
	fields := ent.GetFields()
	if len(fields) == 0 {
		return nil
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
		col += ColumnDefaultClause(f, dialect)
		columns = append(columns, col)
	}

	if all != nil {
		fks, err := foreignKeyClauses(ent, all)
		if err != nil {
			return err
		}
		columns = append(columns, fks...)
	}

	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return fmt.Errorf("migrate %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}

	if !tableExists {
		stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
			query.QuoteIdent(safeTable),
			strings.Join(columns, ",\n\t"),
		)

		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	// Secondary indices — emit AFTER the table exists. CREATE INDEX IF NOT
	// EXISTS works on both engines so re-running AutoMigrate is idempotent.
	// An index with neither Columns nor Expression is a no-op (legacy: empty
	// Columns used to silently skip; we preserve that for the all-zero case).
	for _, idx := range ent.Config.Indices {
		if len(idx.Columns) == 0 && idx.Expression == "" {
			continue
		}
		if _, err := db.Exec(indexDDL(safeTable, idx)); err != nil {
			return fmt.Errorf("create index on %s: %w", ent.GetTable(), err)
		}
	}
	return nil
}

// indexDDL builds the CREATE INDEX statement for one declared Index. Name
// is synthesised from the table + columns when empty. The table parameter
// must already be validated via SafeIdent.
//
// When idx.Expression is non-empty, the body of the index parens is the
// raw expression (so functional indices like `lower(food)` work).
// Expression indices require an explicit Name — there's no safe slug
// for an arbitrary expression. The expression itself is rejected if it
// contains a semicolon or a SQL line/block comment marker, which is
// the minimal sanity check appropriate for an operator-supplied
// schema declaration loaded at startup.
func indexDDL(table string, idx entity.Index) string {
	name := idx.Name
	if name == "" {
		if idx.Expression != "" {
			panic(fmt.Sprintf("migrate: index on %s has Expression but no Name — expression indices require an explicit Name", table))
		}
		name = "idx_" + table + "_" + strings.Join(idx.Columns, "_")
	}
	safeName, err := query.SafeIdent(name)
	if err != nil {
		panic(fmt.Sprintf("migrate: invalid index name %q: %v", name, err))
	}
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	var body string
	if idx.Expression != "" {
		body = sanitizeIndexExpression(idx.Expression)
	} else {
		safeCols := make([]string, len(idx.Columns))
		for i, col := range idx.Columns {
			safeCols[i] = query.QuoteIdent(query.MustIdent(col))
		}
		body = strings.Join(safeCols, ", ")
	}
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, query.QuoteIdent(safeName), query.QuoteIdent(table), body)
}

// sanitizeIndexExpression rejects index expressions that contain SQL
// statement terminators or comment markers. The expression is rendered
// verbatim into DDL at startup, so we want to fail loud on suspicious
// payloads rather than silently emit a possibly-broken statement.
// Anything more expressive (real SQL parsing) belongs in a separate
// validator — for now this catches the obvious "operator pasted a
// trailing semicolon" / "comment block" footguns.
func sanitizeIndexExpression(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		panic("migrate: index Expression is empty after trim")
	}
	for _, banned := range []string{";", "--", "/*", "*/"} {
		if strings.Contains(trimmed, banned) {
			panic(fmt.Sprintf("migrate: index Expression %q contains forbidden token %q", trimmed, banned))
		}
	}
	return trimmed
}

// foreignKeyClauses produces "FOREIGN KEY (col) REFERENCES target(id)"
// fragments for every BelongsTo relation declared on the entity. Targets
// must exist in `all` or the function returns an error.
func foreignKeyClauses(ent *entity.Entity, all map[string]*entity.Entity) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	for _, rel := range ent.Config.Relations {
		if rel.Type != entity.RelManyToOne || rel.ForeignKey == "" {
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
		safeRelFK, err := query.SafeIdent(rel.ForeignKey)
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid FK %q: %w", rel.Name, rel.ForeignKey, err)
		}
		safeTargetTable, err := query.SafeIdent(target.GetTable())
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid target table %q: %w", rel.Name, target.GetTable(), err)
		}
		safeTargetPK, err := query.SafeIdent(target.PrimaryKey)
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid target PK %q: %w", rel.Name, target.PrimaryKey, err)
		}
		out = append(out, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)",
			query.QuoteIdent(safeRelFK), query.QuoteIdent(safeTargetTable), query.QuoteIdent(safeTargetPK)))
	}
	return out, nil
}

// topoSortEntities orders entities so referenced tables come before their
// referencers. Cycles are broken by name-sorted insertion at the cycle
// detection point — this is conservative; SQLite tolerates forward refs in
// CREATE TABLE because FK enforcement is per-statement, not at create time.
func topoSortEntities(all map[string]*entity.Entity) ([]*entity.Entity, error) {
	// Stable input order
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	out := make([]*entity.Entity, 0, len(all))

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
			if rel.Type == entity.RelManyToOne {
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

// SQLType maps a schema FieldType to a SQL column type for the given dialect.
// Postgres needs concrete types (TIMESTAMPTZ, REAL, BOOLEAN); SQLite is more
// permissive but still benefits from explicit declarations.
func SQLType(f schema.Field, dialect Dialect) string {
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

// ColumnDefaultClause returns the trailing " DEFAULT …" fragment a
// column definition should include, or "" when none applies. Centralises
// two decisions every DDL site has to make:
//
//  1. An explicit f.Default always wins — rendered via SQLDefault.
//  2. Otherwise, f.AutoGenerate == AutoUUID on Postgres gets
//     DEFAULT gen_random_uuid() so raw-SQL INSERTs that omit the id
//     column don't crash with a NOT NULL constraint violation. (PG 13+
//     ships gen_random_uuid in core; on older versions it lived in
//     pgcrypto.) SQLite has no built-in UUID generator — the column
//     stays app-managed there to avoid silently doing nothing.
//  3. AutoTimestamp is intentionally NOT auto-defaulted; created_at /
//     updated_at are populated by framework hooks. Auto-emitting now()
//     would create a divergence between SQLite (no DEFAULT, app sets
//     it) and PG (DEFAULT now(), app ALSO sets it, last write wins).
//
// The returned fragment is prefixed with a leading space so callers can
// always concat without inserting one themselves.
func ColumnDefaultClause(f schema.Field, dialect Dialect) string {
	if f.Default != nil {
		return " DEFAULT " + SQLDefault(f, dialect)
	}
	if f.AutoGenerate == schema.AutoUUID && dialect == DialectPostgres {
		return " DEFAULT gen_random_uuid()"
	}
	return ""
}

// SQLDefault returns the SQL DEFAULT value for a field. Booleans render as
// TRUE/FALSE for Postgres and 1/0 for SQLite (both engines accept either,
// but emitting the native form keeps DDL idiomatic and avoids surprises in
// pg_dump output).
func SQLDefault(f schema.Field, dialect Dialect) string {
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
