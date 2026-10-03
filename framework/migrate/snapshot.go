package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
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
	TableDDL map[string]string     `json:"table_ddl,omitempty"`
	Indices  map[string][]string   `json:"indices,omitempty"`
	Views    map[string]RoutineDef `json:"views,omitempty"`
	Routines map[string]RoutineDef `json:"routines,omitempty"`
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
				cols[f.Name] = SQLType(f, dialect)
			}
			snap.Tables[ent.GetTable()] = cols
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
					idxDDLs = append(idxDDLs, indexDDL(safeTable, idx))
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
		seenPivot := make(map[string]bool)
		for _, ent := range orderedPivots {
			for _, rel := range ent.Config.Relations {
				if rel.Type != entity.RelManyToMany || rel.Through == "" {
					continue
				}
				pivotTable := rel.Through
				key := strings.ToLower(pivotTable)
				if seenPivot[key] {
					continue
				}
				hasEntity := false
				if all != nil {
					if all[pivotTable] != nil {
						hasEntity = true
					} else {
						for _, e := range all {
							if strings.EqualFold(e.GetTable(), pivotTable) {
								hasEntity = true
								break
							}
						}
					}
				}
				if hasEntity {
					continue
				}
				seenPivot[key] = true
				target := all[rel.Entity]
				if target == nil {
					continue
				}
				sourcePK := ent.PrimaryKey
				if sourcePK == "" {
					sourcePK = "id"
				}
				targetPK := target.PrimaryKey
				if targetPK == "" {
					targetPK = "id"
				}
				sourcePKType := fkColumnSQLType(ent, sourcePK, dialect)
				targetPKType := fkColumnSQLType(target, targetPK, dialect)
				safeThrough, err := query.SafeIdent(pivotTable)
				if err != nil {
					continue
				}
				safeLocalKey, err := query.SafeIdent(rel.LocalKey)
				if err != nil {
					continue
				}
				safeTargetKey, err := query.SafeIdent(rel.ForeignKeyTarget)
				if err != nil {
					continue
				}
				safeSourceTable, err := query.SafeIdent(ent.GetTable())
				if err != nil {
					continue
				}
				safeSourcePK, err := query.SafeIdent(sourcePK)
				if err != nil {
					continue
				}
				safeTargetTable, err := query.SafeIdent(target.GetTable())
				if err != nil {
					continue
				}
				safeTargetPK, err := query.SafeIdent(targetPK)
				if err != nil {
					continue
				}

				snap.Tables[safeThrough] = map[string]string{
					safeLocalKey:  sourcePKType,
					safeTargetKey: targetPKType,
				}
				pivotDDL := fmt.Sprintf(
					"CREATE TABLE IF NOT EXISTS %s (\n\t%s %s NOT NULL,\n\t%s %s NOT NULL,\n\tPRIMARY KEY (%s, %s),\n\tFOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE,\n\tFOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE\n)",
					safeThrough,
					safeLocalKey, sourcePKType,
					safeTargetKey, targetPKType,
					safeLocalKey, safeTargetKey,
					safeLocalKey, safeSourceTable, safeSourcePK,
					safeTargetKey, safeTargetTable, safeTargetPK,
				)
				if snap.TableDDL == nil {
					snap.TableDDL = map[string]string{}
				}
				snap.TableDDL[safeThrough] = pivotDDL
				if snap.Indices == nil {
					snap.Indices = map[string][]string{}
				}
				snap.Indices[safeThrough] = []string{
					fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_%s ON %s(%s)", safeThrough, safeTargetKey, safeThrough, safeTargetKey),
				}
			}
		}
	}
	if len(plan.Views) > 0 {
		snap.Views = map[string]RoutineDef{}
		for _, v := range plan.Views {
			up, down := v.render(dialect)
			snap.Views[v.Name] = RoutineDef{Up: up, Down: down}
		}
	}
	if len(plan.Routines) > 0 {
		snap.Routines = map[string]RoutineDef{}
		for _, r := range plan.Routines {
			snap.Routines[r.Name] = RoutineDef{Up: r.Up, Down: r.Down}
		}
	}
	return snap
}

