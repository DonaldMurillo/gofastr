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

// Incremental migration generation
//
// The declarative workflow: an entity declaration is the desired schema. To
// turn a change to that declaration into a reviewable, reversible, versioned
// migration WITHOUT touching a live database, GoFastr keeps a committed
// snapshot of the last-generated schema. GenerateMigration diffs the current
// entity declarations against the snapshot and emits the Up/Down DDL plus the
// new snapshot. The generated SQL reuses the same builders AutoMigrate uses, so
// what you generate is exactly what auto-migrate would have applied.

// SchemaSnapshot is the serialized schema state migrations have been generated
// up to. Tables maps a table name to its column-name→SQL-type set; the same
// shape diffEntityFromLive consumes, so the snapshot diff and the live-DB diff
// share one code path.
type SchemaSnapshot struct {
	Tables map[string]map[string]string `json:"tables"`
	// TableDDL holds the full CREATE TABLE statement per table, so a dropped
	// table's Down recreates it WITH all constraints (PK, NOT NULL, UNIQUE,
	// FK, defaults) rather than a lossy column-list reconstruction. Optional:
	// snapshots written by an older gofastr fall back to recreateTableSQL.
	TableDDL map[string]string   `json:"table_ddl,omitempty"`
	Indices  map[string][]string `json:"indices,omitempty"`
	// PrimaryKeys holds each entity table's key columns in key order, so a
	// later generation can see a key change. A snapshot written before it
	// existed has none, and the first generation after records them.
	PrimaryKeys map[string][]string `json:"primary_keys,omitempty"`
	// UniqueColumns holds each entity table's field-level Unique columns
	// (an empty list for a table with none), so a later generation can
	// see Unique set or cleared on a column that already exists. A table
	// missing from it is unknown, and its unique columns are not compared.
	UniqueColumns map[string][]string   `json:"unique_columns,omitempty"`
	Views         map[string]RoutineDef `json:"views,omitempty"`
	Routines      map[string]RoutineDef `json:"routines,omitempty"`
}

// RoutineDef is the snapshot record for a routine: its Up and Down bodies, so
// a later generation can restore the previous definition on rollback.
type RoutineDef struct {
	Up   string `json:"up"`
	Down string `json:"down"`
}

// SnapshotFromRegistry builds the desired-state snapshot from the registered
// entities for the given dialect, the schema the entities describe right now.
func SnapshotFromRegistry(reg entity.Registry, dialect Dialect) SchemaSnapshot {
	return SnapshotFromPlan(Plan{Registry: reg}, dialect)
}

