package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"regexp"
	"slices"
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
// DB in line. The diff covers ADD COLUMN, DROP COLUMN, RENAME COLUMN (with an
// explicit Renames hint), and column type changes. Type changes are flagged
// destructive: Postgres gets an in-place ALTER COLUMN TYPE, SQLite gets a
// full table rebuild (see sqliteRebuildChange), and both are refused unless
// the caller opts in — a type change can need data conversion the diff
// cannot infer.

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

	// sqliteRebuildOf names the entity whose table this change rebuilds (a
	// SQLite retype). ApplySchemaDiffWithOptions runs a change set holding
	// one with foreign keys off, and GeneratePlan refuses one that a
	// row-mutating ON DELETE action references.
	sqliteRebuildOf string
	// rebuiltTable is that entity's table as declared, so the apply
	// can read the triggers its DROP TABLE takes.
	rebuiltTable string

	// deferredFKs are the declared foreign keys this Postgres key change
	// detaches and cannot re-add yet, because their child table gains the
	// key's columns only in its own changes: the caller runs them after
	// every table's changes.
	deferredFKs []SchemaChange

	// irreversible marks a change with no inverse at all (a SQLite
	// rebuild, or a key change off a column the diff drops). A generated
	// migration holding one ships no Down: the other changes' inverses
	// alone would roll back to a table that matches neither schema.
	irreversible bool
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
	if err := checkIndexNames(ordered, all); err != nil {
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
	seenThrough := make(map[string]bool)
	for _, ent := range ordered {
		for _, rel := range ent.Config.Relations {
			if rel.Type == entity.RelManyToMany && rel.Through != "" {
				key := strings.ToLower(rel.Through)
				if !seenThrough[key] {
					seenThrough[key] = true
					tables = append(tables, rel.Through)
				}
			}
		}
	}
	liveByTable, err := ReadLiveColumnsBulk(ctx, db, tables, dialect)
	if err != nil {
		return nil, err
	}
	existing := existingEntityTables(ordered, liveByTable)
	keysByTable, err := readLiveKeysQ(ctx, db, existing, dialect)
	if err != nil {
		return nil, err
	}
	fksByTable, err := readLiveForeignKeysQ(ctx, db, existing, dialect)
	if err != nil {
		return nil, err
	}
	canonicalRefTables(fksByTable, all, dialect)
	env := &fkDiffEnv{fks: fksByTable, tables: liveByTable, keyChanged: map[string][]string{}}
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue
		}
		var lk *liveKey
		if len(liveByTable[ent.GetTable()]) > 0 {
			k := keysByTable[ent.GetTable()]
			lk = &k
		}
		changes, err := diffEntity(ent, all, dialect, liveByTable[ent.GetTable()], lk, env)
		if err != nil {
			return nil, fmt.Errorf("diff %s: %w", ent.GetName(), err)
		}
		out = append(out, changes...)
	}
	out = append(out, env.deferred...)

	pivotChanges, err := diffPivotTables(ordered, all, dialect, liveByTable)
	if err != nil {
		return nil, err
	}
	out = append(out, pivotChanges...)
	return out, nil
}

