package crud

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The handler's derived field caches (visible-field projection, wire-key
// maps, JSON-column sets) are shared request-path state. They used to be
// plain struct maps rebuilt in place by refreshFieldCache whenever the
// entity's field declarations changed signature, which put unguarded map
// writes on the request path: two concurrent list requests could both
// decide the cache was stale and both rebuild it, killing the process with
// the unrecoverable "concurrent map writes" (and racing under -race even
// when they didn't crash).
//
// The fix is copy-on-write with immutable snapshots. Everything the
// request path needs is derived once into a fieldCacheData value that is
// NEVER mutated after publication; readers load the current snapshot and
// may share it freely (no locks, no torn reads). A rebuild constructs a
// fresh snapshot and swaps it in under fieldCache.mu.
//
// Snapshot staleness is a deliberate contract, not an oversight: the
// request path never re-reads the entity's live field declarations, so a
// host flipping a schema.Field (say Hidden) while traffic is in flight
// cannot race the readers. Mutations are picked up at the next
// SINGLE-THREADED schema read — VisibleFields() re-checks the signature
// and republishes — or by rebuilding the handler. Raw concurrent
// mutation during live traffic is served from the last consistent
// snapshot, which is the only safe answer: detecting the change would
// require reading the very bytes being written.

// fieldCacheData is one immutable derived-field snapshot. Every field is
// read-only once published.
type fieldCacheData struct {
	// fields is a stable copy of the entity's declarations the snapshot
	// was built from. Request paths that need the schema (filter parsing,
	// validation) read this copy, never Entity.GetFields(), so a host
	// mutating the live declarations cannot race them.
	fields []schema.Field
	// visible holds the non-Hidden field names, visibleJSONKeys the same
	// fields under their wire keys.
	visible      []string
	visibleJSONs []string
	// wireKeyOf maps DB column → wire key, columnOfWire the reverse.
	// Covers ALL fields (visible or not) so input deserialization can
	// resolve a WireName on a write-only field.
	wireKeyOf    map[string]string
	columnOfWire map[string]string
	// jsonColumns / jsonWireKeys hold the schema.JSON fields by column and
	// by wire key. The write path binds by column, the read path (scanned
	// rows are already key-converted) by wire key.
	jsonColumns  map[string]struct{}
	jsonWireKeys map[string]struct{}
	sig          uint64
}

// fieldCache is the shared holder. The handler stores a POINTER to it, so
// the tx-bound copies inTx makes share the same cache instead of copying
// a lock by value.
type fieldCache struct {
	mu  sync.RWMutex
	cur *fieldCacheData
}

func (fc *fieldCache) load() *fieldCacheData {
	fc.mu.RLock()
	s := fc.cur
	fc.mu.RUnlock()
	return s
}

func (fc *fieldCache) publish(s *fieldCacheData) {
	fc.mu.Lock()
	fc.cur = s
	fc.mu.Unlock()
}

// newFieldCache builds the initial snapshot for ch. Called from
// NewCrudHandler (single-threaded, before any request) and from the lazy
// init path for handlers constructed as bare struct literals.
func newFieldCache(ch *CrudHandler) *fieldCache {
	fc := &fieldCache{}
	fc.publish(buildFieldSnapshot(ch))
	return fc
}

// fieldCacheInitMu serializes the one-time cache allocation for handlers
// built as struct literals (`&CrudHandler{Entity: …}`, the include /
// nested-filter / llmmd probes). NewCrudHandler allocates eagerly, so the
// nil path never runs for handlers it built; a hand-built handler shared
// across goroutines before its first use was never supported (the old
// lazy map fill was equally racy there).
var fieldCacheInitMu sync.Mutex

// snapshot returns the current field-cache snapshot, building it lazily
// (once) for hand-constructed handlers. The returned snapshot is immutable
// and safe to read from any goroutine forever.
func (ch *CrudHandler) snapshot() *fieldCacheData {
	if fc := ch.fcache; fc != nil {
		if s := fc.load(); s != nil {
			return s
		}
	}
	fieldCacheInitMu.Lock()
	defer fieldCacheInitMu.Unlock()
	if ch.fcache == nil {
		ch.fcache = newFieldCache(ch)
	}
	return ch.fcache.load()
}

// refreshFieldCacheIfStale re-reads the entity's live field declarations
// (single-threaded host introspection only — VisibleFields) and
// republishes the snapshot when the signature changed. This is the
// supported way a mid-flight field mutation reaches the request path: the
// host mutates, then the next schema read picks it up.
func (ch *CrudHandler) refreshFieldCacheIfStale() {
	if ch.fcache == nil {
		return // hand-built handler: every read rebuilds anyway
	}
	if s := ch.fcache.load(); s != nil && s.sig == ch.fieldCacheSignature() {
		return
	}
	ch.fcache.publish(buildFieldSnapshot(ch))
}

// buildFieldSnapshot derives every cached field mapping from the
// entity's current declarations. Pure: writes nothing back to ch.
func buildFieldSnapshot(ch *CrudHandler) *fieldCacheData {
	s := &fieldCacheData{
		wireKeyOf:    map[string]string{},
		columnOfWire: map[string]string{},
		jsonColumns:  map[string]struct{}{},
		jsonWireKeys: map[string]struct{}{},
	}
	if ch.Entity == nil {
		s.sig = ch.fieldCacheSignature()
		return s
	}
	fields := ch.Entity.GetFields()
	s.fields = append([]schema.Field(nil), fields...)
	for _, f := range fields {
		// Build the wire-key maps for ALL fields (visible or not) so
		// input deserialization can resolve a WireName on a write-only
		// field. First field wins on collision, but a collision cannot
		// reach here: entity.Validate rejects two fields resolving to
		// one wire key at registration, because the write path (this
		// map) and the filter path (filter.ParseFiltersValues, which
		// builds its own alias map) would otherwise disagree about
		// which column the key means.
		//
		// The keep-first branch stays as a belt-and-braces guard for
		// any caller constructing a CrudHandler without going through
		// Validate.
		wk := ch.wireKeyOfField(f)
		s.wireKeyOf[f.Name] = wk
		if _, exists := s.columnOfWire[wk]; !exists {
			s.columnOfWire[wk] = f.Name
		}
		if f.Type == schema.JSON {
			s.jsonColumns[f.Name] = struct{}{}
			s.jsonWireKeys[wk] = struct{}{}
		}
		if !f.Hidden {
			s.visible = append(s.visible, f.Name)
		}
	}
	s.visibleJSONs = convertedKeys(s.visible, ch.convertKeyRaw)
	s.sig = ch.fieldCacheSignature()
	return s
}

// snapshotFields returns the stable field copy for request-path schema
// consumers (filter parsing, sort validation, the where-tree). Reading
// the live Entity.GetFields() on the request path would race a host
// mutating field declarations mid-traffic; the snapshot copy cannot.
func (ch *CrudHandler) snapshotFields() []schema.Field {
	return ch.snapshot().fields
}