// GenerateMigration diffs the registered entities against prev (the last
// snapshot) and returns the forward (up) and inverse (down) DDL for the delta,
// plus the new snapshot to persist. up is empty when there is nothing to do.
//
// Covered changes: create table, add column, drop column, and drop table (when
// an entity is removed). Type changes are out of scope, same limitation as
// DiffSchema.
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
	}

	var changes []SchemaChange
	for _, ent := range ordered {
		if ent.Config.Unmanaged {
			continue // views / external tables generate no table DDL
		}
		prevCols := lookupTableCols(prev.Tables, ent.GetTable())
		entChanges, derr := diffEntityFromLive(ent, all, dialect, prevCols)
		if derr != nil {
			return "", "", SchemaSnapshot{}, fmt.Errorf("generate %s: %w", ent.GetName(), derr)
		}
		changes = append(changes, entChanges...)

		if len(prevCols) > 0 {
			safeTable, err := query.SafeIdent(ent.GetTable())
			if err == nil {
				desiredIdxList := entityIndices(ent)
				desiredIdxDDLs := make([]string, len(desiredIdxList))
				for i, idx := range desiredIdxList {
					desiredIdxDDLs[i] = indexDDL(safeTable, idx)
				}
				if prev.Indices != nil {
					prevIdxDDLs := lookupIndicesCase(prev.Indices, ent.GetTable())
					idxChanges := diffTableIndices(ent.GetName(), safeTable, desiredIdxDDLs, prevIdxDDLs)
					changes = append(changes, idxChanges...)
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
						changes = append(changes, SchemaChange{
							Summary: fmt.Sprintf("%s: index %s", ent.GetName(), safeIdxName),
							SQL:     ddl,
							Down:    fmt.Sprintf("DROP INDEX IF EXISTS %s", safeIdxName),
						})
					}
				}
			}
		}
	}

	if len(ordered) > 0 {
		pivotChanges, err := diffPivotTables(ordered, all, dialect, prev.Tables)
		if err != nil {
			return "", "", SchemaSnapshot{}, err
		}
		changes = append(changes, pivotChanges...)
	}

	// Tables in the snapshot that no entity declares anymore → DROP TABLE.
	declaredTables := map[string]bool{}
	for _, ent := range all {
		declaredTables[strings.ToLower(ent.GetTable())] = true
		for _, rel := range ent.Config.Relations {
			if rel.Type == entity.RelManyToMany && rel.Through != "" {
				declaredTables[strings.ToLower(rel.Through)] = true
			}
		}
	}
	dropped := make([]string, 0)
	for table := range prev.Tables {
		if !declaredTables[strings.ToLower(table)] {
			dropped = append(dropped, table)
		}
	}
	dropped = sortDroppedTables(dropped, prev.TableDDL)
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
			SQL:         fmt.Sprintf("DROP TABLE IF EXISTS %s", qtable),
			Down:        down,
			Destructive: true,
		})
	}

	// Views after table changes (they SELECT from those tables), then routines
	// after views. The reverse-ordered Down therefore drops routines, then
	// views, then tables. Dependencies unwind cleanly.
	viewRoutines := make([]Routine, 0, len(plan.Views))
	for _, v := range topoSortViews(plan.Views) {
		viewRoutines = append(viewRoutines, v.routine(dialect))
	}
	changes = append(changes, routineChanges(viewRoutines, prev.Views)...)
	changes = append(changes, routineChanges(plan.Routines, prev.Routines)...)

	// Deduplicate identical SchemaChange statements (e.g. index DDL emitted
	// both by diffEntityFromLive column additions and diffTableIndices).
	seenSQL := make(map[string]bool, len(changes))
	dedupedChanges := make([]SchemaChange, 0, len(changes))
	for _, c := range changes {
		norm := strings.TrimRight(strings.TrimSpace(c.SQL), ";")
		if norm != "" && seenSQL[norm] {
			continue
		}
		if norm != "" {
			seenSQL[norm] = true
		}
		dedupedChanges = append(dedupedChanges, c)
	}
	changes = dedupedChanges

	next = SnapshotFromPlan(plan, dialect)
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

