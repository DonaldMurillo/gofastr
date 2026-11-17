package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// SchemaSnapshot is the committed schema state migrations have been
// generated up to.
type SchemaSnapshot struct {
	// Tables maps a table name to its column name → SQL type set.
	Tables map[string]map[string]string `json:"tables"`
	// TableDDL is each table's CREATE TABLE, the Down of a dropped table;
	// an older snapshot without it falls back to the column set.
	TableDDL map[string]string `json:"table_ddl,omitempty"`
	// Indices is each table's CREATE INDEX statements.
	Indices map[string][]string `json:"indices,omitempty"`
	// PrimaryKeys is each entity and generated pivot table's key columns in
	// key order; a table missing from it has an unknown key.
	PrimaryKeys map[string][]string `json:"primary_keys,omitempty"`
	// UniqueColumns is each entity table's field-level Unique columns; a
	// table missing from it is not compared.
	UniqueColumns map[string][]string `json:"unique_columns,omitempty"`
	// Views and Routines are the declared views and routines by name.
	Views    map[string]RoutineDef `json:"views,omitempty"`
	Routines map[string]RoutineDef `json:"routines,omitempty"`
}

// RoutineDef is the snapshot record of a view or routine.
type RoutineDef struct {
	// Up and Down are the recorded bodies.
	Up   string `json:"up"`
	Down string `json:"down"`
	// DependsOn is a view's declared sources.
	DependsOn []string `json:"depends_on,omitempty"`
	// Order is a routine's place in the plan, from 1; 0 is unrecorded.
	Order int `json:"order,omitempty"`
}

// SnapshotFromRegistry is the snapshot the registered entities describe
// for dialect.
func SnapshotFromRegistry(reg entity.Registry, dialect Dialect) SchemaSnapshot {
	return SnapshotFromPlan(Plan{Registry: reg}, dialect)
}

// SnapshotFromPlan is the snapshot a full Plan describes for dialect.
func SnapshotFromPlan(plan Plan, dialect Dialect) SchemaSnapshot {
	snap := SchemaSnapshot{Tables: map[string]map[string]string{}}
	if plan.Registry != nil {
		all := UnionEntities(plan.Registry)
		for _, ent := range all {
			if ent.Config.Unmanaged {
				continue
			}
			cols := map[string]string{}
			for _, f := range ent.GetFields() {
				cols[f.Name] = columnSQLType(ent, f, all, dialect)
			}
			snap.Tables[ent.GetTable()] = cols
			if snap.PrimaryKeys == nil {
				snap.PrimaryKeys = map[string][]string{}
			}
			snap.PrimaryKeys[ent.GetTable()] = ent.KeyColumns()
			if snap.UniqueColumns == nil {
				snap.UniqueColumns = map[string][]string{}
			}
			snap.UniqueColumns[ent.GetTable()] = uniqueColumns(ent)
			if ddl, err := buildCreateTableSQL(ent, all, dialect); err == nil {
				if snap.TableDDL == nil {
					snap.TableDDL = map[string]string{}
				}
				snap.TableDDL[ent.GetTable()] = ddl
			}
			if safeTable, err := query.SafeIdent(ent.GetTable()); err == nil {
				var idxDDLs []string
				for _, idx := range entityIndices(ent) {
					idxDDLs = append(idxDDLs, indexDDL(dialect, safeTable, idx))
				}
				if len(idxDDLs) > 0 {
					if snap.Indices == nil {
						snap.Indices = map[string][]string{}
					}
					snap.Indices[ent.GetTable()] = idxDDLs
				}
			}
		}

		orderedPivots, err := topoSortEntities(all)
		if err != nil {
			names := make([]string, 0, len(all))
			for n := range all {
				names = append(names, n)
			}
			sort.Strings(names)
			orderedPivots = make([]*entity.Entity, 0, len(names))
			for _, n := range names {
				orderedPivots = append(orderedPivots, all[n])
			}
		}
		for ent, rel := range generatedPivots(orderedPivots, all) {
			spec, err := buildPivotSpec(ent, rel, all, dialect)
			if err != nil {
				continue
			}
			snap.Tables[spec.Safe] = spec.Cols
			if snap.PrimaryKeys == nil {
				snap.PrimaryKeys = map[string][]string{}
			}
			snap.PrimaryKeys[spec.Safe] = []string{rel.LocalKey, rel.ForeignKeyTarget}
			if snap.TableDDL == nil {
				snap.TableDDL = map[string]string{}
			}
			snap.TableDDL[spec.Safe] = spec.DDL
			if snap.Indices == nil {
				snap.Indices = map[string][]string{}
			}
			snap.Indices[spec.Safe] = []string{spec.IndexDDL}
		}
	}
	if len(plan.Views) > 0 {
		snap.Views = map[string]RoutineDef{}
		for _, v := range plan.Views {
			up, down := v.render(dialect)
			snap.Views[v.Name] = RoutineDef{Up: up, Down: down, DependsOn: slices.Clone(v.DependsOn)}
		}
	}
	if routines, _ := partitionRoutinesByDialect(plan.Routines, dialect); len(routines) > 0 {
		snap.Routines = map[string]RoutineDef{}
		for i, r := range routines {
			def := RoutineDef{Up: r.Up, Down: r.Down, Order: i + 1}
			if first, ok := snap.Routines[r.Name]; ok {
				def.Order = first.Order
			}
			snap.Routines[r.Name] = def
		}
	}
	return snap
}

