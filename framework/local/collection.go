package local

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Default and ceiling caps. A record is re-serialised on every write
// and a collection is re-read to enforce its cap, so the ceilings are
// about what browser-owned state should cost, not what IndexedDB
// could hold.
const (
	DefaultMaxRecordBytes = 64 << 10
	MaxRecordBytesLimit   = 1 << 20
	DefaultMaxRecords     = 1000
	MaxRecordsLimit       = 100_000
	DefaultMaxBytes       = 1 << 20
	MaxBytesLimit         = 64 << 20

	// A mirrored record rides a cookie on EVERY request to the origin,
	// component-encoded (which can triple its size), inside the ~4 KiB
	// a browser allows one cookie and the few dozen it allows a domain.
	// The caps are therefore small and not raisable.
	MirrorDefaultMaxRecordBytes = 512
	MirrorDefaultMaxRecords     = 4
	MirrorMaxRecordBytes        = 1 << 10
	MirrorMaxRecords            = 16

	// MirrorStoreMaxBytes bounds what the mirrored collections of EVERY
	// store in the process may declare together: the sum of MaxRecords
	// × MaxRecordBytes over them. The per-collection ceilings bound one
	// cookie; nothing bounded the Cookie HEADER, and that is the one
	// the world has an opinion about: most proxies and servers refuse
	// a request header block over 8 to 16 KiB, and the answer is a 431
	// that makes the origin unreachable from that browser until the
	// user clears their cookies by hand. Four mirrored collections at
	// the old ceilings were 64 KiB of declaration. 4 KiB leaves room
	// for the session cookie, the CSRF cookie and everything else the
	// app puts on the origin, and the browser measures the same bound
	// on what it holds (encoding is not free). The budget is
	// per process and not per store because the Cookie header is per
	// origin: two stores an app declares ride the same header.
	MirrorStoreMaxBytes = 4 << 10
)

// CollectionConfig declares one collection.
type CollectionConfig struct {
	// Version is the schema version the browser must hold, 1 or more.
	// Raising it runs the Migrations whose Version lies in
	// (stored, Version] once per browser, in order, before the first
	// read or write of the page.
	Version int
	// Migrations are the steps that bring a record from one version to
	// the next. Every step is a data transform declared here (Rename,
	// Default, Remove) or a host-registered browser function (Func);
	// see Migration.
	Migrations []Migration
	// KeyField names the JSON field the browser's put(value) reads the
	// key from, so a record can carry its own identity. Optional: with
	// no key field, the browser API is put(key, value).
	KeyField string
	// MaxRecordBytes caps one record's JSON text. 0 means
	// DefaultMaxRecordBytes (MirrorMaxRecordBytes when Mirror).
	MaxRecordBytes int
	// MaxRecords caps the number of records. 0 means DefaultMaxRecords
	// (MirrorMaxRecords when Mirror).
	MaxRecords int
	// MaxBytes caps the sum of the collection's record sizes. 0 means
	// DefaultMaxBytes.
	MaxBytes int
	// Mirror keeps each record in a cookie too, so a Go render sees it
	// at first paint through Get/List. For tiny values only: the caps
	// above are clamped to the Mirror* ceilings, and every record
	// travels on every request.
	Mirror bool
}

// Migration is one version step: the steps that turn every record of
// version Version-1 into version Version.
type Migration struct {
	Version int
	Steps   []Step
}

// Step is one declared transform over a record. Build one with Rename,
// Default, Remove or Func.
type Step struct {
	op    string
	from  string
	to    string
	field string
	value any
	name  string
}

// Rename moves field from to field to. A record without from, or one
// that already has to, is left alone: the step is idempotent, which is
// what lets two tabs run the same migration without a lock.
func Rename(from, to string) Step { return Step{op: "rename", from: from, to: to} }

// Default sets field to value on every record that lacks it. value
// must be JSON-encodable.
func Default(field string, value any) Step { return Step{op: "default", field: field, value: value} }

// Remove deletes field from every record.
func Remove(field string) Step { return Step{op: "remove", field: field} }

// Func runs the browser function registered as
// window.__gofastr._localMigrations[name] over every record,
// (record, key) => record, loaded from the host's extra-script rail
// like a computed reducer (never inline: the page stays CSP-clean).
// A name with no function registered when the migration runs leaves
// the records and the stored version untouched and raises
// gofastr:local-error with reason "migration". Keep it idempotent.
func Func(name string) Step { return Step{op: "func", name: name} }