// diffPivotTables generates CREATE TABLE and CREATE INDEX statements for ManyToMany
// pivot tables that do not have their own entity declaration.
func diffPivotTables(ordered []*entity.Entity, all map[string]*entity.Entity, dialect Dialect, existingTables map[string]map[string]string) ([]SchemaChange, error) {
	var out []SchemaChange
	for ent, rel := range generatedPivots(ordered, all) {
		if cols := lookupTableCols(existingTables, rel.Through); len(cols) > 0 {
			continue
		}
		spec, err := buildPivotSpec(ent, rel, all, dialect)
		if err != nil {
			return nil, err
		}
		out = append(out, SchemaChange{
			Summary: fmt.Sprintf("%s: create pivot table", rel.Through),
			SQL:     spec.DDL,
			Down:    spec.DropDDL,
		})
		out = append(out, SchemaChange{
			Summary: fmt.Sprintf("%s: index %s", rel.Through, rel.ForeignKeyTarget),
			SQL:     spec.IndexDDL,
			Down:    spec.DropIndexDDL,
		})
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
	for _, c := range changes {
		if c.sqliteRebuildOf != "" {
			return applySQLiteRebuild(ctx, db, changes)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	if i, err := execChanges(ctx, tx, changes); err != nil {
		_ = tx.Rollback()
		return i, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(changes), nil
}

// newViolations returns the entries of after that before does not account
// for, counting duplicates (one entry per violating row).
func newViolations(before, after []string) []string {
	seen := make(map[string]int, len(before))
	for _, v := range before {
		seen[v]++
	}
	var out []string
	for _, v := range after {
		if seen[v] > 0 {
			seen[v]--
			continue
		}
		out = append(out, v)
	}
	return out
}

// execChanges runs each change's SQL on tx, returning the index of the first
// one that fails.
func execChanges(ctx context.Context, tx *sql.Tx, changes []SchemaChange) (int, error) {
	for i, c := range changes {
		if _, err := tx.ExecContext(ctx, c.SQL); err != nil {
			return i, fmt.Errorf("apply %q: %w", c.Summary, err)
		}
	}
	return len(changes), nil
}

// applySQLiteRebuild applies a change set that rebuilds a SQLite table. The
// rebuild's DROP TABLE would otherwise run every ON DELETE action against
// the rows that reference the table: a generated pivot (always CASCADE)
// lost its rows and a SET NULL child lost its link. The set runs in one
// transaction on a connection with foreign keys off, and is rolled back if
// PRAGMA foreign_key_check finds a dangling reference before commit.
//
// Each change runs through execRebuild, which puts back the triggers the
// DROP takes.
func applySQLiteRebuild(ctx context.Context, db *sql.DB, changes []SchemaChange) (int, error) {
	applied := 0
	err := withSQLiteRebuildConn(ctx, db, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		// Only violations the change set introduces count: a dangling row
		// that predates it (written while enforcement was off) is not this
		// rebuild's to refuse.
		before, err := foreignKeyViolations(ctx, tx)
		if err != nil {
			return fmt.Errorf("verify foreign keys before the rebuild: %w", err)
		}
		for i, c := range changes {
			if err := execRebuild(ctx, tx, c); err != nil {
				applied = i
				return fmt.Errorf("apply %q: %w", c.Summary, err)
			}
		}
		after, err := foreignKeyViolations(ctx, tx)
		if err != nil {
			return fmt.Errorf("verify foreign keys after the rebuild: %w", err)
		}
		if violations := newViolations(before, after); len(violations) > 0 {
			return fmt.Errorf("the rebuild would leave %d foreign key violation(s): %s; rolled back",
				len(violations), strings.Join(violations, ", "))
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		applied = len(changes)
		return nil
	})
	return applied, err
}

func diffEntityFromLive(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string) ([]SchemaChange, error) {
	return diffEntity(ent, all, dialect, live, nil, nil)
}

// diffEntity is diffEntityFromLive with the table's live (or snapshot)
// primary key. A nil lk means the key is unknown, and the key is not
// compared. env carries the other tables' column sets, which a Postgres key
// change needs to detach the foreign keys onto it, and, when its fks are
// known (DiffSchema), the live foreign keys to compare; nil skips both.
func diffEntity(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string, lk *liveKey, env *fkDiffEnv) ([]SchemaChange, error) {
	qtable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return nil, fmt.Errorf("invalid table name %q: %w", ent.GetTable(), err)
	}
	live = uncutLiveColumns(ent, live, dialect)
	if lk != nil {
		lk = &liveKey{constraint: lk.constraint, cols: uncutKey(ent, lk.cols, dialect)}
	}

	if len(live) == 0 {
		// Table missing entirely: emit a CREATE TABLE via the same path
		// AutoMigrate uses, captured as SQL string.
		ddl, err := buildCreateTableSQL(ent, all, dialect)
		if err != nil {
			return nil, err
		}
		var sqlParts []string
		sqlParts = append(sqlParts, ddl)
		for _, idx := range entityIndices(ent) {
			sqlParts = append(sqlParts, indexDDL(dialect, qtable, idx))
		}
		return []SchemaChange{{
			Summary: fmt.Sprintf("%s: create table", ent.GetName()),
			SQL:     strings.Join(sqlParts, ";\n"),
			Down:    fmt.Sprintf("DROP TABLE IF EXISTS %s", ddlIdent(qtable)),
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
	renameNewByOld := map[string]string{} // old (lower) → new, so the live key reads by the names the renames give it
	// Sorted by the old name: map order would give the same plan a
	// different statement order on every run.
	for _, oldName := range slices.Sorted(maps.Keys(ent.Config.Renames)) {
		newName := ent.Config.Renames[oldName]
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
			SQL:     fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", ddlIdent(qtable), ddlIdent(qOld), ddlIdent(qNew)),
			Down:    fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", ddlIdent(qtable), ddlIdent(qNew), ddlIdent(qOld)),
		})
		renameTargets[newLow] = true
		renameSources[oldLow] = true
		renameOldByNew[newLow] = oldLow
		renameNewByOld[oldLow] = newName
	}

	// ADD COLUMN for declared-but-missing fields.
	added := map[string]string{} // column → type, for the rebuild below
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
		colType := columnSQLType(ent, f, all, dialect)
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
			ddlIdent(qtable), ddlIdent(qcol), colType, nullable, defaultClause)
		added[f.Name] = colType
		changes = append(changes, SchemaChange{
			Summary: summary,
			SQL:     ddl,
			Down:    fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", ddlIdent(qtable), ddlIdent(qcol)),
		})
		// CREATE TABLE writes UNIQUE inline, but SQLite refuses UNIQUE on
		// ADD COLUMN, so the added column gets a unique index instead, named
		// the way Postgres names the inline constraint. Its Down runs before
		// the column's, which SQLite needs to drop an indexed column.
		if f.Unique {
			uqName, err := uniqueIndexName(ent, f.Name)
			if err != nil {
				return nil, err
			}
			changes = append(changes, SchemaChange{
				Summary: fmt.Sprintf("%s: unique index %s", ent.GetName(), uqName),
				SQL:     createIndexSQL(dialect, true, uqName, qtable, ddlIdent(qcol)),
				Down:    dropIndexSQL(qtable, uqName),
			})
		}
		for _, idx := range entityIndices(ent) {
			if len(idx.Columns) == 1 && strings.EqualFold(idx.Columns[0], f.Name) {
				idxDDL := indexDDL(dialect, qtable, idx)
				idxName := parseIndexName(idxDDL)
				safeIdxName, err := query.SafeIdent(idxName)
				if err != nil {
					continue
				}
				changes = append(changes, SchemaChange{
					Summary: fmt.Sprintf("%s: index %s", ent.GetName(), safeIdxName),
					SQL:     idxDDL,
					Down:    dropIndexSQL(qtable, safeIdxName),
				})
			}
		}
	}

	// TYPE CHANGE for declared-and-present columns whose type drifted. A type
	// change can need a data-specific conversion (a Postgres USING clause; a
	// SQLite table rebuild), so it is flagged Destructive and refused by
	// default, surfaced for review, never silently applied. Types are
	// normalized per dialect (see canonicalType) so PG information_schema
	// names ("character varying", "timestamp with time zone") don't
	// false-positive. Postgres gets a per-column in-place ALTER; SQLite has
	// no in-place ALTER COLUMN TYPE at all, so its retypes are collected and
	// rendered as ONE table-rebuild change after the DROP loop (the rebuild
	// must run last: it recreates the table from the entity, so it has to
	// see the post-rename/post-add/post-drop column set).
	var sqliteRetypes []retypeCol
	defaultOwned := map[string]bool{}
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
		declaredType := columnSQLType(ent, f, all, dialect)
		if typesEquivalent(declaredType, liveType, dialect) {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		if dialect == DialectSQLite {
			sqliteRetypes = append(sqliteRetypes, retypeCol{name: f.Name, liveType: liveType, declaredType: declaredType})
			continue
		}
		if isTextType(liveType) && isIntegerType(declaredType) {
			defaultOwned[nameLow] = true // the USING form sets the declared default
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: change column %s type %s → %s (destructive: data conversion may be required)", ent.GetName(), f.Name, liveType, declaredType),
			SQL:         alterColumnTypeSQL(qtable, qcol, liveType, declaredType, ColumnDefaultClause(f, dialect)),
			Down:        alterColumnTypeSQL(qtable, qcol, declaredType, liveType, ""),
			Destructive: true,
		})
	}

	// PRIMARY KEY change. Postgres swaps the constraint here, before the
	// DROP loop, because dropping a column drops the constraint holding it.
	// SQLite cannot alter a key in place: the change joins the rebuild below,
	// and the old key columns the entity no longer declares leave with the
	// rebuild, since SQLite refuses DROP COLUMN on a key column.
	//
	// The renames above run first, so a renamed key column is compared and
	// altered under its new name: renaming the key column alone is no key
	// change.
	if lk != nil && len(renameNewByOld) > 0 {
		k := liveKey{constraint: lk.constraint, cols: slices.Clone(lk.cols)}
		for i, c := range k.cols {
			if n, ok := renameNewByOld[strings.ToLower(c)]; ok {
				k.cols[i] = n
			}
		}
		lk = &k
	}
	keyChanged := lk != nil && !sameKey(lk.cols, ent.KeyColumns())
	var rebuildDrops []string
	if keyChanged && dialect == DialectPostgres {
		var tables map[string]map[string]string
		var extraKeep []string
		if env != nil {
			tables = env.tables
			// A live key no relation declares is reported by its own
			// table's diff as a destructive drop, so the detach may drop
			// it here: the change set does not apply without
			// AllowDestructive.
			for _, child := range slices.Sorted(maps.Keys(env.fks)) {
				for _, fk := range env.fks[child] {
					if strings.EqualFold(fk.refTable, ent.GetTable()) {
						extraKeep = append(extraKeep, fkKeepKey(child, fk.cols))
					}
				}
			}
		}
		c, err := postgresKeyChange(ent, all, qtable, *lk, tables, extraKeep)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
		if env != nil {
			if env.keyChanged != nil {
				env.keyChanged[strings.ToLower(ent.GetTable())] = lk.cols
			}
			env.deferred = append(env.deferred, c.deferredFKs...)
		}
	}

	// DROP COLUMN for live-but-undeclared (skip framework-managed columns).
	// Sorted for stable output.
	liveNames := make([]string, 0, len(live))
	for name := range live {
		liveNames = append(liveNames, name)
	}
	sort.Strings(liveNames)
	for _, name := range liveNames {
		if _, ok := declared[strings.ToLower(name)]; ok {
			continue
		}
		if isFrameworkManagedColumn(name, ent) {
			continue
		}
		if renameSources[strings.ToLower(name)] {
			continue // handled by RENAME COLUMN above
		}
		if keyChanged && dialect == DialectSQLite && keyHas(lk.cols, name) {
			rebuildDrops = append(rebuildDrops, name)
			continue
		}
		qcol, err := query.SafeIdent(name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", name, err)
		}
		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", ddlIdent(qtable), ddlIdent(qcol))
		// Inverse re-adds the column with the type it had in the previous
		// snapshot. Reversible at the schema level: row data is not restored.
		downType := live[name]
		if downType == "" {
			downType = "TEXT"
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: drop column %s", ent.GetName(), name),
			SQL:         ddl,
			Down:        fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", ddlIdent(qtable), ddlIdent(qcol), downType),
			Destructive: true,
		})
	}

	// FOREIGN KEY changes, when the live keys are known. Postgres swaps the
	// constraints after the columns above converged; SQLite rebuilds the
	// table, whose CREATE renders the declared keys.
	fkDiffer := false
	if env != nil && env.fks != nil {
		fkChanges, differ, err := foreignKeyChanges(ent, all, dialect, qtable, env, renameNewByOld)
		if err != nil {
			return nil, err
		}
		changes = append(changes, fkChanges...)
		fkDiffer = differ && dialect == DialectSQLite
	}

	// Unique set or cleared on an existing column, when the snapshot
	// knows the table's unique columns (see unique.go). A SQLite clear
	// rebuilds the table below. An add that precedes a rebuild is spent
	// work, since the DROP takes its index and the new CREATE writes the
	// UNIQUE inline, but it leaves the same table.
	var uniqueCleared []string
	if env != nil && env.uniques != nil {
		if prevUnique, ok := lookupKeyCase(env.uniques, ent.GetTable()); ok {
			adds, drops, cleared, err := uniqueFlipChanges(ent, dialect, qtable, liveLower, renameOldByNew, prevUnique)
			if err != nil {
				return nil, err
			}
			uniqueCleared = cleared
			changes = append(changes, drops...)
			changes = append(changes, adds...)
		}
	}
	uniqueRebuild := len(uniqueCleared) > 0 && dialect == DialectSQLite

	// NOT NULL or DEFAULT changed on an existing column, when the
	// snapshot recorded the table's CREATE TABLE (see
	// column_constraint.go). A SQLite change rebuilds the table below.
	var constraintChanged []string
	if env != nil && env.tableDDL != nil {
		if prevDDL := lookupStringCase(env.tableDDL, ent.GetTable()); prevDDL != "" {
			alters, changed, err := constraintChanges(ent, dialect, qtable, prevDDL, renameOldByNew, defaultOwned)
			if err != nil {
				return nil, err
			}
			constraintChanged = changed
			changes = append(changes, alters...)
		}
	}
	constraintRebuild := len(constraintChanged) > 0 && dialect == DialectSQLite

	// The SQLite retype rebuild goes LAST: it recreates the table from the
	// entity, so it must see the column set the renames/adds/drops above
	// already converged. One change per table, no matter how many columns
	// retyped — the rebuild converges every declared type at once.
	if len(sqliteRetypes) > 0 || (keyChanged && dialect == DialectSQLite) || fkDiffer || uniqueRebuild || constraintRebuild {
		var parts []string
		for _, r := range sqliteRetypes {
			parts = append(parts, fmt.Sprintf("%s type %s → %s", r.name, r.liveType, r.declaredType))
		}
		summary := ""
		if len(parts) > 0 {
			summary = "change column " + strings.Join(parts, ", ")
		}
		if keyChanged {
			summary = strings.TrimPrefix(summary+"; "+keyChangeSummary(lk.cols, ent.KeyColumns()), "; ")
		}
		for _, name := range rebuildDrops {
			summary += "; drop column " + name
		}
		if fkDiffer {
			summary = strings.TrimPrefix(summary+"; change foreign keys to the declared relations", "; ")
		}
		if uniqueRebuild {
			summary = strings.TrimPrefix(summary+"; drop unique on "+strings.Join(uniqueCleared, ", "), "; ")
		}
		if constraintRebuild {
			summary = strings.TrimPrefix(summary+"; change NOT NULL or default on "+strings.Join(constraintChanged, ", "), "; ")
		}
		// The ADD changes above run first, so by the time the rebuild runs
		// the old table holds the columns they add, filled from their
		// defaults, and the rebuild copies them like any other. Leaving
		// them out of the copy left a key change onto new columns nothing
		// to copy: the INSERT was skipped and the DROP still ran, emptying
		// the table.
		rebuildLive := maps.Clone(live)
		maps.Copy(rebuildLive, added)
		rebuild, err := sqliteRebuildChange(ent, all, dialect, rebuildLive, renameOldByNew, summary, false)
		if err != nil {
			return nil, err
		}
		changes = append(changes, rebuild)
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
		case "smallserial":
			t = "smallint"
		}
	}
	return t
}