func recordDeferredNotNull(all map[string]*entity.Entity, next SchemaSnapshot, dialect Dialect, deferred map[string][]string) error {
	for table, cols := range deferred {
		ent := entityForTable(all, table)
		if ent == nil {
			continue
		}
		relaxed := *ent
		relaxed.Config.Fields = slices.Clone(ent.Config.Fields)
		for i := range relaxed.Config.Fields {
			if slices.Contains(cols, relaxed.Config.Fields[i].Name) {
				relaxed.Config.Fields[i].Required = false
			}
		}
		ddl, err := buildCreateTableSQL(&relaxed, all, dialect)
		if err != nil {
			return err
		}
		if next.TableDDL == nil {
			next.TableDDL = map[string]string{}
		}
		next.TableDDL[ent.GetTable()] = ddl
	}
	return nil
}

// GenerateMigration diffs the registered entities against prev and returns
// the up and down DDL for the delta and the snapshot to save; up is empty
// when nothing changed and down is empty when the delta has no inverse.
// Foreign keys are not compared: the snapshot records no constraint names.
func GenerateMigration(reg entity.Registry, prev SchemaSnapshot, dialect Dialect) (up, down string, next SchemaSnapshot, err error) {
	return GeneratePlan(Plan{Registry: reg}, prev, dialect)
}

