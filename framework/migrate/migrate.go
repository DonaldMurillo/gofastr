package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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

// execQueryer is the subset of *sql.DB / *sql.Tx the migrator needs: Exec for
// DDL, Query to re-read live columns under the advisory lock. Taking the
// interface lets AutoMigrate run every entity's DDL inside one transaction
// (passing the *sql.Tx) while MigrateEntity still works against the raw pool.
type execQueryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// AutoMigrate converges the database schema with all registered entities —
// creates missing tables and adds missing columns. Equivalent to
// AutoMigrateContext with a background context. See AutoMigrateContext for the
// full contract (advisory lock + single-transaction atomicity).
func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	return AutoMigrateContext(context.Background(), db, registry)
}

// AutoMigrateContext converges the database schema with all registered
// entities: missing tables are created, and a field declared on an entity
// whose existing table lacks the column is added via ALTER TABLE ADD COLUMN
// (additive only — boot never drops, renames, or retypes; those stay behind
// `migrate diff --apply`). Entities are migrated in dependency order so FK
// targets exist before referencing tables, and CREATE TABLE/INDEX IF NOT
// EXISTS keeps re-runs safe.
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
//
// Routines declare their engine via Routine.Dialect; routines whose Dialect
// doesn't match the detected dialect are skipped (one slog.Info per boot lists
// them). Every matching routine's Up STILL runs every boot — boot is
// idempotent and self-heals against DB-side drift; it does NOT skip based on
// a stored checksum. The gofastr_routines ledger table records (name, sha256
// of Up, applied_at) for each routine that ran, inside the SAME tx, so an
// agent introspecting the running app can see what landed and what drifted.
// Ledger rows whose name has no registered routine trigger a loud slog.Warn
// (additive-only: boot never auto-drops).
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
	// Partition routines into "applies on this dialect" vs "skipped". The
	// skip set is logged in ONE line per boot so a misconfigured dialect tag
	// (e.g. a Postgres stored proc left in the plan against a SQLite dev DB)
	// is visible without spamming per-routine lines.
	appliedRoutines, skippedRoutines := partitionRoutinesByDialect(plan.Routines, dialect)
	if len(skippedRoutines) > 0 {
		names := make([]string, 0, len(skippedRoutines))
		for _, r := range skippedRoutines {
			names = append(names, r.Name)
		}
		sort.Strings(names)
		slog.Info("migrate: skipping routines declared for a different dialect",
			"declared", dialectString(skippedRoutines[0].Dialect),
			"running", dialectString(dialect),
			"routines", strings.Join(names, ","),
			"count", len(names),
		)
	}
	// Pre-read every managed table's live columns in one pass (existence and
	// column-drift detection in a single bulk query). This runs BEFORE the
	// advisory lock — when drift is detected, addMissingColumns re-reads on the
	// lock-holding connection so a peer replica's concurrent ALTERs are seen.
	liveByTable := map[string]map[string]string{}
	tableNames := make([]string, 0, len(ordered))
	for _, ent := range ordered {
		// Same skip set as migrateEntity: unmanaged objects and field-less
		// entities get no DDL, so they need no live read either.
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 {
			continue
		}
		tableNames = append(tableNames, ent.GetTable())
	}
	if len(tableNames) > 0 {
		var err error
		liveByTable, err = ReadLiveColumnsBulk(ctx, db, tableNames, dialect)
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
			if err := migrateEntity(ctx, tx, ent, all, dialect, liveByTable[ent.GetTable()]); err != nil {
				return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
			}
		}
		// The routine ledger, dialect skip log, orphan WARN, and summary
		// line all apply ONLY when the plan carries at least one routine.
		// Gating here keeps apps that don't use routines from acquiring a
		// phantom gofastr_routines table and a per-boot summary line, and
		// keeps the no-routine sqlmock/sequence tests unchanged.
		if len(plan.Routines) > 0 {
			// Ledger lives in the same tx as routine application so the
			// bookkeeping cannot diverge from what landed on the DB. Created
			// on every boot (IF NOT EXISTS) — cheap and means an older
			// deployment upgrading picks it up without a dedicated migration.
			if err := ensureRoutineLedger(ctx, tx, dialect); err != nil {
				return fmt.Errorf("migrate: ensure routine ledger: %w", err)
			}
			// Previous ledger state is needed to compute the first-time vs
			// changed-vs-unchanged summary. Read inside the tx so we see the
			// pre-this-boot view.
			prevLedger, err := readRoutineLedger(ctx, tx)
			if err != nil {
				return fmt.Errorf("migrate: read routine ledger: %w", err)
			}

			// Views after tables (they SELECT from them), ordered so a view
			// that depends on another view follows it. Views do NOT carry a
			// Dialect tag — their render() already takes dialect and emits
			// the right DDL per engine (PG: CREATE OR REPLACE VIEW; SQLite:
			// DROP IF EXISTS + CREATE; PG MATERIALIZED via the Materialized
			// bool). Adding a redundant Dialect field to View would be
			// ambiguous (Materialized already says "PG-only"), so View
			// stays dialect-untagged.
			for _, v := range topoSortViews(plan.Views) {
				if _, err := tx.ExecContext(ctx, v.routine(dialect).Up); err != nil {
					return fmt.Errorf("migrate view %s: %w", v.Name, err)
				}
			}
			// Routines after views — a trigger/function may reference a
			// view. CREATE OR REPLACE keeps re-runs idempotent. Every
			// matching routine runs every boot; the ledger records the
			// checksum but does not gate.
			for _, r := range appliedRoutines {
				if _, err := tx.ExecContext(ctx, r.Up); err != nil {
					return fmt.Errorf("migrate routine %s: %w", r.Name, err)
				}
			}

			// Upsert ledger rows for every applied routine. The checksum
			// change is how introspection (app_routines) detects "the
			// registered body drifted since last boot" — but the apply
			// above is unconditional.
			var (
				appliedCount   = len(appliedRoutines)
				changedCount   int
				firstTimeCount int
			)
			for _, r := range appliedRoutines {
				newSum := RoutineChecksum(r)
				if oldSum, ok := prevLedger[r.Name]; ok {
					if oldSum != newSum {
						changedCount++
					}
				} else {
					firstTimeCount++
				}
				if err := upsertRoutineLedger(ctx, tx, dialect, r.Name, newSum); err != nil {
					return fmt.Errorf("migrate: record routine %s: %w", r.Name, err)
				}
			}

			// Ledger rows whose name has no registered routine: additive-
			// only means we DO NOT drop them — the operator removed the
			// Routine from code, but boot doesn't know whether the DB
			// object is still needed by ad-hoc SQL, a view, or another
			// service. Surface each one loudly.
			registeredNames := make(map[string]struct{}, len(appliedRoutines)+len(skippedRoutines))
			for _, r := range appliedRoutines {
				registeredNames[r.Name] = struct{}{}
			}
			for _, r := range skippedRoutines {
				// Skipped-by-dialect routines ARE still registered — they
				// should NOT be reported as orphaned. They keep their ledger
				// row from whichever dialect last applied them; on a future
				// dialect switch the matching engine will reconcile.
				registeredNames[r.Name] = struct{}{}
			}
			var orphaned []string
			for name := range prevLedger {
				if _, ok := registeredNames[name]; !ok {
					orphaned = append(orphaned, name)
				}
			}
			sort.Strings(orphaned)
			for _, name := range orphaned {
				slog.Warn("migrate: previously applied routine is no longer registered; not dropped (additive-only)",
					"routine", name,
					"hint", "drop via Routine.Down in a versioned migration, or remove the ledger row manually",
				)
			}

			slog.Info("migrate: routine apply summary",
				"applied", appliedCount,
				"changed", changedCount,
				"first_time", firstTimeCount,
				"skipped", len(skippedRoutines),
				"orphaned", len(orphaned),
			)
		} else {
			// No routines in the plan, but views still need to run — keep
			// the existing view-apply loop here so a plan with views but
			// no routines doesn't silently skip its views.
			for _, v := range topoSortViews(plan.Views) {
				if _, err := tx.ExecContext(ctx, v.routine(dialect).Up); err != nil {
					return fmt.Errorf("migrate view %s: %w", v.Name, err)
				}
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate: commit: %w", err)
		}
		return nil
	})
}

