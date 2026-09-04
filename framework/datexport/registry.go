// Package datexport is a process-wide registry of data-bearing tables that
// live OUTSIDE the framework entity registry, the physical tables a battery
// (auth sessions, the job queue, …) or an app creates with raw DDL.
//
// The framework's data export/import (App.ExportData / App.ImportData) walks
// BOTH the entity registry AND this registry so a dump is complete. A
// data-bearing module registers its tables from init() (mirroring
// framework/agentsinv): importing the module == including its tables.
//
// Entries are declarative, {Name, Table, PrimaryKey, Columns}, so the
// framework can centralize all raw read/write behind one SafeIdent-guarded
// code path (see framework/export_data.go). A registered table that is absent
// from the live DB at export time is skipped with a note; an unregistered raw
// table is silently excluded (register an exporter to include it).
//
// The same registry also carries the right-to-be-forgotten plane: a battery
// registers DataErasers (App.EraseUserData) so an erasure reaches the same
// tables an export does. Erasers are declarative too, {Table, Column, Mode},
// and ride the same SafeIdent-guarded code path (see framework/erase_data.go).
package datexport

import (
	"fmt"
	"maps"
	"sort"
	"sync"
	"testing"
)

// DataExporter describes one physical table owned by a battery or app that the
// entity registry does not cover.
//
//   - Name is the unique archive key and the .ndjson filename stem. It must
//     be one path segment of [A-Za-z0-9_-] (Register panics otherwise: the
//     name reaches filepath.Join at export and import, and it doubles as a
//     lookup key) and unique across all registered exporters and entity
//     names.
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

// IdentityKind selects which principal an eraser matches against. The default
// (zero value, IdentityUserID) matches the erased user's id, the original
// behavior every existing registration relies on. A non-default identity is
// resolved ONCE at erase time via a registered [DataIdentityResolver], and the
// resolved value is bound for the eraser's Column instead of the user id.
//
// This is the seam that reaches tables keyed by an identity OTHER than the
// user id: battery/auth's magic_link_tokens is keyed by EMAIL, so it declares
// IdentityEmail and the framework resolves email from the user table at erase
// time. See framework/erase_data.go → identity resolution.
type IdentityKind int

const (
	// IdentityUserID matches Column against the erased user's id. It is the
	// zero value so every DataEraser that leaves Identity unset keeps the
	// original behavior.
	IdentityUserID IdentityKind = iota
	// IdentityEmail matches Column against the erased user's email, resolved
	// from the user table at erase time. battery/auth registers the resolver
	// against auth_users (id → email).
	IdentityEmail
)

// DataEraser declares the right-to-be-forgotten behavior for one physical
// table that lives outside the entity registry, the erase-plane mirror of
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
//   - Identity selects which principal Column is matched against. The default
//     (IdentityUserID, the zero value) matches the erased user's id, the
//     original behavior. A non-default Identity (e.g. IdentityEmail) is
//     resolved once at erase time through the matching DataIdentityResolver,
//     and the resolved value is bound for Column instead of the user id.
type DataEraser struct {
	Name         string
	Source       string
	Table        string
	Column       string
	Mode         EraseMode
	ScrubColumns []string
	Tombstone    string
	Identity     IdentityKind
}

// DataIdentityResolver declares how the framework resolves a non-user-id
// identity at erase time, declaratively, so the framework stays the single
// place raw SQL is built (decision E3). The framework runs, ONCE per erasure
// and BEFORE the write transaction opens,
//
//	SELECT ValueColumn FROM Table WHERE IDColumn = <erased user id>
//
// and binds the result for every eraser declaring the resolver's
// IdentityKind. Table, IDColumn, and ValueColumn are SafeIdent-checked before
// SQL; the erased user id is a $n bound argument.
//
// battery/auth registers IdentityEmail against auth_users (id → email) so the
// magic-link token table, keyed by email and not user id, becomes reachable by
// App.EraseUserData. A resolver whose Table is absent at erase time, or whose
// SELECT finds no row (the user row is already gone on an idempotent re-run),
// means the identity cannot be resolved: erasers declaring it are SKIPPED with
// a note rather than failed (nothing left to match).
type DataIdentityResolver struct {
	Table       string
	IDColumn    string
	ValueColumn string
}

var (
	mu        sync.RWMutex
	entries   = []*DataExporter{}
	erasers   = []*DataEraser{}
	resolvers = map[IdentityKind]DataIdentityResolver{}
)

// Register adds a data exporter. Safe to call from init(). An exporter whose
// Name matches an existing entry replaces it (last-writer-wins) so a battery
// that registers a runtime-renamed table updates cleanly.
//
// Name is validated here, once, at registration: it becomes the
// <name>.ndjson path stem through filepath.Join at export AND import
// (framework/export_data.go), so a name carrying path separators or dots
// reads/writes outside the export dir. It is developer input registered from
// init(), so the refusal is a panic at construction — the query.MustIdent
// precedent — never a silent skip that would quietly exclude the table from
// every export.
func Register(e DataExporter) {
	if !validExporterName(e.Name) {
		panic(fmt.Sprintf("datexport: Register: unsafe exporter Name %q: "+
			"must be one path segment of [A-Za-z0-9_-] (it becomes the "+
			"<name>.ndjson path stem at export and import)", e.Name))
	}
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

// validExporterName reports whether n is a safe archive key: one path
// segment of [A-Za-z0-9_-], non-empty. Separators and dots are refused so
// the name can never traverse out of the export dir once it is joined into
// a filename.
func validExporterName(n string) bool {
	if n == "" {
		return false
	}
	for _, r := range n {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
		default:
			return false
		}
	}
	return true
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
		Identity: e.Identity,
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
			Tombstone: ex.Tombstone, Identity: ex.Identity,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RegisterIdentityResolver registers how a non-user-id identity is resolved at
// erase time. Safe to call from init(); a re-registration for the same kind
// replaces it (last-writer-wins), mirroring RegisterEraser. battery/auth
// registers IdentityEmail from init() so any app importing battery/auth can
// reach email-keyed tables (magic_link_tokens) via App.EraseUserData.
func RegisterIdentityResolver(kind IdentityKind, r DataIdentityResolver) {
	mu.Lock()
	defer mu.Unlock()
	resolvers[kind] = r
}

// UnregisterIdentityResolver removes a resolver by kind. Returns true if one
// was present.
func UnregisterIdentityResolver(kind IdentityKind) bool {
	mu.Lock()
	defer mu.Unlock()
	_, ok := resolvers[kind]
	delete(resolvers, kind)
	return ok
}

// ResolveIdentity looks up the resolver declared for kind. The second return
// is false when no resolver is registered. App.EraseUserData treats an eraser
// that declares such an identity as a FAIL-LOUD misconfiguration (the erasure
// would be incomplete).
func ResolveIdentity(kind IdentityKind) (DataIdentityResolver, bool) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := resolvers[kind]
	return r, ok
}

// AllIdentityResolvers returns a copy of the registered resolvers.
func AllIdentityResolvers() map[IdentityKind]DataIdentityResolver {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[IdentityKind]DataIdentityResolver, len(resolvers))
	maps.Copy(out, resolvers)
	return out
}

// Reset clears the registry. The *testing.T parameter is a discipline marker
// (production may pass nil since testing is stdlib); intended for tests.
func Reset(_ *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
	erasers = nil
	for k := range resolvers {
		delete(resolvers, k)
	}
}
