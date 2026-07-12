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

var (
	mu      sync.RWMutex
	entries = []*DataExporter{}
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

// Reset clears the registry. The *testing.T parameter is a discipline marker
// (production may pass nil since testing is stdlib); intended for tests.
func Reset(_ *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
}