// SnapshotFromPlan builds the desired-state snapshot from a full Plan (tables
// plus routines).
func SnapshotFromPlan(plan Plan, dialect Dialect) SchemaSnapshot {
	snap := SchemaSnapshot{Tables: map[string]map[string]string{}}
	if plan.Registry != nil {
		all := UnionEntities(plan.Registry)
		for _, ent := range all {
			if ent.Config.Unmanaged {
				continue // views / external tables aren't part of the table snapshot
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
			// Capture the exact CREATE TABLE so a future drop is faithfully
			// reversible. Skip silently if the entity can't render (it would
			// have failed the diff anyway).
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

		// Record ManyToMany pivot tables in snapshot using the exact same topological ordering
		// as diffPivotTables and migratePivotTables so reciprocal declarations pick identical
		// canonical pivot metadata.
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
			// The desired snapshot skips what cannot render; the diff
			// reports the same relation as an error.
			spec, err := buildPivotSpec(ent, rel, all, dialect)
			if err != nil {
				continue
			}
			snap.Tables[spec.Safe] = spec.Cols
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
			snap.Views[v.Name] = RoutineDef{Up: up, Down: down}
		}
	}
	// A routine scoped to another dialect never runs on this one, at boot or
	// in a generated file, so the snapshot does not record it.
	if routines, _ := partitionRoutinesByDialect(plan.Routines, dialect); len(routines) > 0 {
		snap.Routines = map[string]RoutineDef{}
		for _, r := range routines {
			snap.Routines[r.Name] = RoutineDef{Up: r.Up, Down: r.Down}
		}
	}
	return snap
}

// GenerateMigration diffs the registered entities against prev (the last
// snapshot) and returns the forward (up) and inverse (down) DDL for the delta,
// plus the new snapshot to persist. up is empty when there is nothing to do.
//
// Covered changes: create and drop table (an entity removed); add, drop and
// rename column; a column type change (in place on Postgres, a table rebuild
// on SQLite); a primary-key change; declared and belongs_to indexes; a
// field's Unique set or cleared; and a column's NOT NULL or DEFAULT
// changed (ALTER COLUMN on Postgres, a table rebuild on SQLite). Foreign
// keys are not compared: the snapshot
// records no constraint names.
func GenerateMigration(reg entity.Registry, prev SchemaSnapshot, dialect Dialect) (up, down string, next SchemaSnapshot, err error) {
	return GeneratePlan(Plan{Registry: reg}, prev, dialect)
}

// GeneratePlan is GenerateMigration for a full Plan: it diffs both the tables
// (entities + declared relations) and the views/routines against the snapshot, emitting one
// reversible migration that covers everything. Routine bodies are compared
// verbatim; a changed routine's Down restores the previous body, and a removed
// routine is dropped (with its recreation as the Down).
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
	}
	prev = respellTables(prev, declaredTableNames(all), dialect)

	// Views and routines: new and changed ones are created after the tables
	// (they select from them), and removed ones are dropped BEFORE any table
	// change, since both engines refuse to drop a table or column a view
	// still reads. A changed view is dropped before the table changes too,
	// since its old body may read what they drop (viewChanges). Views keep
	// their DependsOn order, and routines the plan's order, which is the
	// order boot applies them in; removals drop routines, then views, the
	// reverse of creation. The Down runs the changes backwards, so it
	// unwinds the same way.
	viewRoutines := make([]Routine, 0, len(plan.Views))
	for _, v := range topoSortViews(plan.Views) {
		viewRoutines = append(viewRoutines, v.routine(dialect))
	}
	// Boot skips a routine scoped to another dialect, so a migration
	// generated for this one must not carry it either.
	routines, _ := partitionRoutinesByDialect(plan.Routines, dialect)
	_, viewDrops, err := routineChanges(viewRoutines, prev.Views)
	if err != nil {
		return "", "", SchemaSnapshot{}, err
	}
	viewRedrops, viewUpserts := viewChanges(topoSortViews(plan.Views), prev.Views, dialect)
	routineUpserts, routineDrops, err := routineChanges(routines, prev.Routines)
	if err != nil {
		return "", "", SchemaSnapshot{}, err
	}

	changes := append(routineDrops, viewDrops...)
	changes = append(changes, viewRedrops...)
	// The snapshot's column sets let a Postgres key change detach the
	// foreign keys onto its table; it records no constraint names, so
	// foreign keys themselves are not compared (fks stays nil). The keys a
	// key change re-adds only after their child's own changes run after
	// every table's.
	env := &fkDiffEnv{tables: prev.Tables, uniques: snapshotUniques(prev), tableDDL: prev.TableDDL}
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue // views / external tables generate no table DDL
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
		// The table's index drops run BEFORE its column changes and its
		// index creates after them. Appended after the column changes, a
		// drop came too late twice over: SQLite refuses to drop a column
		// an index still covers, and a unique index retargeted onto a
		// column added in this change was created by the add, dropped as
		// the old definition, and its re-create deduplicated away. The
		// Down runs backwards, so a dropped index is restored after the
		// column it covers.
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
					// Legacy snapshot without Indices map: prior indices unknown.
					// Emit idempotent CREATE INDEX IF NOT EXISTS for all desired indices
					// so existing tables acquire new/missing indices without silent skips.
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

	if len(ordered) > 0 {
		pivotChanges, err := diffPivotTables(ordered, all, dialect, prev.Tables)
		if err != nil {
			return "", "", SchemaSnapshot{}, err
		}
		changes = append(changes, pivotChanges...)
	}

	// Keyed by tableKey, so no spelling of a declared table is dropped.
	declaredTables := map[string]bool{}
	for _, t := range declaredTableNames(all) {
		declaredTables[tableKey(dialect, t)] = true
	}

	// SQLite names a foreign key's referenced columns, so a rebuild that
	// moves the key (or drops the unique column a key references) strands
	// the tables the snapshot records referencing it, unless this file
	// rebuilds or drops them too: every later write to one would fail with
	// "foreign key mismatch". Refuse, as boot does.
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

	// A SQLite rebuild drops its table, and the migration runner applies
	// this file inside a transaction, where PRAGMA foreign_keys is a no-op:
	// every ON DELETE action onto the table would fire against live rows.
	// Refuse rather than write a file that empties a pivot. The constraints
	// in force when the file runs are the snapshot's, not the declarations':
	// a relation removed in this same change still holds its CASCADE key on
	// the child table (nothing here drops a foreign key), so the tables the
	// snapshot records with one count too, unless this file drops them.
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
	// A plain foreign key (no ON DELETE action, or RESTRICT) onto the table
	// fails the DROP instead: SQLite's implicit DELETE FROM finds the child
	// rows still referencing it. The file applied on a database whose child
	// tables were empty and failed with "FOREIGN KEY constraint failed" on
	// every one where a child held a row, the database it was written for.
	// A child this file drops still exists when the rebuild runs, since the
	// drops come last, so it counts too.
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

	// Tables in the snapshot that no entity declares anymore → DROP TABLE.
	dropped := make([]string, 0)
	for table := range prev.Tables {
		if !declaredTables[tableKey(dialect, table)] {
			dropped = append(dropped, table)
		}
	}
	dropped = sortDroppedTables(dialect, dropped, prev.TableDDL)
	for _, table := range dropped {
		qtable, qerr := query.SafeIdent(table)
		if qerr != nil {
			return "", "", SchemaSnapshot{}, fmt.Errorf("generate: invalid table name %q: %w", table, qerr)
		}
		// Prefer the faithful CREATE TABLE captured in the snapshot (all
		// constraints); fall back to a column-list reconstruction for snapshots
		// written before TableDDL existed.
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

	// A SQLite rebuild drops the table and renames its replacement into
	// place. Boot runs it on a connection with legacy_alter_table on,
	// because a modern RENAME re-resolves every view and trigger that names
	// the table, finds it dropped, and fails the migration. The generated
	// file sets the pragma around the rename alone. The DROP also takes the
	// table's own triggers, which boot puts back by running every
	// routine's Up, so the file runs them all too, not only the changed
	// ones. A rebuild is irreversible, so none of this needs a Down.
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
		// No previous definitions, so no removals and no error.
		routineUpserts, _, _ = routineChanges(routines, nil)
	}

	changes = append(changes, viewUpserts...)
	changes = append(changes, routineUpserts...)

	// Deduplicate identical SchemaChange statements (e.g. index DDL emitted
	// both by diffEntityFromLive column additions and diffTableIndices). A
	// dropped duplicate's irreversibility carries over to the one kept: two
	// entities on one table can render the same key change, and only one of
	// them may drop the column that makes it irreversible.
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
	keepRefitted(prev, next)
	if len(changes) == 0 {
		return "", "", next, nil
	}

	ups := make([]string, 0, len(changes))
	for _, c := range changes {
		ups = append(ups, strings.TrimRight(strings.TrimSpace(c.SQL), ";"))
	}
	// Down is the inverse in REVERSE order so dependencies unwind correctly
	// (e.g. drop the FK-holding table before the one it references).
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

