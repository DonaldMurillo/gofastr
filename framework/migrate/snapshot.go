package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

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
	Tables   map[string]map[string]string `json:"tables"`
	Views    map[string]RoutineDef        `json:"views,omitempty"`
	Routines map[string]RoutineDef        `json:"routines,omitempty"`
}

// RoutineDef is the snapshot record for a routine — its Up and Down bodies, so
// a later generation can restore the previous definition on rollback.
type RoutineDef struct {
	Up   string `json:"up"`
	Down string `json:"down"`
}

// SnapshotFromRegistry builds the desired-state snapshot from the registered
// entities for the given dialect — the schema the entities describe right now.
func SnapshotFromRegistry(reg entity.Registry, dialect Dialect) SchemaSnapshot {
	return SnapshotFromPlan(Plan{Registry: reg}, dialect)
}

// SnapshotFromPlan builds the desired-state snapshot from a full Plan (tables
// plus routines).
func SnapshotFromPlan(plan Plan, dialect Dialect) SchemaSnapshot {
	snap := SchemaSnapshot{Tables: map[string]map[string]string{}}
	if plan.Registry != nil {
		for _, ent := range plan.Registry.All() {
			if ent.Config.Unmanaged {
				continue // views / external tables aren't part of the table snapshot
			}
			cols := map[string]string{}
			for _, f := range ent.GetFields() {
				cols[f.Name] = SQLType(f, dialect)
			}
			snap.Tables[ent.GetTable()] = cols
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
// an entity is removed). Type changes are out of scope — same limitation as
// DiffSchema.
func GenerateMigration(reg entity.Registry, prev SchemaSnapshot, dialect Dialect) (up, down string, next SchemaSnapshot, err error) {
	return GeneratePlan(Plan{Registry: reg}, prev, dialect)
}

// GeneratePlan is GenerateMigration for a full Plan — it diffs both the tables
// (entities + raw Tables) and the routines against the snapshot, emitting one
// reversible migration that covers everything. Routine bodies are compared
// verbatim; a changed routine's Down restores the previous body, and a removed
// routine is dropped (with its recreation as the Down).
func GeneratePlan(plan Plan, prev SchemaSnapshot, dialect Dialect) (up, down string, next SchemaSnapshot, err error) {
	all := map[string]*entity.Entity{}
	var ordered []*entity.Entity
	if plan.Registry != nil {
		all = plan.Registry.All()
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
		prevCols := prev.Tables[ent.GetTable()]
		entChanges, derr := diffEntityFromLive(ent, all, dialect, prevCols)
		if derr != nil {
			return "", "", SchemaSnapshot{}, fmt.Errorf("generate %s: %w", ent.GetName(), derr)
		}
		changes = append(changes, entChanges...)
	}

	// Tables in the snapshot that no entity declares anymore → DROP TABLE.
	declaredTables := map[string]bool{}
	for _, ent := range all {
		declaredTables[ent.GetTable()] = true
	}
	dropped := make([]string, 0)
	for table := range prev.Tables {
		if !declaredTables[table] {
			dropped = append(dropped, table)
		}
	}
	sort.Strings(dropped)
	for _, table := range dropped {
		changes = append(changes, SchemaChange{
			Summary:     fmt.Sprintf("%s: drop table", table),
			SQL:         fmt.Sprintf("DROP TABLE IF EXISTS %s", table),
			Down:        recreateTableSQL(table, prev.Tables[table]),
			Destructive: true,
		})
	}

	// Views after table changes (they SELECT from those tables), then routines
	// after views. The reverse-ordered Down therefore drops routines, then
	// views, then tables — dependencies unwind cleanly.
	viewRoutines := make([]Routine, 0, len(plan.Views))
	for _, v := range topoSortViews(plan.Views) {
		viewRoutines = append(viewRoutines, v.routine(dialect))
	}
	changes = append(changes, routineChanges(viewRoutines, prev.Views)...)
	changes = append(changes, routineChanges(plan.Routines, prev.Routines)...)

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
	for i := len(changes) - 1; i >= 0; i-- {
		if d := strings.TrimSpace(changes[i].Down); d != "" {
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
		changes = append(changes, SchemaChange{
			Summary: fmt.Sprintf("routine %s: drop", name),
			SQL:     prev[name].Down, // the drop
			Down:    prev[name].Up,   // recreate on rollback
		})
	}
	return changes
}

// recreateTableSQL reconstructs a CREATE TABLE from a snapshot's column set, the
// Down for a dropped table. Column order is sorted for deterministic output.
func recreateTableSQL(table string, cols map[string]string) string {
	names := make([]string, 0, len(cols))
	for n := range cols {
		names = append(names, n)
	}
	sort.Strings(names)
	defs := make([]string, 0, len(names))
	for _, n := range names {
		defs = append(defs, fmt.Sprintf("%s %s", n, cols[n]))
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n\t%s\n)", table, strings.Join(defs, ",\n\t"))
}

// RenderMigrationFile formats a versioned migration in the `-- +migrate`
// directive layout the runner parses. down may be empty.
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

// LoadSnapshot reads a snapshot JSON file. A missing file is not an error — it
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
	// MarshalIndent cannot fail for a snapshot of string maps — no channels,
	// funcs, or cycles — so the only real error here is the file write.
	data, _ := json.MarshalIndent(snap, "", "  ")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