// typesEquivalent reports whether a declared type and a live-DB type are the
// same after per-dialect canonicalization. Conservative by design: it only
// asserts equality, never "close enough". A real type change always differs
// canonically, and a dialect-naming variant never does.
//
// One widening is taken as in sync: an Int is BIGINT on Postgres, and a
// table an older build made holds it as INTEGER (or a SERIAL key's
// integer). Proposing a retype of every Int column in every app, plus
// the int4 sequence behind each SERIAL that ALTER COLUMN TYPE leaves
// alone, is not a change the declaration asked for; widening an old
// column is a hand-written migration (see migrations.md).
//
// A VARCHAR's width counts on Postgres, which enforces it: a String whose
// Max went from 50 to 200 kept its VARCHAR(50), validation passed a
// 150-character value, and the insert failed. A side that names no width
// (an older snapshot, a catalog read without one) is not compared. SQLite
// ignores the width, so a change there rebuilds nothing.
func typesEquivalent(declared, live string, dialect Dialect) bool {
	d, l := canonicalType(declared, dialect), canonicalType(live, dialect)
	if dialect == DialectPostgres && d == "bigint" && l == "integer" {
		return true
	}
	if d != l {
		return false
	}
	if dialect == DialectPostgres && (d == "varchar" || d == "character" || d == "char") {
		dw, dok := typeWidth(declared)
		lw, lok := typeWidth(live)
		return !dok || !lok || dw == lw
	}
	return true
}