// GeneratePlan is GenerateMigration for a full Plan, views and routines
// included; routine bodies are compared verbatim. It errors on a delta it
// cannot write safely, such as a SQLite rebuild that foreign keys onto the
// table would break, or a removed routine with no recorded Down.
func GeneratePlan(plan Plan, prev SchemaSnapshot, dialect Dialect) (up, down string, next SchemaSnapshot, err error) {
	all := map[string]*entity.Entity{}
	var ordered []*entity.Entity
	if plan.Registry != nil {
		all = UnionEntities(plan.Registry)
		ordered, err = topoSortEntities(all)
		if err != nil {
			return "", "", SchemaSnapshot{}, err
		}
		if err := checkIndexNames(ordered, all); err != nil {
			return "", "", SchemaSnapshot{}, err
		}
		if err := checkGeneratedPivotDeclarations(ordered, all, dialect); err != nil {
			return "", "", SchemaSnapshot{}, err
		}
	}
	prev = respellTables(prev, declaredTableNames(all), dialect)

	viewRoutines := make([]Routine, 0, len(plan.Views))
	for _, v := range topoSortViews(plan.Views) {
		viewRoutines = append(viewRoutines, v.routine(dialect))
	}
	routines, _ := partitionRoutinesByDialect(plan.Routines, dialect)
	_, viewDrops, err := routineChanges(viewRoutines, prev.Views, viewRemovalOrder)
	if err != nil {
		return "", "", SchemaSnapshot{}, err
	}
	routineUpserts, routineDrops, err := routineChanges(routines, prev.Routines, routineRemovalOrder)
	if err != nil {
		return "", "", SchemaSnapshot{}, err
	}

	var changes []SchemaChange
	env := &fkDiffEnv{tables: prev.Tables, uniques: snapshotUniques(prev), tableDDL: prev.TableDDL}
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue
		}
		prevCols := lookupTableCols(prev.Tables, ent.GetTable())
		var lk *liveKey
		if cols, ok := lookupKeyCase(prev.PrimaryKeys, ent.GetTable()); ok && len(prevCols) > 0 {
			lk = &liveKey{cols: cols, constraint: defaultPKName(ent.GetTable())}
		}
		entChanges, derr := diffEntity(ent, all, dialect, prevCols, lk, env)
		if derr != nil {
			return "", "", SchemaSnapshot{}, fmt.Errorf("generate %s: %w", ent.GetName(), derr)
		}
		var idxDrops, idxAdds []SchemaChange
		if len(prevCols) > 0 {
			safeTable, err := query.SafeIdent(ent.GetTable())
			if err == nil {
				desiredIdxList := entityIndices(ent)
				desiredIdxDDLs := make([]string, len(desiredIdxList))
				for i, idx := range desiredIdxList {
					desiredIdxDDLs[i] = indexDDL(dialect, safeTable, idx)
				}
				if prev.Indices != nil {
					prevIdxDDLs := lookupIndicesCase(prev.Indices, ent.GetTable())
					for _, c := range diffTableIndices(ent.GetName(), safeTable, desiredIdxDDLs, prevIdxDDLs) {
						if isIndexDrop(c.SQL) {
							idxDrops = append(idxDrops, c)
						} else {
							idxAdds = append(idxAdds, c)
						}
					}
				} else {
					for _, ddl := range desiredIdxDDLs {
						idxName := parseIndexName(ddl)
						safeIdxName, err := query.SafeIdent(idxName)
						if err != nil {
							continue
						}
						idxAdds = append(idxAdds, SchemaChange{
							Summary: fmt.Sprintf("%s: index %s", ent.GetName(), safeIdxName),
							SQL:     ddl,
							Down:    dropIndexSQL(safeTable, safeIdxName),
						})
					}
				}
			}
		}
		changes = append(changes, idxDrops...)
		changes = append(changes, entChanges...)
		changes = append(changes, idxAdds...)
	}

	changes = append(changes, env.deferred...)

	retyped := map[string]bool{}
	for _, c := range changes {
		for _, name := range c.pgRetypes {
			retyped[strings.ToLower(name)] = true
			retyped[tableKey(dialect, name)] = true
		}
	}
	viewRedrops, viewUpserts := viewChanges(topoSortViews(plan.Views), prev.Views, dialect, retyped)
	changes = slices.Concat(routineDrops, viewRedrops, viewDrops, changes)

	if len(ordered) > 0 {
		pivotChanges, err := diffPivotTables(ordered, all, dialect, prev.Tables)
		if err != nil {
			return "", "", SchemaSnapshot{}, err
		}
		changes = append(changes, pivotChanges...)
	}

	declaredTables := map[string]bool{}
	for _, t := range declaredTableNames(all) {
		declaredTables[tableKey(dialect, t)] = true
	}

	if dialect == DialectSQLite {
		rebuilt := map[string]bool{}
		for _, c := range changes {
			if c.sqliteRebuildOf != "" {
				rebuilt[strings.ToLower(all[c.sqliteRebuildOf].GetTable())] = true
			}
		}
		for _, c := range changes {
			if c.sqliteRebuildOf == "" {
				continue
			}
			ent := all[c.sqliteRebuildOf]
			oldKey, _ := lookupKeyCase(prev.PrimaryKeys, ent.GetTable())
			renames := renamesApplied(ent, lookupTableCols(prev.Tables, ent.GetTable()))
			refs := recordedReferencesOnto(dialect, prev.TableDDL, ent.GetTable())
			var stranded []string
			for _, t := range slices.Sorted(maps.Keys(refs)) {
				if rebuilt[strings.ToLower(t)] || !declaredTables[tableKey(dialect, t)] {
					continue
				}
				for _, cols := range refs[t] {
					named := make([]string, len(cols))
					for i, col := range cols {
						named[i] = col
						if n, ok := renames[strings.ToLower(col)]; ok {
							named[i] = n
						}
					}
					if strandsReference(ent, oldKey, named) {
						stranded = append(stranded, t)
						break
					}
				}
			}
			if len(stranded) > 0 {
				return "", "", SchemaSnapshot{}, fmt.Errorf("%s: %w", c.sqliteRebuildOf,
					strandedReferrersError(ent.GetTable(), oldKey, ent.KeyColumns(), stranded, "this migration rebuilds no table but the ones whose columns change"))
			}
		}
	}

	for _, c := range changes {
		if c.sqliteRebuildOf == "" {
			continue
		}
		ent := all[c.sqliteRebuildOf]
		refs := mutatingFKReferrers(ent, all)
		for _, t := range snapshotMutatingReferrers(dialect, prev, ent.GetTable()) {
			if declaredTables[tableKey(dialect, t)] && !slices.ContainsFunc(refs, func(r string) bool { return strings.EqualFold(r, t) }) {
				refs = append(refs, t)
			}
		}
		if len(refs) > 0 {
			sort.Strings(refs)
			return "", "", SchemaSnapshot{}, fmt.Errorf("%s: cannot generate this SQLite table rebuild: it drops the table, "+
				"and the ON DELETE CASCADE / SET NULL foreign keys in %s would delete or unlink their rows; "+
				"write this migration by hand with -- +migrate NoTransaction and PRAGMA foreign_keys=OFF around the rebuild, "+
				"or apply it with ApplySchemaDiffWithOptions, which turns foreign keys off for the rebuild",
				c.sqliteRebuildOf, strings.Join(refs, ", "))
		}
	}
	for _, c := range changes {
		if c.sqliteRebuildOf == "" {
			continue
		}
		ent := all[c.sqliteRebuildOf]
		refs := slices.Sorted(maps.Keys(recordedReferencesOnto(dialect, prev.TableDDL, ent.GetTable())))
		if len(refs) > 0 {
			return "", "", SchemaSnapshot{}, fmt.Errorf("%s: cannot generate this SQLite table rebuild: it drops the table, "+
				"and the foreign keys in %s that reference it fail the DROP while any of their rows do; "+
				"write this migration by hand with -- +migrate NoTransaction and PRAGMA foreign_keys=OFF around the rebuild, "+
				"or apply it with ApplySchemaDiffWithOptions, which turns foreign keys off for the rebuild",
				c.sqliteRebuildOf, strings.Join(refs, ", "))
		}
	}

	dropped := make([]string, 0)
	for table := range prev.Tables {
		if !declaredTables[tableKey(dialect, table)] {
			dropped = append(dropped, table)
		}
	}
	dropped = sortDroppedTables(dialect, dropped, prev.TableDDL)
	for _, table := range dropped {
		var kept []string
		for _, referrer := range slices.Sorted(maps.Keys(recordedReferencesOnto(dialect, prev.TableDDL, table))) {
			if declaredTables[tableKey(dialect, referrer)] {
				kept = append(kept, referrer)
			}
		}
		if len(kept) > 0 {
			return "", "", SchemaSnapshot{}, fmt.Errorf("%s: cannot drop table while kept tables %s reference it; remove their foreign keys in a separate migration first", table, strings.Join(kept, ", "))
		}
		qtable, qerr := query.SafeIdent(table)
		if qerr != nil {
			return "", "", SchemaSnapshot{}, fmt.Errorf("generate: invalid table name %q: %w", table, qerr)
		}
		down := lookupStringCase(prev.TableDDL, table)
		if down == "" {
			down = recreateTableSQL(table, lookupTableCols(prev.Tables, table))
		}
		if prev.Indices != nil {
			if idxs := lookupIndicesCase(prev.Indices, table); len(idxs) > 0 {
				var trimmedIdxs []string
				for _, idx := range idxs {
					trimmedIdxs = append(trimmedIdxs, strings.TrimRight(strings.TrimSpace(idx), ";"))
				}
				down = strings.TrimRight(strings.TrimSpace(down), ";") + ";\n" + strings.Join(trimmedIdxs, ";\n")
			}
		}
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: drop table", table),
			SQL:         fmt.Sprintf("DROP TABLE IF EXISTS %s", ddlIdent(qtable)),
			Down:        down,
			Destructive: true,
		})
	}

	if dialect == DialectSQLite && slices.ContainsFunc(changes, func(c SchemaChange) bool { return c.sqliteRebuildOf != "" }) {
		for i, c := range changes {
			if c.sqliteRebuildOf == "" {
				continue
			}
			qtable, qerr := query.SafeIdent(all[c.sqliteRebuildOf].GetTable())
			if qerr != nil {
				return "", "", SchemaSnapshot{}, fmt.Errorf("generate: invalid table name %q: %w", all[c.sqliteRebuildOf].GetTable(), qerr)
			}
			rename := sqliteRebuildRename(qtable)
			changes[i].SQL = strings.Replace(c.SQL, rename,
				"PRAGMA legacy_alter_table=ON;\n"+rename+";\nPRAGMA legacy_alter_table=OFF", 1)
		}
		routineUpserts, _, _ = routineChanges(routines, nil, routineRemovalOrder)
	}

	changes = append(changes, viewUpserts...)
	changes = append(changes, routineUpserts...)

	seenSQL := make(map[string]int, len(changes))
	dedupedChanges := make([]SchemaChange, 0, len(changes))
	for _, c := range changes {
		norm := strings.TrimRight(strings.TrimSpace(c.SQL), ";")
		if i, ok := seenSQL[norm]; ok && norm != "" {
			dedupedChanges[i].irreversible = dedupedChanges[i].irreversible || c.irreversible
			continue
		}
		if norm != "" {
			seenSQL[norm] = len(dedupedChanges)
		}
		dedupedChanges = append(dedupedChanges, c)
	}
	changes = dedupedChanges

	next = SnapshotFromPlan(plan, dialect)
	if err := recordDeferredNotNull(all, next, dialect, env.deferredNotNull); err != nil {
		return "", "", SchemaSnapshot{}, err
	}
	keepRefitted(prev, next)
	if len(changes) == 0 {
		return "", "", next, nil
	}

	ups := make([]string, 0, len(changes))
	for _, c := range changes {
		ups = append(ups, strings.TrimRight(strings.TrimSpace(c.SQL), ";"))
	}
	downs := make([]string, 0, len(changes))
	if slices.ContainsFunc(changes, func(c SchemaChange) bool { return c.irreversible }) {
		return strings.Join(ups, ";\n") + ";", "", next, nil
	}
	for _, change := range slices.Backward(changes) {
		if d := strings.TrimSpace(change.Down); d != "" {
			downs = append(downs, strings.TrimRight(d, ";"))
		}
	}
	up = strings.Join(ups, ";\n") + ";"
	down = strings.Join(downs, ";\n")
	if down != "" {
		down += ";"
	}
	return up, down, next, nil
}