// viewChanges diffs the declared views, in dependency order, against the
// snapshot. A new view is created after the table changes. A changed one
// is dropped BEFORE them and created again after: its old body may read a
// column the same migration drops, and both engines refuse that while the
// view exists (Postgres also refuses a CREATE OR REPLACE that drops or
// renames a view column). Every declared view that reads a changed one
// goes with it, dropped deepest first, since Postgres refuses to drop a
// view another still reads. A drop runs the snapshot's own Down, the old
// kind's statement, so a view changing kind drops cleanly. Each drop's
// Down recreates the old view and each creation's Down drops the new one,
// so the rollback runs the same steps backwards. Every snapshot has
// recorded a view's Down since views were added, so the drop always has
// one.
func viewChanges(ordered []View, prev map[string]RoutineDef, dialect Dialect) (drops, creates []SchemaChange) {
	dependents := viewDependents(ordered)
	redo := map[string]bool{}
	for _, v := range ordered {
		up, _ := v.render(dialect)
		if p, ok := prev[v.Name]; ok && p.Up != up {
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

// routineChanges diffs current routines against the snapshot. New or changed
// routines emit their Up in the order given: a view after the views it
// depends on, and routines in the plan's order, the order boot runs them
// in. A changed routine's Down restores the previous body, a new routine's
// Down is its own Down. Removed routines come back separately, name-sorted,
// so the caller can drop them before the tables they read: each is dropped
// via the snapshot's stored Down, with its recreation as the inverse.
func routineChanges(current []Routine, prev map[string]RoutineDef) (upserts, removals []SchemaChange, err error) {
	seen := map[string]bool{}
	byName := map[string]Routine{}
	for _, r := range current {
		byName[r.Name] = r // a repeated name keeps its last definition
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
				Down:    prevDef.Up, // restore the previous definition
			})
		case strings.TrimSpace(prevDef.Down) == "" && strings.TrimSpace(r.Down) != "":
			// The snapshot recorded no Down, so a later removal could not
			// be generated. Re-running the idempotent Up records the Down
			// it now declares; rolling this back changes nothing.
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
	for _, name := range removed {
		// A Routine's Down is optional (see routine.go), so the snapshot
		// may hold no drop for a removed routine. Writing the removal
		// anyway made a migration of nothing but ";" while the snapshot
		// advanced to record the routine as gone, and the routine stayed
		// in the database. Refuse, and say how to record the drop.
		drop := strings.TrimSpace(prev[name].Down)
		if drop == "" {
			return nil, nil, fmt.Errorf("routine %q was removed, but the snapshot records no Down for it, "+
				"so its drop cannot be generated: declare it again with the same Up and a Down "+
				"(e.g. DROP FUNCTION ...), generate to record the Down, then remove it", name)
		}
		removals = append(removals, SchemaChange{
			Summary: fmt.Sprintf("routine %s: drop", name),
			SQL:     drop,          // the drop
			Down:    prev[name].Up, // recreate on rollback
		})
	}
	return upserts, removals, nil
}

// recreateTableSQL reconstructs a CREATE TABLE from a snapshot's column set,
// the FALLBACK Down for a dropped table when the snapshot predates TableDDL.
// Constraints (PK/NOT NULL/UNIQUE/FK) are not recoverable from a column→type
// map, so this is column-and-type only; the TableDDL path is faithful.
// Identifiers are validated (via MustIdent) and bare unless they are keywords
// (ddlIdent), the same convention as buildCreateTableSQL, so a mixed-case name
// folds to lowercase on Postgres consistently with the original CREATE rather
// than being case-preserved by quoting. Column order is sorted for deterministic output.
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

// containsDirectiveLine reports whether any line of sql would be read as
// a directive by the runner, which scans line by line for the
// "-- +migrate" prefix with no idea whether it is inside a string
// literal. It asks the parser's own predicate (coremig.IsDirectiveLine),
// which also covers the near misses the parser refuses ("--+migrate
// Down", "-- +Migrate Down"): matching the exact prefix alone wrote files
// the runner then refused to load.
//
// A column DEFAULT is author-supplied and may be multi-line, so a value
// like "x\n-- +migrate Down\nDROP TABLE victims;-- " renders as valid
// SQL inside one quoted literal and ALSO as a section boundary to the
// runner: it truncates Up mid-literal, the committed migration no longer
// parses, and the attacker's statements sit in Down waiting for a
// rollback. Refusing to write the file is the fix that does not require
// teaching the runner to parse SQL string literals.
func containsDirectiveLine(sql string) bool {
	for line := range strings.SplitSeq(sql, "\n") {
		if coremig.IsDirectiveLine(line) {
			return true
		}
	}
	return false
}

// RenderMigrationFile formats a versioned migration in the `-- +migrate`
// directive layout the runner parses. down may be empty.
//
// Deprecated: use [RenderMigrationFileChecked], which refuses SQL that
// would synthesize a directive. This wrapper is kept for callers that
// render SQL they control end to end.
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

// RenderMigrationFileChecked is [RenderMigrationFile] plus the guard: it
// returns [ErrDirectiveInSQL] when a line of up, down, OR name would be
// read as a directive by the runner. Name is rendered verbatim onto its
// own directive line, so a name containing a newline can carry a whole
// synthesized directive block ("x\n-- +migrate Down\nDROP TABLE victims;-- ")
// — same refusal as the SQL bodies, never a silent rewrite, because
// rewriting a migration's Name changes its identity.
func RenderMigrationFileChecked(version uint64, name, up, down string) (string, error) {
	for label, sql := range map[string]string{"up": up, "down": down, "name": name} {
		if containsDirectiveLine(sql) {
			return "", fmt.Errorf("%w (in the %s section); a column DEFAULT or other literal spans a line starting with \"-- +migrate\"", ErrDirectiveInSQL, label)
		}
	}
	return RenderMigrationFile(version, name, up, down), nil
}

// LoadSnapshot reads a snapshot JSON file. A missing file is not an error. It
// returns an empty snapshot so the first generation emits a full create.
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

// SaveSnapshot writes a snapshot JSON file (pretty-printed for clean diffs).
func SaveSnapshot(path string, snap SchemaSnapshot) error {
	// MarshalIndent cannot fail for a snapshot of string maps: no channels,
	// funcs, or cycles, so the only real error here is the file write.
	data, _ := json.MarshalIndent(snap, "", "  ")
	data = append(data, '\n')
	// Write a sibling and rename it over the snapshot. Writing in place
	// truncated the file first, so a crash or a full disk mid-write left
	// torn JSON that every later generate refused at LoadSnapshot, and
	// deleting it regenerated a full CREATE that could not apply. The
	// snapshot is a committed artifact: a new one is 0644, and an existing
	// one keeps its mode.
	// A symlinked snapshot is written at its target: the rename would
	// otherwise replace the link with a regular file.
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

// reFKReferences matches a REFERENCES clause in a recorded CREATE TABLE,
// capturing the table it references, schema included: a reference onto
// app.a captured as app made the child no dependency of its parent.
var reFKReferences = regexp.MustCompile(`(?i)\bREFERENCES\s+("?[a-zA-Z0-9_]+"?(?:\."?[a-zA-Z0-9_]+"?)*)`)

// reMutatingFK matches a foreign key in a recorded CREATE TABLE whose
// ON DELETE action changes rows, capturing the table it references.
var reMutatingFK = regexp.MustCompile(`(?i)\bREFERENCES\s+("?[a-zA-Z0-9_]+"?(?:\."?[a-zA-Z0-9_]+"?)*)\s*\([^)]*\)\s*ON\s+DELETE\s+(?:CASCADE|SET\s+NULL)\b`)

// ddlWithoutLiterals blanks the string literals and comments of a recorded
// CREATE TABLE, so the patterns above read only its definitions: a text
// DEFAULT 'REFERENCES p(id) ON DELETE CASCADE' references nothing. Quoted
// identifiers are kept as written, so a quote inside one opens no literal.
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

// snapshotMutatingReferrers names the tables whose CREATE TABLE in the
// snapshot holds a CASCADE or SET NULL foreign key onto table, sorted. A
// snapshot written before TableDDL existed records none.
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

// sortDroppedTables orders tables for DROP TABLE so that child/referencing tables
// (such as pivot tables or tables with foreign keys) are dropped BEFORE the tables
// they reference. Because formatPlan reverses the changes slice for Down migrations
// (via slices.Backward), this ordering simultaneously guarantees that Down migrations
// recreate referenced/parent tables BEFORE the child tables that reference them.
func sortDroppedTables(dialect Dialect, tables []string, tableDDL map[string]string) []string {
	if len(tables) <= 1 {
		return tables
	}

	// dep[A] = list of tables that A references (A must be dropped before B)
	dep := make(map[string][]string, len(tables))
	inDegree := make(map[string]int, len(tables))
	// Keys fold ASCII only, as Postgres folds an unquoted identifier:
	// strings.ToLower would give look-alike names one key and lose a table.
	canon := make(map[string]string, len(tables)) // lower -> original casing
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
		// In case of cycles or unvisited nodes, append remaining deterministically.
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

// lookupTableCols performs a case-insensitive lookup in a table-to-columns map.
// SQL identifiers are case-folded in PostgreSQL and case-insensitive in SQLite,
// so snapshot matching must not treat case changes as missing tables.
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

// lookupStringCase performs a case-insensitive lookup in a string-to-string map.
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

// lookupIndicesCase performs a case-insensitive lookup in a table-to-indices map.
// lookupKeyCase finds a table's recorded primary key, ignoring case.
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

// isIndexDrop reports whether a change's SQL drops an index.
func isIndexDrop(sql string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "DROP INDEX")
}

// refitted reports whether prev index name, past 63 bytes, is today's
// query.FitIdent name of the same index: desired holds the fitted name
// with the same definition. An older build created such an index under
// the raw name, which Postgres cut to 63 bytes. The diff creates the
// fitted index and keeps the old one rather than dropping it: a foreign
// key a developer added by hand may depend on a unique index, and the
// DROP fails then. The old index is left for the developer to drop.
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

// keepRefitted carries into next each index of prev that the diff kept on
// a name-only refit (refitted): the old index still exists beside the
// fitted one, so the snapshot keeps recording it. A later generation then
// sees it as prev, keeps it while the fitted index is declared, and drops
// it with the fitted one when the index is removed or changed. Recorded
// only as the fitted index, it outlived the removal and went on refusing
// duplicates.
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

// diffTableIndices compares desired index DDLs against previous index DDLs for a table,
// emitting CREATE INDEX for new indices and DROP INDEX for removed indices.
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
			// Index definition changed under the same index name (e.g. Unique flip, or column change under explicit name).
			// Drop the old index and create the new index.
			// Down reverses this: drops the new index and restores the old index DDL.
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