// typeWidth returns the parenthesized size of a type ("VARCHAR(255)",
// "character varying(255)"), and whether it names one.
func typeWidth(t string) (string, bool) {
	open := strings.IndexByte(t, '(')
	end := strings.LastIndexByte(t, ')')
	if open < 0 || end < open {
		return "", false
	}
	return strings.ReplaceAll(t[open+1:end], " ", ""), true
}

// alterColumnTypeSQL renders the Postgres DDL for a column type change: an
// in-place ALTER COLUMN TYPE. Text to an integer type has no assignment
// cast, so Postgres refuses it without a USING clause; that one gets an
// explicit cast, which converts numeric text and fails on anything else.
// Other changes keep the assignment cast, which refuses what an explicit
// cast would truncate (TEXT to VARCHAR(n)). SQLite never reaches this
// function — its retypes are rendered by sqliteRebuildChange, because
// SQLite has no in-place ALTER COLUMN TYPE and a rendered comment would
// execute as a no-op while being counted as applied.
//
// USING converts the rows but not the column's default: a text default
// ('0') cannot be cast automatically, so the ALTER failed. The USING
// form drops the default first and sets defaultClause (the declared
// field's " DEFAULT …", or "" for none) after, in one ALTER TABLE, the
// spelling the Postgres manual gives for this case.
func alterColumnTypeSQL(qtable, qcol, fromType, newType, defaultClause string) string {
	qtable, qcol = ddlIdent(qtable), ddlIdent(qcol)
	if isTextType(fromType) && isIntegerType(newType) {
		sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT, ALTER COLUMN %s TYPE %s USING %s::%s",
			qtable, qcol, qcol, newType, qcol, newType)
		if defaultClause != "" {
			sql += fmt.Sprintf(", ALTER COLUMN %s SET%s", qcol, defaultClause)
		}
		return sql
	}
	return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", qtable, qcol, newType)
}