func viewChanges(ordered []View, prev map[string]RoutineDef, dialect Dialect, retyped map[string]bool) (drops, creates []SchemaChange) {
	dependents := viewDependents(ordered)
	redo := map[string]bool{}
	for _, v := range ordered {
		up, _ := v.render(dialect)
		readsRetyped := slices.ContainsFunc(v.DependsOn, func(d string) bool {
			return retyped[strings.ToLower(d)] || retyped[tableKey(dialect, d)]
		})
		if p, ok := prev[v.Name]; ok && (p.Up != up || readsRetyped) {
			redo[v.Name] = true
			for _, d := range dependents[v.Name] {
				redo[d.Name] = true
			}
		}
	}
	for _, v := range slices.Backward(ordered) {
		if p, ok := prev[v.Name]; ok && redo[v.Name] {
			drops = append(drops, SchemaChange{Summary: fmt.Sprintf("view %s: drop to recreate", v.Name), SQL: p.Down, Down: p.Up})
		}
	}
	for _, v := range ordered {
		up, down := v.render(dialect)
		if _, existed := prev[v.Name]; !existed || redo[v.Name] {
			creates = append(creates, SchemaChange{Summary: fmt.Sprintf("view %s: create", v.Name), SQL: up, Down: down})
		}
	}
	return drops, creates
}

