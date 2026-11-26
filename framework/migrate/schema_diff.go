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

// SchemaChange is one DDL statement of a diff, with a summary.
type SchemaChange struct {
	// Summary describes the change, e.g. "posts: add column views INTEGER".
	Summary string
	// SQL is the statement to run.
	SQL string
	// Down is the inverse statement, or empty when no safe inverse is
	// known; a re-added column gets its old type, not its rows.
	Down string
	// Destructive marks a change that can lose data (a drop or a retype),
	// which ApplySchemaDiff refuses without AllowDestructive.
	Destructive bool

	sqliteRebuildOf string
	rebuiltTable    string
	deferredFKs     []SchemaChange
	pgRetypes       []string
	irreversible    bool
}

// DestructiveChangeError is returned by ApplySchemaDiff when the change set
// holds destructive changes the caller did not allow.
type DestructiveChangeError struct {
	// Summaries are the blocked changes' summaries.
	Summaries []string
}

func (e *DestructiveChangeError) Error() string {
	return fmt.Sprintf("refusing %d destructive change(s) without explicit opt-in: %s",
		len(e.Summaries), strings.Join(e.Summaries, "; "))
}

// DiffSchema returns the changes that bring db in line with the registry,
// a missing table as its CREATE TABLE. The dialect is detected from db.
func DiffSchema(ctx context.Context, db *sql.DB, registry entity.Registry) ([]SchemaChange, error) {
	dialect, err := DetectDialectStrictContext(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	all := UnionEntities(registry)

	ordered, err := topoSortEntities(all)
	if err != nil {
		return nil, err
	}
	if err := checkIndexNames(ordered, all); err != nil {
		return nil, err
	}
	if err := checkGeneratedPivotDeclarations(ordered, all, dialect); err != nil {
		return nil, err
	}

	var out []SchemaChange
	tables := make([]string, 0, len(ordered))
	for _, ent := range ordered {
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 {
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
	if dialect == DialectSQLite {
		if env.indexed, err = readSQLiteIndexedColumnsQ(ctx, db, existing); err != nil {
			return nil, err
		}
	}
	for _, ent := range ordered {
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 || len(liveByTable[ent.GetTable()]) == 0 {
			continue
		}
		stale, err := readStaleUniques(ctx, db, ent, dialect)
		if err != nil {
			return nil, err
		}
		if len(stale) > 0 {
			if env.staleUniques == nil {
				env.staleUniques = map[string][]string{}
			}
			env.staleUniques[ent.GetTable()] = stale
		}
	}
	for _, ent := range ordered {
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 {
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
	for ent, relation := range generatedPivots(ordered, all) {
		if len(lookupTableCols(liveByTable, relation.Through)) == 0 {
			continue
		}
		spec, err := buildPivotSpec(ent, relation, all, dialect)
		if err != nil {
			return nil, err
		}
		for index, foreignKey := range spec.fks {
			if liveCols, rewritten := env.keyChanged[strings.ToLower(foreignKey.refTable)]; rewritten {
				spec.fks[index].refCols = liveCols
			}
		}
		if err := checkLivePivotConstraints(ctx, db, spec, all, dialect); err != nil {
			return nil, err
		}
	}

	pivotChanges, err := diffPivotTables(ordered, all, dialect, liveByTable)
	if err != nil {
		return nil, err
	}
	out = append(out, pivotChanges...)
	return out, nil
}

func diffPivotTables(ordered []*entity.Entity, all map[string]*entity.Entity, dialect Dialect, existingTables map[string]map[string]string) ([]SchemaChange, error) {
	var out []SchemaChange
	for ent, rel := range generatedPivots(ordered, all) {
		spec, err := buildPivotSpec(ent, rel, all, dialect)
		if err != nil {
			return nil, err
		}
		if cols := lookupTableCols(existingTables, rel.Through); len(cols) > 0 {
			if err := checkPivotColumns(spec, cols, dialect); err != nil {
				return nil, err
			}
			continue
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

func checkPivotColumns(spec pivotSpec, live map[string]string, dialect Dialect) error {
	for _, column := range slices.Sorted(maps.Keys(spec.Cols)) {
		actual := lookupStringCase(live, column)
		if actual == "" || !typesEquivalent(spec.Cols[column], actual, dialect) {
			return fmt.Errorf("pivot table %q column %q: declared %s, stored %q; migrate the pivot explicitly before changing its relation", spec.Safe, column, spec.Cols[column], actual)
		}
	}
	return nil
}

// ApplyOptions tunes ApplySchemaDiffWithOptions.
type ApplyOptions struct {
	// AllowDestructive permits destructive changes; without it a change set
	// holding one fails with *DestructiveChangeError before any DDL runs.
	AllowDestructive bool
}

// ApplySchemaDiff applies changes in order in one transaction and returns
// how many applied; an error rolls everything back. Destructive changes are
// refused.
func ApplySchemaDiff(ctx context.Context, db *sql.DB, changes []SchemaChange) (int, error) {
	return ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{})
}

// ApplySchemaDiffWithOptions is ApplySchemaDiff with options. A set that
// rebuilds a SQLite table runs with foreign keys off and is rolled back if
// it leaves a new foreign key violation.
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

func execChanges(ctx context.Context, tx *sql.Tx, changes []SchemaChange) (int, error) {
	for i, c := range changes {
		if _, err := tx.ExecContext(ctx, c.SQL); err != nil {
			return i, fmt.Errorf("apply %q: %w", c.Summary, err)
		}
	}
	return len(changes), nil
}

func applySQLiteRebuild(ctx context.Context, db *sql.DB, changes []SchemaChange) (int, error) {
	applied := 0
	err := withSQLiteRebuildConn(ctx, db, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
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

	liveLower := make(map[string]string, len(live))
	for k, v := range live {
		liveLower[strings.ToLower(k)] = v
	}

	declared := make(map[string]schema.Field, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		declared[strings.ToLower(f.Name)] = f
	}

	var changes []SchemaChange

	renameTargets := map[string]bool{}
	renameSources := map[string]bool{}
	renameOldByNew := map[string]string{}
	renameNewByOld := map[string]string{}
	for _, oldName := range slices.Sorted(maps.Keys(ent.Config.Renames)) {
		newName := ent.Config.Renames[oldName]
		oldLow := strings.ToLower(oldName)
		newLow := strings.ToLower(newName)
		if _, oldLive := liveLower[oldLow]; !oldLive {
			continue
		}
		if _, newDeclared := declared[newLow]; !newDeclared {
			continue
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

	added := map[string]string{}
	deferredRequired := map[string]bool{}
	for _, f := range ent.GetFields() {
		if _, ok := liveLower[strings.ToLower(f.Name)]; ok {
			continue
		}
		if renameTargets[strings.ToLower(f.Name)] {
			continue
		}
		qcol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		colType := columnSQLType(ent, f, all, dialect)
		defaultClause := ColumnDefaultClause(f, dialect)
		summary := fmt.Sprintf("%s: add column %s %s", ent.GetName(), f.Name, colType)
		nullable := ""
		if f.Required && f.AutoGenerate == schema.AutoNone {
			if defaultClause != "" {
				nullable = " NOT NULL"
			} else {
				summary += " (NOT NULL deferred: no default for existing rows)"
				deferredRequired[f.Name] = true
				if env != nil {
					if env.deferredNotNull == nil {
						env.deferredNotNull = map[string][]string{}
					}
					env.deferredNotNull[ent.GetTable()] = append(env.deferredNotNull[ent.GetTable()], f.Name)
				}
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

	var sqliteRetypes []retypeCol
	defaultOwned := map[string]bool{}
	var previousConstraints map[string]colConstraint
	if env != nil {
		previousConstraints, _ = recordedConstraints(lookupStringCase(env.tableDDL, ent.GetTable()))
	}
	for _, f := range ent.GetFields() {
		if f.RawType != "" {
			continue
		}
		nameLow := strings.ToLower(f.Name)
		liveType, ok := liveLower[nameLow]
		if !ok {
			if old, renamed := renameOldByNew[nameLow]; renamed {
				liveType, ok = liveLower[old]
			}
		}
		if !ok {
			continue
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
			defaultOwned[nameLow] = true
		}
		source := nameLow
		if old, renamed := renameOldByNew[nameLow]; renamed {
			source = old
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: change column %s type %s → %s (destructive: data conversion may be required)", ent.GetName(), f.Name, liveType, declaredType),
			SQL:         alterColumnTypeSQL(qtable, qcol, liveType, declaredType, ColumnDefaultClause(f, dialect)),
			Down:        alterColumnTypeSQL(qtable, qcol, declaredType, liveType, previousConstraints[source].dflt),
			Destructive: true,
			pgRetypes:   []string{ent.GetTable(), ent.GetName()},
		})
	}

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
	rebuildDropNotes := ""
	if keyChanged && dialect == DialectPostgres {
		var tables map[string]map[string]string
		var extraKeep []string
		if env != nil {
			tables = env.tables
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

	var sqliteHeld map[string]bool
	if dialect == DialectSQLite {
		sqliteHeld = sqliteHeldColumns(ent, env)
	}
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
			continue
		}
		qcol, err := query.SafeIdent(name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", name, err)
		}
		if dialect == DialectSQLite && ((keyChanged && keyHas(lk.cols, name)) || sqliteHeld[strings.ToLower(name)]) {
			rebuildDrops = append(rebuildDrops, name)
			rebuildDropNotes += sqliteRebuildDropNote(qtable, qcol)
			continue
		}
		ddl := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", ddlIdent(qtable), ddlIdent(qcol))
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

	fkDiffer := false
	if env != nil && env.fks != nil {
		fkChanges, differ, err := foreignKeyChanges(ent, all, dialect, qtable, env, renameNewByOld)
		if err != nil {
			return nil, err
		}
		changes = append(changes, fkChanges...)
		fkDiffer = differ && dialect == DialectSQLite
	}

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
	if env != nil {
		if stale, ok := lookupKeyCase(env.staleUniques, ent.GetTable()); ok {
			for _, col := range stale {
				if slices.ContainsFunc(uniqueCleared, func(c string) bool { return strings.EqualFold(c, col) }) {
					continue
				}
				uniqueCleared = append(uniqueCleared, col)
				if dialect == DialectSQLite {
					continue
				}
				qcol, err := query.SafeIdent(col)
				if err != nil {
					return nil, fmt.Errorf("invalid column name %q: %w", col, err)
				}
				name, err := uniqueIndexName(ent, col)
				if err != nil {
					return nil, err
				}
				changes = append(changes, SchemaChange{
					Summary: fmt.Sprintf("%s: drop unique on %s alone", ent.GetName(), col),
					SQL:     dropSingleColumnUniquesSQL(qtable, col),
					Down:    createIndexSQL(dialect, true, name, qtable, ddlIdent(qcol)),
				})
			}
		}
	}
	uniqueRebuild := len(uniqueCleared) > 0 && dialect == DialectSQLite

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

	if len(sqliteRetypes) > 0 || (keyChanged && dialect == DialectSQLite) || len(rebuildDrops) > 0 || fkDiffer || uniqueRebuild || constraintRebuild {
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
			summary = strings.TrimPrefix(summary+"; drop column "+name, "; ")
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
		rebuildLive := maps.Clone(live)
		maps.Copy(rebuildLive, added)
		rebuildEntity := *ent
		rebuildEntity.Config.Fields = slices.Clone(ent.Config.Fields)
		for index := range rebuildEntity.Config.Fields {
			field := &rebuildEntity.Config.Fields[index]
			if deferredRequired[field.Name] {
				field.Required = false
			}
		}
		rebuild, err := sqliteRebuildChange(&rebuildEntity, all, dialect, rebuildLive, renameOldByNew, summary, false)
		if err != nil {
			return nil, err
		}
		rebuild.SQL = rebuildDropNotes + rebuild.SQL
		changes = append(changes, rebuild)
	}

	return changes, nil
}

func isFrameworkManagedColumn(name string, ent *entity.Entity) bool {
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
			t = "integer"
		case "bigserial":
			t = "bigint"
		case "smallserial":
			t = "smallint"
		}
	}
	return t
}

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

func typeWidth(t string) (string, bool) {
	open := strings.IndexByte(t, '(')
	end := strings.LastIndexByte(t, ')')
	if open < 0 || end < open {
		return "", false
	}
	return strings.ReplaceAll(t[open+1:end], " ", ""), true
}

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

func isTextType(t string) bool {
	switch canonicalType(t, DialectPostgres) {
	case "text", "varchar", "char", "character":
		return true
	}
	return false
}

func isIntegerType(t string) bool {
	switch canonicalType(t, DialectPostgres) {
	case "smallint", "integer", "int", "int2", "int4", "int8", "bigint":
		return true
	}
	return false
}

type retypeCol struct {
	name         string
	liveType     string
	declaredType string
}

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
			continue
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
			continue
		}
		managed = append(managed, name)
		_ = typ
	}
	sort.Strings(managed)
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

func sqliteTableTriggers(ctx context.Context, q queryer, table string) ([]string, error) {
	schema, base := splitTable(table)
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
		out = append(out, inSchema(ddl, schema))
	}
	return out, rows.Err()
}

func sqliteRebuildTable(qtable string) string { return qtable + "__rebuild" }

func sqliteRebuildRename(qtable string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", sqliteRebuildTable(qtable), ddlIdent(tableBase(qtable)))
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ReadLiveColumns returns a live table's column name → type, empty when
// the table does not exist. On Postgres the type is data_type with a
// character type's width ("character varying(255)"); on SQLite it is the
// declared type. The table name matches without case.
func ReadLiveColumns(ctx context.Context, db *sql.DB, table string, dialect Dialect) (map[string]string, error) {
	return readLiveColumnsQ(ctx, db, table, dialect)
}

func readLiveColumnsQ(ctx context.Context, q queryer, table string, dialect Dialect) (map[string]string, error) {
	if dialect == DialectPostgres {
		return readLiveColumnsPostgresQ(ctx, q, table, false)
	}
	return readLiveColumnsSQLiteQ(ctx, q, table)
}

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

// ReadLiveColumnsSQLite is ReadLiveColumns on SQLite.
func ReadLiveColumnsSQLite(ctx context.Context, db *sql.DB, table string) (map[string]string, error) {
	return readLiveColumnsSQLiteQ(ctx, db, table)
}

func readLiveColumnsSQLiteQ(ctx context.Context, q queryer, table string) (map[string]string, error) {
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

func columnDefs(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, withFKs bool) ([]string, error) {
	var columns []string
	keys := ent.KeyColumns()
	for _, f := range ent.GetFields() {
		safeCol, err := query.SafeIdent(f.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid column name %q: %w", f.Name, err)
		}
		colType := columnSQLType(ent, f, all, dialect)
		col := fmt.Sprintf("%s %s", ddlIdent(safeCol), colType)
		inKey := slices.Contains(keys, f.Name)
		if inKey && len(keys) == 1 {
			col += " PRIMARY KEY"
		}
		if f.Unique {
			uq, err := uniqueIndexName(ent, f.Name)
			if err != nil {
				return nil, err
			}
			if dialect == DialectPostgres && uq != entity.LegacyUniqueKeyName(ent.GetTable(), f.Name) {
				col += " CONSTRAINT " + uq
			}
			col += " UNIQUE"
		}
		c := declaredConstraint(f, keys, dialect, colType)
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