// isTextType reports whether a Postgres column type holds text.
func isTextType(t string) bool {
	switch canonicalType(t, DialectPostgres) {
	case "text", "varchar", "char", "character":
		return true
	}
	return false
}

// isIntegerType reports whether a Postgres column type is an integer.
func isIntegerType(t string) bool {
	switch canonicalType(t, DialectPostgres) {
	case "smallint", "integer", "int", "int2", "int4", "int8", "bigint":
		return true
	}
	return false
}

// retypeCol is one column whose declared type drifted from the live/snapshot
// type; SQLite retypes are collected into a single table rebuild.
type retypeCol struct {
	name         string
	liveType     string
	declaredType string
}

// sqliteRebuildChange renders the SQLite table rebuild that performs the
// collected column type changes or a primary-key change: create the new table, copy the shared
// columns across, drop the old table, rename, then recreate the entity's
// declared indices (DROP TABLE destroys them with the old table). This is
// SQLite's own documented procedure for the ALTER COLUMN TYPE it does not
// support; anything less either no-ops (a comment is not DDL) or loses data
// (drop + re-add the column).
//
// Fidelity, stated plainly:
//   - The new table is rendered from the ENTITY (columnDefs: types, PK,
//     NOT NULL, defaults, FK clauses), the same single column-rendering
//     path AutoMigrate uses, so the rebuild converges to exactly what a
//     fresh deploy would create. Columns SQLite constraints that only
//     existed on the old table (hand-added CHECKs, non-declared unique
//     indices) are not carried over.
//   - Live framework-managed columns the entity does not declare
//     (created_at with timestamps off, etc.) are preserved, matching the
//     DROP guard's posture, and copied across. keepUndeclared preserves
//     every undeclared live column: boot, which never drops a column,
//     rebuilds an empty table that way for a key change.
//   - Row data is carried over by INSERT ... SELECT; values convert by
//     SQLite column affinity. A value that cannot convert losslessly
//     keeps its old storage class (SQLite's rule), so review the copy
//     for exactly the columns named in the summary.
//   - Down is empty: the only faithful inverse needs the pre-migration
//     DDL with its full constraints, which neither the live column map
//     nor the snapshot's column→type form carries. A column-map
//     reconstruction would also double-apply the sibling ADD/DROP Downs
//     that run around it in reverse order. The change is forward-only;
//     SchemaChange.Down documents "empty when no safe inverse is known".
//   - DROP TABLE runs every ON DELETE action against the rows that
//     reference this table while foreign keys are on. ApplySchemaDiff runs
//     the change with them off (applySQLiteRebuild); GeneratePlan, whose
//     file runs inside the migration runner's transaction where the pragma
//     cannot change, refuses a rebuild a row-mutating action references.
func sqliteRebuildChange(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string, renameOldByNew map[string]string, summary string, keepUndeclared bool) (SchemaChange, error) {
	qtable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return SchemaChange{}, fmt.Errorf("invalid table name %q: %w", ent.GetTable(), err)
	}
	liveLower := make(map[string]string, len(live))
	for k, v := range live {
		liveLower[strings.ToLower(k)] = v
	}

	defs, err := columnDefs(ent, all, dialect, false)
	if err != nil {
		return SchemaChange{}, err
	}

	// Copy list: declared columns that exist live (a rename target counts —
	// the RENAME change above already ran, so the column carries its new
	// name by the time the rebuild executes). Managed live columns are
	// appended to both the new table and the copy list.
	copyCols := make([]string, 0, len(live))
	declaredLower := make(map[string]bool, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		low := strings.ToLower(f.Name)
		declaredLower[low] = true
		present := low
		if old, renamed := renameOldByNew[low]; renamed {
			present = old
		}
		if _, ok := liveLower[present]; !ok {
			continue // not in the old table: its default fills the new one
		}
		q, err := query.SafeIdent(f.Name)
		if err != nil {
			return SchemaChange{}, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		copyCols = append(copyCols, q)
	}
	managed := make([]string, 0)
	for name, typ := range live {
		low := strings.ToLower(name)
		if declaredLower[low] || (!keepUndeclared && !isFrameworkManagedColumn(low, ent)) {
			continue // undeclared non-managed columns were dropped by the DROP loop
		}
		managed = append(managed, name)
		_ = typ
	}
	sort.Strings(managed)
	// Kept columns go before a composite key's table constraint, which
	// columnDefs appends last: SQLite wants every column first.
	var kept []string
	for _, name := range managed {
		q, err := query.SafeIdent(name)
		if err != nil {
			return SchemaChange{}, fmt.Errorf("invalid column name %q: %w", name, err)
		}
		kept = append(kept, fmt.Sprintf("%s %s", ddlIdent(q), liveLower[strings.ToLower(name)]))
		copyCols = append(copyCols, q)
	}
	at := len(defs)
	if len(ent.KeyColumns()) > 1 {
		at--
	}
	defs = slices.Insert(defs, at, kept...)
	if all != nil {
		fks, err := foreignKeyClauses(ent, all, DialectSQLite)
		if err != nil {
			return SchemaChange{}, err
		}
		defs = append(defs, fks...)
	}

	newTable := sqliteRebuildTable(qtable)
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n\t%s\n)", newTable, strings.Join(defs, ",\n\t"))
	if len(copyCols) > 0 {
		joined := strings.Join(ddlIdents(copyCols), ", ")
		fmt.Fprintf(&b, ";\nINSERT INTO %s (%s) SELECT %s FROM %s", newTable, joined, joined, ddlIdent(qtable))
	}
	fmt.Fprintf(&b, ";\nDROP TABLE %s", ddlIdent(qtable))
	fmt.Fprintf(&b, ";\n%s", sqliteRebuildRename(qtable))
	// The DROP took every index with it. entityIndices is the set the
	// snapshot records (declared plus the automatic belongs_to ones), so
	// anything less leaves an index the next plan believes exists.
	for _, idx := range entityIndices(ent) {
		b.WriteString(";\n")
		b.WriteString(indexDDL(DialectSQLite, qtable, idx))
	}
	return SchemaChange{
		Summary: fmt.Sprintf("%s: %s (destructive: SQLite cannot alter column types or a primary key in place; table is rebuilt and rows are copied with affinity conversion)",
			ent.GetName(), summary),
		SQL:             b.String(),
		Destructive:     true,
		sqliteRebuildOf: ent.GetName(),
		rebuiltTable:    ent.GetTable(),
		irreversible:    true,
	}, nil
}