// MarshalJSON emits the step the browser reads.
func (st Step) MarshalJSON() ([]byte, error) {
	switch st.op {
	case "rename":
		return json.Marshal(map[string]any{"op": st.op, "from": st.from, "to": st.to})
	case "default":
		return json.Marshal(map[string]any{"op": st.op, "field": st.field, "value": st.value})
	case "remove":
		return json.Marshal(map[string]any{"op": st.op, "field": st.field})
	case "func":
		return json.Marshal(map[string]any{"op": st.op, "name": st.name})
	}
	return nil, fmt.Errorf("local: unknown migration step %q", st.op)
}

// collectionDef is the declaration the store keeps; the generic
// Collection[T] is a typed handle on it.
type collectionDef struct {
	store      *Store
	name       string
	version    int
	migrations []Migration
	keyField   string
	maxRecord  int
	maxRecords int
	maxBytes   int
	mirror     bool
}

// Collection is a typed handle on one declared collection.
type Collection[T any] struct {
	def *collectionDef
}

// Define declares collection name on s with the record type T, which
// must round-trip through encoding/json (a channel or a function
// field panics here, not in a handler). It panics on an invalid name,
// a duplicate name, a version below 1, a migration set with a gap or
// a duplicate, a cap over its ceiling, or a store whose manifest was
// already served.
func Define[T any](s *Store, name string, cfg CollectionConfig) *Collection[T] {
	if !reName.MatchString(name) {
		panic(fmt.Sprintf("local: collection %q must match ^[a-z][a-z0-9-]{0,63}$", name))
	}
	var zero T
	if _, err := json.Marshal(zero); err != nil {
		panic(fmt.Sprintf("local: collection %q: %T does not round-trip through JSON: %v", name, zero, err))
	}
	if k := reflect.TypeFor[T]().Kind(); cfg.KeyField != "" && k != reflect.Struct && k != reflect.Map {
		panic(fmt.Sprintf("local: collection %q: KeyField %q needs an object record type, not %s", name, cfg.KeyField, k))
	}
	if cfg.Version < 1 {
		panic(fmt.Sprintf("local: collection %q: Version must be 1 or more, got %d", name, cfg.Version))
	}
	seen := map[int]bool{}
	for _, m := range cfg.Migrations {
		if m.Version < 2 || m.Version > cfg.Version {
			panic(fmt.Sprintf("local: collection %q: migration to version %d is outside (1, %d]", name, m.Version, cfg.Version))
		}
		if seen[m.Version] {
			panic(fmt.Sprintf("local: collection %q: two migrations to version %d", name, m.Version))
		}
		seen[m.Version] = true
		for _, st := range m.Steps {
			if st.op == "" {
				panic(fmt.Sprintf("local: collection %q: a zero Step in the migration to version %d — use Rename, Default, Remove or Func", name, m.Version))
			}
			if st.op == "default" {
				if _, err := json.Marshal(st.value); err != nil {
					panic(fmt.Sprintf("local: collection %q: Default(%q) value does not encode: %v", name, st.field, err))
				}
			}
		}
	}
	for v := 2; v <= cfg.Version; v++ {
		if !seen[v] {
			panic(fmt.Sprintf("local: collection %q: no migration to version %d — every version step needs one, even an empty Migration{Version: %d}", name, v, v))
		}
	}
	def := &collectionDef{
		store:      s,
		name:       name,
		version:    cfg.Version,
		migrations: cfg.Migrations,
		keyField:   cfg.KeyField,
		mirror:     cfg.Mirror,
	}
	def.maxRecord = capOrDefault("MaxRecordBytes", name, cfg.MaxRecordBytes, DefaultMaxRecordBytes, MaxRecordBytesLimit)
	def.maxRecords = capOrDefault("MaxRecords", name, cfg.MaxRecords, DefaultMaxRecords, MaxRecordsLimit)
	def.maxBytes = capOrDefault("MaxBytes", name, cfg.MaxBytes, DefaultMaxBytes, MaxBytesLimit)
	if cfg.Mirror {
		def.maxRecord = capOrDefault("MaxRecordBytes", name, cfg.MaxRecordBytes, MirrorDefaultMaxRecordBytes, MirrorMaxRecordBytes)
		def.maxRecords = capOrDefault("MaxRecords", name, cfg.MaxRecords, MirrorDefaultMaxRecords, MirrorMaxRecords)
	}
	if def.maxRecord > def.maxBytes {
		panic(fmt.Sprintf("local: collection %q: MaxRecordBytes %d exceeds MaxBytes %d", name, def.maxRecord, def.maxBytes))
	}

	// Lock order: appsMu, then a store's mu. The mirror budget reads
	// every store, so the registry lock comes first and holds for the
	// whole declaration; nothing takes appsMu under a store's mu.
	appsMu.Lock()
	defer appsMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.frozen {
		panic(fmt.Sprintf("local: collection %q declared after store %q served its manifest — define every collection before the first render", name, s.app))
	}
	if _, dup := s.colls[name]; dup {
		panic(fmt.Sprintf("local: collection %q is already declared on store %q", name, s.app))
	}
	if def.mirror {
		total, sharing := mirrorDeclaredLocked(s)
		total += def.maxRecords * def.maxRecord
		if total > MirrorStoreMaxBytes {
			panic(fmt.Sprintf("local: store %q: the mirrored collections of stores %s declare %d bytes together, over the %d-byte budget; the Cookie header is per origin, so every store's mirrored collection rides it on EVERY request, and a header block past 8-16 KiB is a 431 the browser cannot recover from; lower MaxRecords or MaxRecordBytes, or stop mirroring a collection the server does not need at first paint", s.app, strings.Join(sharing, ", "), total, MirrorStoreMaxBytes))
		}
	}
	s.colls[name] = def
	return &Collection[T]{def: def}
}

