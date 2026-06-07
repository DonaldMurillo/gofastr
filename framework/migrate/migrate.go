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

// execer is the subset of *sql.DB / *sql.Tx the DDL emitter needs. Taking the
// interface lets AutoMigrate run every entity's DDL inside one transaction
// (passing the *sql.Tx) while MigrateEntity still works against the raw pool.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// AutoMigrate creates tables for all registered entities. Equivalent to
// AutoMigrateContext with a background context. See AutoMigrateContext for the
// full contract (advisory lock + single-transaction atomicity).
func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	return AutoMigrateContext(context.Background(), db, registry)
}

// AutoMigrateContext creates tables for all registered entities. Entities are
// migrated in dependency order so FK targets exist before referencing tables,
// and CREATE TABLE/INDEX IF NOT EXISTS keeps re-runs safe.
//
// Two production guarantees beyond the bare DDL:
//
//   - Advisory lock. The whole run is serialized by a database advisory lock
//     (coremig.WithAdvisoryLock), so booting N replicas at once cannot race
//     two concurrent streams of DDL against the same database. No-op on
//     SQLite, which serializes writers itself.
//   - Atomicity. All DDL runs inside one transaction; a failure on entity K
//     rolls back entities 1..K-1 too, so a botched migration never leaves a
//     half-created schema behind. Both Postgres and SQLite support
//     transactional DDL.
//
// db == nil is a silent no-op, matching the rest of the boot path.
func AutoMigrateContext(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	return AutoMigratePlanContext(ctx, db, Plan{Registry: registry})
}

// AutoMigratePlanContext is AutoMigrateContext for a full Plan — entity and
// raw-Table schema PLUS stored routines (functions / procedures / triggers /
// views). Tables are created first, then each routine's Up runs (idempotent
// CREATE OR REPLACE), all inside the one advisory-locked transaction so the
// whole schema converges atomically.
func AutoMigratePlanContext(ctx context.Context, db *sql.DB, plan Plan) error {
	if db == nil {
		return nil
	}
	var ordered []*entity.Entity
	all := map[string]*entity.Entity{}
	if plan.Registry != nil {
		all = plan.Registry.All()
		var err error
		ordered, err = topoSortEntities(all)
		if err != nil {
			return err
		}
	}
	dialect := DetectDialect(db)
	existing := map[string]bool{}
	if dialect == DialectPostgres && len(ordered) > 0 {
		tableNames := make([]string, 0, len(ordered))
		for _, ent := range ordered {
			tableNames = append(tableNames, ent.GetTable())
		}
		var err error
		existing, err = TableExistsBulk(ctx, db, tableNames, dialect)
		if err != nil {
			return err
		}
	}

	return coremig.WithAdvisoryLock(ctx, db, dialect, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migrate: begin tx: %w", err)
		}
		// Always roll back on the way out. A no-op after a successful Commit,
		// but critical on the error AND panic paths: migrateEntity can panic
		// (e.g. an expression index with no name fails loud), and a leaked open
		// transaction would wedge the pinned connection when it's returned to
		// the pool. The deferred rollback runs before WithAdvisoryLock closes
		// the conn, and the panic still propagates to the caller.
		defer func() { _ = tx.Rollback() }()
		for _, ent := range ordered {
			if err := migrateEntity(ctx, tx, ent, all, dialect, existing[ent.GetTable()]); err != nil {
				return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
			}
		}
		// Views after tables (they SELECT from them), ordered so a view that
		// depends on another view follows it.
		for _, v := range topoSortViews(plan.Views) {
			if _, err := tx.ExecContext(ctx, v.routine(dialect).Up); err != nil {
				return fmt.Errorf("migrate view %s: %w", v.Name, err)
			}
		}
		// Routines after views — a trigger/function may reference a view.
		// CREATE OR REPLACE keeps re-runs idempotent.
		for _, r := range plan.Routines {
			if _, err := tx.ExecContext(ctx, r.Up); err != nil {
				return fmt.Errorf("migrate routine %s: %w", r.Name, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate: commit: %w", err)
		}
		return nil
	})
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
// It does not emit FK constraints since it has no view of the wider registry;
// callers that need foreign keys should call AutoMigrate. The dialect is
// auto-detected from db.
func MigrateEntity(db *sql.DB, ent *entity.Entity) error {
	return migrateEntity(context.Background(), db, ent, nil, DetectDialect(db), false)
}

// MigrateEntityDialect is the explicit-dialect variant used by callers that
// already know the target (e.g. CLI codegen, tests).
func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect Dialect) error {
	return migrateEntity(context.Background(), db, ent, nil, dialect, false)
}

// migrateEntity is the shared implementation. When `all` is non-nil it is
// consulted for FK target tables; missing targets return an error before any
// DDL runs. exec is either the *sql.DB pool (single-entity callers) or the
// shared *sql.Tx (AutoMigrate's atomic run).
func migrateEntity(ctx context.Context, exec execer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, tableExists bool) error {
	// Unmanaged objects (views, FTS virtual tables, external/legacy tables) are
	// created elsewhere — the migration system emits no DDL for them.
	if ent.Config.Unmanaged {
		return nil
	}
	if len(ent.GetFields()) == 0 {
		return nil
	}

	if !tableExists {
		stmt, err := buildCreateTableSQL(ent, all, dialect)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
		}
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return fmt.Errorf("migrate %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}

	// Secondary indices — emit AFTER the table exists. CREATE INDEX IF NOT
	// EXISTS works on both engines so re-running AutoMigrate is idempotent.
	// An index with neither Columns nor Expression is a no-op (legacy: empty
	// Columns used to silently skip; we preserve that for the all-zero case).
	for _, idx := range ent.Config.Indices {
		if len(idx.Columns) == 0 && idx.Expression == "" {
			continue
		}
		if _, err := exec.ExecContext(ctx, indexDDL(safeTable, idx)); err != nil {
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
			// Validate but DON'T quote — same convention as columnDefs. Quoting
			// would make Postgres preserve case here while the unquoted CREATE
			// TABLE folds the column to lowercase, so "UserName" would reference
			// a column that doesn't exist.
			safeCols[i] = query.MustIdent(col)
		}
		body = strings.Join(safeCols, ", ")
	}
	return fmt.Sprintf("CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)",
		unique, safeName, table, body)
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
		// Validated but UNQUOTED — same convention as columnDefs. Quoting would
		// preserve case on Postgres while the referenced CREATE TABLE folded its
		// identifiers to lowercase, so a mixed-case target like "MixedAccount"
		// would resolve to a relation that doesn't exist.
		out = append(out, fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)",
			safeRelFK, safeTargetTable, safeTargetPK))
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
		// name is always present: the outer loop iterates all's keys and every
		// recursive visit(rel.Entity) is guarded by the all[rel.Entity] check
		// below, so no membership test is needed here.
		ent := all[name]
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
	// An explicit RawType wins — the escape hatch for column types the
	// FieldType enum doesn't model (NUMERIC(p,s), INET, custom domains, …).
	if f.RawType != "" {
		return f.RawType
	}
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