// execRebuild runs c and, when c rebuilds a SQLite table, puts back the
// table's own triggers, which its DROP TABLE takes. A generated file and
// boot put declared routines back by running them, but a trigger written
// by hand is no routine, and the live apply has no routines at all. The
// triggers are read just before c runs, not before its change set, so
// what an earlier RENAME COLUMN in the set rewrote in their bodies stays.
func execRebuild(ctx context.Context, exec execQueryer, c SchemaChange) error {
	var triggers []string
	if c.rebuiltTable != "" {
		var err error
		if triggers, err = sqliteTableTriggers(ctx, exec, c.rebuiltTable); err != nil {
			return fmt.Errorf("read the table's triggers: %w", err)
		}
	}
	if _, err := exec.ExecContext(ctx, c.SQL); err != nil {
		return err
	}
	for _, ddl := range triggers {
		if _, err := exec.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("recreate a trigger the rebuild dropped: %w", err)
		}
	}
	return nil
}

// sqliteTableTriggers returns the CREATE TRIGGER statement of each trigger
// on table, in its schema, as sqlite_master records it, oldest first.
// table must already be validated.
func sqliteTableTriggers(ctx context.Context, q queryer, table string) ([]string, error) {
	schema, base := splitTable(table)
	// SQLite resolves table names without case, so a trigger written
	// ON Widgets is on widgets; sqlite_master compares exactly unless told.
	// SQLite fires a table's triggers for one event newest first, so they
	// are recreated in the order they were created (rowid order).
	rows, err := q.QueryContext(ctx, "SELECT sql FROM "+sqliteMaster(table)+
		" WHERE type = 'trigger' AND tbl_name = ? COLLATE NOCASE ORDER BY rowid", base)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ddl string
		if err := rows.Scan(&ddl); err != nil {
			return nil, err
		}
		// sqlite_master records the DDL without its schema, and an
		// unqualified CREATE TRIGGER lands in main.
		out = append(out, inSchema(ddl, schema))
	}
	return out, rows.Err()
}