func routineChanges(current []Routine, prev map[string]RoutineDef, removalOrder func(removed []string, prev map[string]RoutineDef) []string) (upserts, removals []SchemaChange, err error) {
	seen := map[string]bool{}
	byName := map[string]Routine{}
	for _, r := range current {
		byName[r.Name] = r
	}
	for _, r := range current {
		name := r.Name
		if seen[name] {
			continue
		}
		seen[name] = true
		r = byName[name]
		prevDef, ok := prev[name]
		switch {
		case !ok:
			upserts = append(upserts, SchemaChange{
				Summary: fmt.Sprintf("routine %s: create", name),
				SQL:     r.Up,
				Down:    r.Down,
			})
		case prevDef.Up != r.Up:
			upserts = append(upserts, SchemaChange{
				Summary: fmt.Sprintf("routine %s: replace", name),
				SQL:     r.Up,
				Down:    prevDef.Up,
			})
		case strings.TrimSpace(prevDef.Down) == "" && strings.TrimSpace(r.Down) != "":
			upserts = append(upserts, SchemaChange{
				Summary: fmt.Sprintf("routine %s: record Down", name),
				SQL:     r.Up,
			})
		}
	}

	removed := make([]string, 0)
	for name := range prev {
		if !seen[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	for _, name := range removalOrder(removed, prev) {
		drop := strings.TrimSpace(prev[name].Down)
		if drop == "" {
			return nil, nil, fmt.Errorf("routine %q was removed, but the snapshot records no Down for it, "+
				"so its drop cannot be generated: declare it again with the same Up and a Down "+
				"(e.g. DROP FUNCTION ...), generate to record the Down, then remove it", name)
		}
		removals = append(removals, SchemaChange{
			Summary: fmt.Sprintf("routine %s: drop", name),
			SQL:     drop,
			Down:    prev[name].Up,
		})
	}
	return upserts, removals, nil
}

func viewRemovalOrder(removed []string, prev map[string]RoutineDef) []string {
	out := make([]string, 0, len(removed))
	done := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		if done[name] {
			return
		}
		done[name] = true
		for _, other := range removed {
			if slices.ContainsFunc(prev[other].DependsOn, func(d string) bool { return strings.EqualFold(d, name) }) {
				visit(other)
			}
		}
		out = append(out, name)
	}
	for _, name := range removed {
		visit(name)
	}
	return out
}

func routineRemovalOrder(removed []string, prev map[string]RoutineDef) []string {
	out := slices.Clone(removed)
	slices.Reverse(out)
	if !slices.ContainsFunc(out, func(n string) bool { return prev[n].Order == 0 }) {
		slices.SortStableFunc(out, func(a, b string) int { return prev[b].Order - prev[a].Order })
	}
	return out
}

func recreateTableSQL(table string, cols map[string]string) string {
	names := make([]string, 0, len(cols))
	for n := range cols {
		names = append(names, n)
	}
	sort.Strings(names)
	defs := make([]string, 0, len(names))
	for _, n := range names {
		defs = append(defs, fmt.Sprintf("%s %s", ddlIdent(query.MustIdent(n)), cols[n]))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		ddlIdent(query.MustIdent(table)), strings.Join(defs, ",\n\t"))
}

// ErrDirectiveInSQL is returned when rendered SQL contains a line the
// migration runner would read as a `-- +migrate` directive.
var ErrDirectiveInSQL = errors.New("migrate: rendered SQL contains a line that would parse as a -- +migrate directive")

func containsDirectiveLine(sql string) bool {
	for line := range strings.SplitSeq(sql, "\n") {
		if coremig.IsDirectiveLine(line) {
			return true
		}
	}
	return false
}

// RenderMigrationFile formats a versioned migration in the `-- +migrate`
// layout the runner parses; down may be empty.
//
// Deprecated: use [RenderMigrationFileChecked], which refuses SQL that
// would synthesize a directive.
func RenderMigrationFile(version uint64, name, up, down string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- +migrate Version %d\n", version)
	fmt.Fprintf(&b, "-- +migrate Name %s\n", name)
	b.WriteString("-- +migrate Up\n")
	b.WriteString(up)
	b.WriteString("\n")
	if strings.TrimSpace(down) != "" {
		b.WriteString("-- +migrate Down\n")
		b.WriteString(down)
		b.WriteString("\n")
	}
	return b.String()
}

// RenderMigrationFileChecked is [RenderMigrationFile] that returns
// [ErrDirectiveInSQL] when a line of up, down or name would read as a
// directive to the runner.
func RenderMigrationFileChecked(version uint64, name, up, down string) (string, error) {
	for _, entry := range []struct {
		key string
		val string
	}{{"up", up}, {"down", down}, {"name", name}} {
		label, sql := entry.key, entry.val
		if containsDirectiveLine(sql) {
			return "", fmt.Errorf("%w (in the %s section); a column DEFAULT or other literal spans a line starting with \"-- +migrate\"", ErrDirectiveInSQL, label)
		}
	}
	return RenderMigrationFile(version, name, up, down), nil
}

// LoadSnapshot reads a snapshot JSON file; a missing file is an empty
// snapshot, not an error.
func LoadSnapshot(path string) (SchemaSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return SchemaSnapshot{Tables: map[string]map[string]string{}}, nil
		}
		return SchemaSnapshot{}, err
	}
	var snap SchemaSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return SchemaSnapshot{}, fmt.Errorf("parse snapshot %s: %w", path, err)
	}
	if snap.Tables == nil {
		snap.Tables = map[string]map[string]string{}
	}
	return snap, nil
}