// routineChanges diffs current routines against the snapshot. New or changed
// routines emit their Up; a changed routine's Down restores the previous body,
// a new routine's Down is its own Down. Removed routines are dropped (via the
// snapshot's stored Down) with their recreation as the inverse. Output is
// name-sorted for deterministic migrations.
func routineChanges(current []Routine, prev map[string]RoutineDef) []SchemaChange {
	var changes []SchemaChange
	seen := map[string]bool{}

	names := make([]string, 0, len(current))
	byName := map[string]Routine{}
	for _, r := range current {
		names = append(names, r.Name)
		byName[r.Name] = r
	}
	sort.Strings(names)
	for _, name := range names {
		seen[name] = true
		r := byName[name]
		prevDef, ok := prev[name]
		switch {
		case !ok:
			changes = append(changes, SchemaChange{
				Summary: fmt.Sprintf("routine %s: create", name),
				SQL:     r.Up,
				Down:    r.Down,
			})
		case prevDef.Up != r.Up:
			changes = append(changes, SchemaChange{
				Summary: fmt.Sprintf("routine %s: replace", name),
				SQL:     r.Up,
				Down:    prevDef.Up, // restore the previous definition
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
		// A Routine's Down is optional (see routine.go), so a routine
		// removed without one used to emit an EMPTY statement here. The
		// generated migration then contained nothing but ";": the drop
		// silently did nothing while the snapshot advanced to record the
		// routine as gone, and a rollback would CREATE OR REPLACE it
		// back. Surface it as a change the author must resolve instead
		// of writing a no-op that looks applied.
		drop := strings.TrimSpace(prev[name].Down)
		if drop == "" {
			changes = append(changes, SchemaChange{
				Summary: fmt.Sprintf("routine %s: drop SKIPPED: the routine declares no Down; "+
					"add one (e.g. DROP FUNCTION ...) so the removal actually runs", name),
				SQL:  "",
				Down: prev[name].Up,
			})
			continue
		}
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("routine %s: drop", name),
			SQL:     drop,          // the drop
			Down:    prev[name].Up, // recreate on rollback
		})
	}
	return changes
}

// recreateTableSQL reconstructs a CREATE TABLE from a snapshot's column set,
// the FALLBACK Down for a dropped table when the snapshot predates TableDDL.
// Constraints (PK/NOT NULL/UNIQUE/FK) are not recoverable from a column→type
// map, so this is column-and-type only; the TableDDL path is faithful.
// Identifiers are validated but UNQUOTED (via MustIdent), the same convention
// as buildCreateTableSQL, so a mixed-case name folds to lowercase on Postgres
// consistently with the original CREATE rather than being case-preserved by
// quoting. Column order is sorted for deterministic output.
func recreateTableSQL(table string, cols map[string]string) string {
	names := make([]string, 0, len(cols))
	for n := range cols {
		names = append(names, n)
	}
	sort.Strings(names)
	defs := make([]string, 0, len(names))
	for _, n := range names {
		defs = append(defs, fmt.Sprintf("%s %s", query.MustIdent(n), cols[n]))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)",
		query.MustIdent(table), strings.Join(defs, ",\n\t"))
}

// ErrDirectiveInSQL is returned when rendered SQL contains a line the
// migration runner would read as a `-- +migrate` directive.
var ErrDirectiveInSQL = errors.New("migrate: rendered SQL contains a line that would parse as a -- +migrate directive")

// containsDirectiveLine reports whether any line of sql would be read as
// a directive by the runner, which scans line by line for the
// "-- +migrate" prefix with no idea whether it is inside a string
// literal.
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
		if strings.HasPrefix(strings.TrimSpace(line), "-- +migrate") {
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
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

var reFKReferences = regexp.MustCompile(`(?i)\bREFERENCES\s+([a-zA-Z0-9_]+)`)

// sortDroppedTables orders tables for DROP TABLE so that child/referencing tables
// (such as pivot tables or tables with foreign keys) are dropped BEFORE the tables
// they reference. Because formatPlan reverses the changes slice for Down migrations
// (via slices.Backward), this ordering simultaneously guarantees that Down migrations
// recreate referenced/parent tables BEFORE the child tables that reference them.
func sortDroppedTables(tables []string, tableDDL map[string]string) []string {
	if len(tables) <= 1 {
		return tables
	}
	droppedSet := make(map[string]bool, len(tables))
	for _, t := range tables {
		droppedSet[strings.ToLower(t)] = true
	}

	// dep[A] = list of tables that A references (A must be dropped before B)
	dep := make(map[string][]string, len(tables))
	inDegree := make(map[string]int, len(tables))
	canon := make(map[string]string, len(tables)) // lower -> original casing
	for _, t := range tables {
		low := strings.ToLower(t)
		canon[low] = t
		inDegree[low] = 0
	}

	for _, t := range tables {
		low := strings.ToLower(t)
		ddl := lookupStringCase(tableDDL, t)
		if ddl == "" {
			continue
		}
		matches := reFKReferences.FindAllStringSubmatch(ddl, -1)
		seenRef := make(map[string]bool)
		for _, m := range matches {
			if len(m) > 1 {
				target := strings.ToLower(m[1])
				if target != low && droppedSet[target] && !seenRef[target] {
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
			if !visited[strings.ToLower(t)] {
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
				Down:    fmt.Sprintf("DROP INDEX IF EXISTS %s", safeName),
			})
		} else if strings.TrimRight(strings.TrimSpace(prevDDL), ";") != strings.TrimRight(strings.TrimSpace(ddl), ";") {
			// Index definition changed under the same index name (e.g. Unique flip, or column change under explicit name).
			// Drop the old index and create the new index.
			// Down reverses this: drops the new index and restores the old index DDL.
			dropSQL := fmt.Sprintf("DROP INDEX IF EXISTS %s", safeName)
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
			changes = append(changes, SchemaChange{
				Summary:     fmt.Sprintf("%s: remove index %s", entName, safeName),
				SQL:         fmt.Sprintf("DROP INDEX IF EXISTS %s", safeName),
				Down:        ddl,
				Destructive: true,
			})
		}
	}
	return changes
}