// mirrorDeclaredLocked sums MaxRecords × MaxRecordBytes over every
// mirrored collection of every declared store, and names the stores
// that hold one, plus s. Called with appsMu and s.mu held; it takes
// each other store's mu in turn.
func mirrorDeclaredLocked(s *Store) (total int, sharing []string) {
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := apps[name]
		if st != s {
			st.mu.Lock()
		}
		mirrored := false
		for _, d := range st.colls {
			if d.mirror {
				mirrored = true
				total += d.maxRecords * d.maxRecord
			}
		}
		if st != s {
			st.mu.Unlock()
		}
		if mirrored || st == s {
			sharing = append(sharing, strconv.Quote(name))
		}
	}
	return total, sharing
}

func capOrDefault(what, coll string, v, def, limit int) int {
	if v < 0 {
		panic(fmt.Sprintf("local: collection %q: %s %d is negative", coll, what, v))
	}
	if v == 0 {
		return def
	}
	if v > limit {
		panic(fmt.Sprintf("local: collection %q: %s %d exceeds the ceiling %d", coll, what, v, limit))
	}
	return v
}

// Key names one record of the collection for Send.
func (c *Collection[T]) Key(key string) Ref {
	if !validRecordKey(key) {
		panic(fmt.Sprintf("local: collection %q: %q is not a valid key", c.def.name, key))
	}
	return Ref{def: c.def, key: key}
}

// Ref is one record of a collection, the unit Send accepts beside a
// whole collection.
type Ref struct {
	def *collectionDef
	key string
}

// sendItem is what Send records: a collection, and a key, "" for the
// whole collection.
type sendItem struct {
	def *collectionDef
	key string
}

// Sendable is a whole Collection or a Ref to one of its records.
type Sendable interface{ sendItem() sendItem }

func (c *Collection[T]) sendItem() sendItem { return sendItem{def: c.def} }
func (r Ref) sendItem() sendItem            { return sendItem{def: r.def, key: r.key} }

// manifestEntry is the per-collection block the browser reads.
type manifestEntry struct {
	Version    int             `json:"v"`
	Key        string          `json:"key,omitempty"`
	MaxRecord  int             `json:"maxRecord"`
	MaxRecords int             `json:"maxRecords"`
	MaxBytes   int             `json:"maxBytes"`
	Mirror     bool            `json:"mirror"`
	Migrations []manifestMigra `json:"migrations"`
}

type manifestMigra struct {
	Version int    `json:"v"`
	Steps   []Step `json:"steps"`
}

func (d *collectionDef) manifest() manifestEntry {
	m := manifestEntry{
		Version:    d.version,
		Key:        d.keyField,
		MaxRecord:  d.maxRecord,
		MaxRecords: d.maxRecords,
		MaxBytes:   d.maxBytes,
		Mirror:     d.mirror,
		Migrations: []manifestMigra{},
	}
	for _, mg := range d.migrations {
		steps := mg.Steps
		if steps == nil {
			steps = []Step{}
		}
		m.Migrations = append(m.Migrations, manifestMigra{Version: mg.Version, Steps: steps})
	}
	return m
}