// SaveSnapshot writes a snapshot as indented JSON, replacing the file
// atomically, at a symlink's target, keeping an existing file's mode (new
// files are 0644).
func SaveSnapshot(path string, snap SchemaSnapshot) error {
	data, _ := json.MarshalIndent(snap, "", "  ")
	data = append(data, '\n')
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	committed = true
	return nil
}

var reFKReferences = regexp.MustCompile(`(?i)\bREFERENCES\s+("?[a-zA-Z0-9_]+"?(?:\."?[a-zA-Z0-9_]+"?)*)`)

var reMutatingFK = regexp.MustCompile(`(?i)\bREFERENCES\s+("?[a-zA-Z0-9_]+"?(?:\."?[a-zA-Z0-9_]+"?)*)\s*\([^)]*\)\s*ON\s+DELETE\s+(?:CASCADE|SET\s+NULL)\b`)

func ddlWithoutLiterals(ddl string) string {
	var b strings.Builder
	for i := 0; i < len(ddl); {
		j := skipInert(ddl, i)
		switch {
		case j == i:
			b.WriteByte(ddl[i])
			i++
		case ddl[i] == '\'' || ddl[i] == '-' || ddl[i] == '/':
			b.WriteByte(' ')
			i = j
		default:
			b.WriteString(ddl[i:j])
			i = j
		}
	}
	return b.String()
}