// sqliteRebuildTable names the replacement table a SQLite rebuild fills.
func sqliteRebuildTable(qtable string) string { return qtable + "__rebuild" }

// sqliteRebuildRename is the statement that gives the replacement table
// the original's name, the last step of a SQLite rebuild.
// SQLite takes a schema on the table it renames but not on RENAME TO: the
// table stays in its schema.
func sqliteRebuildRename(qtable string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", sqliteRebuildTable(qtable), ddlIdent(tableBase(qtable)))
}

// queryer is the read-only subset of *sql.DB / *sql.Tx the live-schema
// readers need. The *sql.Tx form lets AutoMigrate re-read columns on the
// advisory-lock-holding connection (a separate pool connection would deadlock
// a MaxOpenConns(1) pool).
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ReadLiveColumns returns a map of column name → column type from the live
// DB. Empty map means "table doesn't exist". On Postgres the type is
// information_schema's data_type plus the width a character type declares
// ("character varying(255)", "character(2)"; "text", "bigint" and the like
// carry none). On SQLite it is the type the table was declared with, as
// PRAGMA table_info reports it.
func ReadLiveColumns(ctx context.Context, db *sql.DB, table string, dialect Dialect) (map[string]string, error) {
	return readLiveColumnsQ(ctx, db, table, dialect)
}

func readLiveColumnsQ(ctx context.Context, q queryer, table string, dialect Dialect) (map[string]string, error) {
	if dialect == DialectPostgres {
		return readLiveColumnsPostgresQ(ctx, q, table, false)
	}
	return readLiveColumnsSQLiteQ(ctx, q, table)
}

// readEntityColumnsQ is readLiveColumnsQ for a table AutoMigrate created.
// Its DDL is unquoted, so on Postgres the table is the folded name and
// nothing else: a hand-made quoted "MixedAccount" is another table, and
// reading its columns as the entity's skipped an ADD COLUMN for a column
// only that table has.
func readEntityColumnsQ(ctx context.Context, q queryer, table string, dialect Dialect) (map[string]string, error) {
	if dialect == DialectPostgres {
		return readLiveColumnsPostgresQ(ctx, q, table, true)
	}
	return readLiveColumnsSQLiteQ(ctx, q, table)
}

// ReadLiveColumnsPostgres is ReadLiveColumns on Postgres: column name →
// data_type, with a character type's width ("character varying(255)").
func ReadLiveColumnsPostgres(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	return readLiveColumnsPostgresQ(ctx, db, table, false)
}

func readLiveColumnsPostgresQ(ctx context.Context, q queryer, table string, foldedOnly bool) (map[string]string, error) {
	// AutoMigrate emits CREATE TABLE with UNQUOTED identifiers, so Postgres
	// folds a mixed-case table name to lowercase in information_schema.
	// The exported readers match without case, so a registry table like
	// "MixedAccount" finds its folded live columns and a table an older
	// build created quoted (an "AuditTrail" audit table) finds its own. Boot reads
	// its entity tables foldedOnly, and then its columns too: a quoted
	// legacy "Title" is not the folded title the framework's DDL names.
	// A qualified app.widgets is widgets in schema app; the bare name is
	// in the search-path schema.
	schema, name := splitTable(table)
	name = pgIdentCut(name)
	var schemaArg any
	if schema != "" {
		schemaArg = pgIdentCut(schema)
	}
	where := `lower(table_schema) = lower(coalesce($2, current_schema())) AND lower(table_name) = lower($1)`
	if foldedOnly {
		where = `table_schema = coalesce(lower($2), current_schema()) AND table_name = lower($1) AND column_name = lower(column_name)`
	}
	rows, err := q.QueryContext(ctx, `
		SELECT column_name, `+pgColumnType("")+`
		FROM information_schema.columns
		WHERE `+where, name, schemaArg)
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
	pragma, err := sqlitePragma("table_info", table)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, pragma)
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
	columns, err := columnDefs(ent, all, dialect, all != nil)
	if err != nil {
		return "", fmt.Errorf("entity %s: %w", ent.GetName(), err)
	}
	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return "", fmt.Errorf("entity %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		ddlIdent(safeTable), strings.Join(columns, ",\n\t")), nil
}