// partitionRoutinesByDialect splits routines into (appliesHere, skippedHere)
// based on whether each routine's declared Dialect matches the running
// dialect. A zero-value Dialect (the empty string) means "runs on every
// dialect" — today's behavior — so untagged routines always land in the
// applies set. The order of both outputs preserves plan.Routines order so
// apply + summary stay deterministic.
func partitionRoutinesByDialect(routines []Routine, running Dialect) (applies, skipped []Routine) {
	applies = make([]Routine, 0, len(routines))
	skipped = make([]Routine, 0)
	for _, r := range routines {
		if r.Dialect == "" || r.Dialect == running {
			applies = append(applies, r)
			continue
		}
		skipped = append(skipped, r)
	}
	return applies, skipped
}

// MigrateEntity creates the table for a single entity if it doesn't exist.
// It does not emit FK constraints since it has no view of the wider registry,
// and it does not add columns to an existing table; callers that need foreign
// keys or column convergence should call AutoMigrate. The dialect is
// auto-detected from db.
func MigrateEntity(db *sql.DB, ent *entity.Entity) error {
	return migrateEntity(context.Background(), db, ent, nil, DetectDialect(db), nil)
}

// MigrateEntityDialect is the explicit-dialect variant used by callers that
// already know the target (e.g. CLI codegen, tests).
func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect Dialect) error {
	return migrateEntity(context.Background(), db, ent, nil, dialect, nil)
}