func snapshotMutatingReferrers(dialect Dialect, prev SchemaSnapshot, table string) []string {
	var out []string
	for _, name := range slices.Sorted(maps.Keys(prev.TableDDL)) {
		for _, m := range reMutatingFK.FindAllStringSubmatch(ddlWithoutLiterals(prev.TableDDL[name]), -1) {
			if refersTo(dialect, name, unquoteDDLIdent(m[1]), table) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func sortDroppedTables(dialect Dialect, tables []string, tableDDL map[string]string) []string {
	if len(tables) <= 1 {
		return tables
	}

	dep := make(map[string][]string, len(tables))
	inDegree := make(map[string]int, len(tables))
	canon := make(map[string]string, len(tables))
	for _, t := range tables {
		low := asciicase.Lower(t)
		canon[low] = t
		inDegree[low] = 0
	}

	for _, t := range tables {
		low := asciicase.Lower(t)
		ddl := lookupStringCase(tableDDL, t)
		if ddl == "" {
			continue
		}
		matches := reFKReferences.FindAllStringSubmatch(ddlWithoutLiterals(ddl), -1)
		seenRef := make(map[string]bool)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			for _, u := range tables {
				target := asciicase.Lower(u)
				if target != low && !seenRef[target] && refersTo(dialect, t, unquoteDDLIdent(m[1]), u) {
					seenRef[target] = true
					dep[low] = append(dep[low], target)
					inDegree[target]++
				}
			}
		}
	}

	var queue []string
	for low, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, low)
		}
	}
	sort.Strings(queue)

	result := make([]string, 0, len(tables))
	visited := make(map[string]bool, len(tables))

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		result = append(result, canon[curr])
		visited[curr] = true

		for _, next := range dep[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
				sort.Strings(queue)
			}
		}
	}

	if len(result) < len(tables) {
		var remaining []string
		for _, t := range tables {
			if !visited[asciicase.Lower(t)] {
				remaining = append(remaining, t)
			}
		}
		sort.Strings(remaining)
		result = append(result, remaining...)
	}

	return result
}

