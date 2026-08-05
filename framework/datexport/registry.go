// Package datexport is a process-wide registry of data-bearing tables that
// live OUTSIDE the framework entity registry — the physical tables a battery
// (auth sessions, the job queue, …) or an app creates with raw DDL.
//
// The framework's data export/import (App.ExportData / App.ImportData) walks
// BOTH the entity registry AND this registry so a dump is complete. A
// data-bearing module registers its tables from init() (mirroring
// framework/agentsinv): importing the module == including its tables.
//
// Entries are declarative — {Name, Table, PrimaryKey, Columns} — so the
// framework can centralize all raw read/write behind one SafeIdent-guarded
// code path (see framework/export_data.go). A registered table that is absent
// from the live DB at export time is skipped with a note; an unregistered raw
// table is silently excluded (register an exporter to include it).
//
// The same registry also carries the right-to-be-forgotten plane: a battery
// registers DataErasers (App.EraseUserData) so an erasure reaches the same
// tables an export does. Erasers are declarative too — {Table, Column, Mode} —
// and ride the same SafeIdent-guarded code path (see framework/erase_data.go).
package datexport

import (
	"sort"
	"sync"
	"testing"
)

// DataExporter describes one physical table owned by a battery or app that the
// entity registry does not cover.
//
//   - Name is the unique archive key and the .ndjson filename stem. It must be
//     a safe SQL identifier (it doubles as a lookup key) and unique across all
//     registered exporters and entity names.
//   - Source is the owning module ("auth", "queue", …) recorded in the manifest
//     for provenance; it is never used to build SQL.
//   - Table is the physical SQL table name.
//   - PrimaryKey is the keyset-paging column (defaults to "id" when empty).
//   - Columns are the physical column names in a stable order.
//
// Table, PrimaryKey, and every Column are validated by the framework via
// core/query.SafeIdent before they ever reach a query.
type DataExporter struct {
	Name       string
	Source     string
	Table      string
	PrimaryKey string
	Columns    []string
}

// EraseMode selects how a registered DataEraser removes a user's rows.
type EraseMode int

const (
	// EraseDelete hard-deletes every row where Column = userID. Use for tables
	// whose rows are pure user data (a session table, the user row itself).
	EraseDelete EraseMode = iota
	// EraseAnonymize does NOT delete; it overwrites each ScrubColumn with
	// Tombstone on every row where Column = userID. Use for rows that must be
	// retained (the audit trail) but whose user reference must be cut.
	EraseAnonymize
)

// DataEraser declares the right-to-be-forgotten behavior for one physical
// table that lives outside the entity registry — the erase-plane mirror of
// DataExporter. The framework's App.EraseUserData walks both the entity
// registry (owner-scoped entities) AND these registered erasers, so an
// erasure reaches the same tables an export does.
//
//   - Name is a unique key and the report label. Reusing the matching
//     DataExporter.Name keeps the two trails aligned.
//   - Source is the owning module ("auth") recorded in the report.
//   - Table is the physical SQL table name.
//   - Column is the user-id column matched against the erased user's id
//     (e.g. "user_id" for sessions, "id" for the user row, "actor_id" for an
//     audit table). Table and Column are SafeIdent-checked before SQL.
//   - Mode selects EraseDelete or EraseAnonymize.
//   - ScrubColumns (EraseAnonymize only) are the columns overwritten with
//     Tombstone. Each is SafeIdent-checked before SQL.
//   - Tombstone (EraseAnonymize only) is the replacement written to every
//     ScrubColumn. Empty defaults to "[erased]".
type DataEraser struct {
	Name         string
	Source       string
	Table        string
	Column       string
	Mode         EraseMode
	ScrubColumns []string
	Tombstone    string
}

var (
	mu      sync.RWMutex
	entries = []*DataExporter{}
	erasers = []*DataEraser{}
)

// Register adds a data exporter. Safe to call from init(). An exporter whose
// Name matches an existing entry replaces it (last-writer-wins) so a battery
// that registers a runtime-renamed table updates cleanly.
func Register(e DataExporter) {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range entries {
		if ex.Name == e.Name {
			cols := make([]string, len(e.Columns))
			copy(cols, e.Columns)
			entries[i] = &DataExporter{
				Name: e.Name, Source: e.Source, Table: e.Table,
				PrimaryKey: e.PrimaryKey, Columns: cols,
			}
			return
		}
	}
	cols := make([]string, len(e.Columns))
	copy(cols, e.Columns)
	entries = append(entries, &DataExporter{
		Name: e.Name, Source: e.Source, Table: e.Table,
		PrimaryKey: e.PrimaryKey, Columns: cols,
	})
}

// Unregister removes an exporter by Name. Returns true if an entry was removed.
func Unregister(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range entries {
		if ex.Name == name {
			entries = append(entries[:i], entries[i+1:]...)
			return true
		}
	}
	return false
}

// All returns a copy of the registered exporters sorted by Name.
func All() []DataExporter {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]DataExporter, 0, len(entries))
	for _, ex := range entries {
		out = append(out, DataExporter{
			Name: ex.Name, Source: ex.Source, Table: ex.Table,
			PrimaryKey: ex.PrimaryKey, Columns: append([]string(nil), ex.Columns...),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RegisterEraser adds a data eraser. Safe to call from init(). An eraser whose
// Name matches an existing entry replaces it (last-writer-wins), mirroring
// Register. Slices are copied defensively.
func RegisterEraser(e DataEraser) {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range erasers {
		if ex.Name == e.Name {
			erasers[i] = cloneEraser(e)
			return
		}
	}
	erasers = append(erasers, cloneEraser(e))
}

func cloneEraser(e DataEraser) *DataEraser {
	scrub := make([]string, len(e.ScrubColumns))
	copy(scrub, e.ScrubColumns)
	return &DataEraser{
		Name: e.Name, Source: e.Source, Table: e.Table, Column: e.Column,
		Mode: e.Mode, ScrubColumns: scrub, Tombstone: e.Tombstone,
	}
}

// UnregisterEraser removes an eraser by Name. Returns true if an entry was
// removed.
func UnregisterEraser(name string) bool {
	mu.Lock()
	defer mu.Unlock()
	for i, ex := range erasers {
		if ex.Name == name {
			erasers = append(erasers[:i], erasers[i+1:]...)
			return true
		}
	}
	return false
}

// AllErasers returns a copy of the registered erasers sorted by Name.
func AllErasers() []DataEraser {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]DataEraser, 0, len(erasers))
	for _, ex := range erasers {
		out = append(out, DataEraser{
			Name: ex.Name, Source: ex.Source, Table: ex.Table, Column: ex.Column,
			Mode: ex.Mode, ScrubColumns: append([]string(nil), ex.ScrubColumns...),
			Tombstone: ex.Tombstone,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Reset clears the registry. The *testing.T parameter is a discipline marker
// (production may pass nil since testing is stdlib); intended for tests.
func Reset(_ *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
	erasers = nil
}