// migrateEntity is the shared implementation. When `all` is non-nil it is
// consulted for FK target tables; missing targets return an error before any
// DDL runs. exec is either the *sql.DB pool (single-entity callers) or the
// shared *sql.Tx (AutoMigrate's atomic run). live is the table's pre-read
// column set: empty/nil means "table absent (or unknown — single-entity
// callers)" and triggers CREATE TABLE IF NOT EXISTS; non-empty means the
// table pre-exists and is converged additively via addMissingColumns. Column
// adds run BEFORE index DDL so a new field and its index land in one boot.
func migrateEntity(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string) error {
	// Unmanaged objects (views, FTS virtual tables, external/legacy tables) are
	// created elsewhere — the migration system emits no DDL for them.
	if ent.Config.Unmanaged {
		return nil
	}
	if len(ent.GetFields()) == 0 {
		return nil
	}

	if len(live) == 0 {
		stmt, err := buildCreateTableSQL(ent, all, dialect)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
		}
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return err
		}
	} else if err := addMissingColumns(ctx, exec, ent, all, dialect, live); err != nil {
		return err
	}

	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return fmt.Errorf("migrate %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}

	// Secondary indices — emit AFTER the table exists (and after column adds,
	// so an index on a just-added column resolves). CREATE INDEX IF NOT
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

// addMissingColumns converges an EXISTING table additively: every field the
// entity declares that the live table lacks is added via ALTER TABLE ADD
// COLUMN — the exact DDL the declarative diff emits (shared
// diffEntityFromLive path, so boot and `migrate diff` can never disagree).
// Boot never drops, renames, or retypes: destructive changes are filtered
// out here and remain behind `migrate diff --apply --allow-destructive`.
//
// preRead was captured BEFORE the advisory lock, so when it shows drift the
// columns are re-read on the lock-holding connection and the diff recomputed:
// a replica that lost the boot race sees its peer's ALTERs and no-ops instead
// of failing on a duplicate column. (SQLite takes no lock — concurrent
// multi-process boots could still collide there; the failed boot rolls back
// and a restart converges.) In the steady state (no drift) this adds zero
// queries inside the transaction.
func addMissingColumns(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, preRead map[string]string) error {
	adds, err := additiveChanges(ent, all, dialect, preRead)
	if err != nil || len(adds) == 0 {
		return err
	}
	liveNow, err := readLiveColumnsQ(ctx, exec, ent.GetTable(), dialect)
	if err != nil {
		return fmt.Errorf("re-read columns for %s: %w", ent.GetTable(), err)
	}
	if len(liveNow) == 0 {
		// Table vanished between pre-read and lock (or is invisible to this
		// session) — leave it to the CREATE path on the next boot rather than
		// guessing here.
		return nil
	}
	adds, err = additiveChanges(ent, all, dialect, liveNow)
	if err != nil {
		return err
	}
	for _, c := range adds {
		if _, err := exec.ExecContext(ctx, c.SQL); err != nil {
			return fmt.Errorf("%s: %w", c.Summary, err)
		}
	}
	return nil
}

// additiveChanges returns the non-destructive subset of the schema diff for
// one entity against a NON-EMPTY live column set — i.e. only the ALTER TABLE
// ADD COLUMN statements. Callers guarantee live is non-empty (an empty set
// would make diffEntityFromLive emit CREATE TABLE instead).
func additiveChanges(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string) ([]SchemaChange, error) {
	changes, err := diffEntityFromLive(ent, all, dialect, live)
	if err != nil {
		return nil, err
	}
	adds := changes[:0]
	for _, c := range changes {
		if !c.Destructive {
			adds = append(adds, c)
		}
	}
	return adds, nil
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
//  3. AutoTimestamp is intentionally NOT auto-defaulted. created_at is
//     populated at insert time by the auto-generate path; updated_at is
//     stamped by crud's doUpdate on every write. Auto-emitting now() in
//     the DDL would create a divergence between SQLite (no DEFAULT, app
//     sets it) and PG (DEFAULT now(), app ALSO sets it, last write wins).
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