func lookupTableCols(tables map[string]map[string]string, name string) map[string]string {
	if tables == nil {
		return nil
	}
	if cols, ok := tables[name]; ok {
		return cols
	}
	for k, cols := range tables {
		if strings.EqualFold(k, name) {
			return cols
		}
	}
	return nil
}

func lookupStringCase(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

func lookupKeyCase(m map[string][]string, key string) ([]string, bool) {
	if cols, ok := m[key]; ok {
		return cols, true
	}
	for k, cols := range m {
		if strings.EqualFold(k, key) {
			return cols, true
		}
	}
	return nil, false
}

func lookupIndicesCase(m map[string][]string, key string) []string {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func isIndexDrop(sql string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "DROP INDEX")
}

// refitted reports whether index name, past 63 bytes, is today's
// query.FitIdent name of a desired index with the same definition.
func refitted(name, prevDDL string, desired map[string]string) bool {
	if len(name) <= query.MaxIdentBytes {
		return false
	}
	fitted := query.FitIdent(name)
	ddl, ok := desired[strings.ToLower(fitted)]
	if !ok {
		return false
	}
	norm := func(s string) string { return strings.TrimRight(strings.TrimSpace(s), ";") }
	return norm(strings.Replace(prevDDL, name, fitted, 1)) == norm(ddl)
}

func keepRefitted(prev, next SchemaSnapshot) {
	for table, ddls := range next.Indices {
		desired := make(map[string]string, len(ddls))
		for _, ddl := range ddls {
			if name := parseIndexName(ddl); name != "" {
				desired[strings.ToLower(name)] = ddl
			}
		}
		for _, ddl := range lookupIndicesCase(prev.Indices, table) {
			name := parseIndexName(ddl)
			if _, ok := desired[strings.ToLower(name)]; ok || name == "" || !refitted(name, ddl, desired) {
				continue
			}
			next.Indices[table] = append(next.Indices[table], ddl)
		}
	}
}

func diffTableIndices(entName, safeTable string, desiredDDLs, prevDDLs []string) []SchemaChange {
	var changes []SchemaChange
	prevMap := make(map[string]string)
	for _, ddl := range prevDDLs {
		if name := parseIndexName(ddl); name != "" {
			prevMap[strings.ToLower(name)] = ddl
		}
	}
	desiredMap := make(map[string]string)
	for _, ddl := range desiredDDLs {
		if name := parseIndexName(ddl); name != "" {
			desiredMap[strings.ToLower(name)] = ddl
		}
	}

	for _, ddl := range desiredDDLs {
		name := parseIndexName(ddl)
		if name == "" {
			continue
		}
		safeName, err := query.SafeIdent(name)
		if err != nil {
			continue
		}
		prevDDL, ok := prevMap[strings.ToLower(name)]
		if !ok {
			changes = append(changes, SchemaChange{
				Summary: fmt.Sprintf("%s: index %s", entName, safeName),
				SQL:     ddl,
				Down:    dropIndexSQL(safeTable, safeName),
			})
		} else if unquoteDDLIdent(strings.TrimRight(strings.TrimSpace(prevDDL), ";")) != unquoteDDLIdent(strings.TrimRight(strings.TrimSpace(ddl), ";")) {
			dropSQL := dropIndexSQL(safeTable, safeName)
			changes = append(changes,
				SchemaChange{
					Summary: fmt.Sprintf("%s: drop modified index %s", entName, safeName),
					SQL:     dropSQL,
					Down:    prevDDL,
				},
				SchemaChange{
					Summary: fmt.Sprintf("%s: index %s", entName, safeName),
					SQL:     ddl,
					Down:    dropSQL,
				},
			)
		}
	}

	for _, ddl := range prevDDLs {
		name := parseIndexName(ddl)
		if name == "" {
			continue
		}
		safeName, err := query.SafeIdent(name)
		if err != nil {
			continue
		}
		if _, ok := desiredMap[strings.ToLower(name)]; !ok {
			if refitted(name, ddl, desiredMap) {
				continue
			}
			changes = append(changes, SchemaChange{
				Summary:     fmt.Sprintf("%s: remove index %s", entName, safeName),
				SQL:         dropIndexSQL(safeTable, safeName),
				Down:        ddl,
				Destructive: true,
			})
		}
	}
	return changes
}