// columnDefs builds the per-column DDL fragments (validated name + type +
// column constraints) plus the FK clauses (when withFKs is set; all must
// then be non-nil). all also resolves a foreign key's type from the key it
// references, so the SQLite rebuild, which re-adds the live foreign keys
// itself, passes all with withFKs off. Shared by buildCreateTableSQL so
// there is exactly one column-rendering path.
//
// Identifiers are VALIDATED (SafeIdent rejects anything but [A-Za-z0-9_.]) and
// left bare, except a keyword, which ddlIdent quotes folded (query.QuoteFolded,
// the spelling CRUD's runtime SQL uses for every name). Quoting a name as
// written would make Postgres preserve case in the schema while the runtime
// folds it to lowercase, breaking any mixed-case identifier. Validation alone
// provides the injection safety.
//
// A one-column key is marked inline (`id TEXT PRIMARY KEY`). A composite
// key is a table constraint, `PRIMARY KEY (a, b)`, and each of its columns
// is NOT NULL: SQLite lets a NULL into a composite key column otherwise.
func columnDefs(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, withFKs bool) ([]string, error) {
	var columns []string
	keys := ent.KeyColumns()
	for _, f := range ent.GetFields() {
		safeCol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		col := fmt.Sprintf("%s %s", ddlIdent(safeCol), columnSQLType(ent, f, all, dialect))
		inKey := slices.Contains(keys, f.Name)
		if inKey && len(keys) == 1 {
			col += " PRIMARY KEY"
		}
		if f.Unique {
			// Past 63 bytes Postgres trims the name it picks, and numbers
			// a second column trimmed to the same one, so the constraint
			// is named here: the name UniqueKeyName gives, which the clear,
			// the boot check and the crud conflict mapping all use.
			uq, err := uniqueIndexName(ent, f.Name)
			if err != nil {
				return nil, err
			}
			if dialect == DialectPostgres && uq != entity.LegacyUniqueKeyName(ent.GetTable(), f.Name) {
				col += " CONSTRAINT " + uq
			}
			col += " UNIQUE"
		}
		c := declaredConstraint(f, keys, dialect)
		if c.notNull {
			col += " NOT NULL"
		}
		col += c.dflt
		columns = append(columns, col)
	}
	if len(keys) > 1 {
		safe := make([]string, len(keys))
		for i, k := range keys {
			q, err := query.SafeIdent(k)
			if err != nil {
				return nil, fmt.Errorf("invalid key column %q: %w", k, err)
			}
			safe[i] = ddlIdent(q)
		}
		columns = append(columns, "PRIMARY KEY ("+strings.Join(safe, ", ")+")")
	}
	if withFKs {
		fks, err := foreignKeyClauses(ent, all, dialect)
		if err != nil {
			return nil, err
		}
		columns = append(columns, fks...)
	}
	return columns, nil
}

var reIndexName = regexp.MustCompile(`(?i)\bINDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+(?:NOT\s+)?EXISTS\s+)?(?:"?[a-zA-Z0-9_]+"?\.)?"?([a-zA-Z0-9_]+)`)

func parseIndexName(ddl string) string {
	m := reIndexName.FindStringSubmatch(ddl)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// entityIndices returns all declared indices on the entity plus auto-created
// BelongsTo foreign key indices matching AutoMigrate.
func entityIndices(ent *entity.Entity) []entity.Index {
	if ent == nil {
		return nil
	}
	var out []entity.Index
	for _, idx := range ent.Config.Indices {
		if len(idx.Columns) == 0 && idx.Expression == "" {
			continue
		}
		out = append(out, idx)
	}
	for _, rel := range ent.Config.Relations {
		fks := rel.ForeignKey
		if rel.Type != entity.RelManyToOne || len(fks) == 0 {
			continue
		}
		if len(fks) == 1 && ent.Config.Scope.OwnerField != "" && strings.EqualFold(fks[0], ent.Config.Scope.OwnerField) {
			continue
		}
		hasIdx := slices.ContainsFunc(ent.Config.Indices, func(idx entity.Index) bool {
			return slices.EqualFunc(idx.Columns, fks, strings.EqualFold)
		})
		if !hasIdx {
			out = append(out, entity.Index{Columns: fks})
		}
	}
	return out
}
