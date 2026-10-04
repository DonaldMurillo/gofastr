// Package localdb declares browser-side IndexedDB databases in Go.
//
// A database is declared once, at package init, with the object stores
// and indexes it holds:
//
//	var Pokedex = localdb.New("pokedex")
//	var Members = Pokedex.Store("members", localdb.AutoKey(),
//	    localdb.Index("by_created", "createdAt"))
//
// The declarations become an inert JSON manifest the host puts in every
// page head (<script type="application/json" id="gofastr-localdb">).
// The `localdb` runtime module reads it and exposes
// window.__gofastr.localdb: open a declared database, read and write
// its declared stores, watch them for changes from this tab and every
// other tab of the origin. Nothing here touches the server: the data
// lives in the visitor's browser.
//
// The schema is applied additively. When a page opens a database whose
// stored schema lacks a declared store or index, the runtime bumps the
// IndexedDB version itself and creates what is missing; it never
// deletes a store or an undeclared index, because a tab still running
// the previous deployment may need it. There is no version number to
// maintain.
//
// This package is the storage layer only. framework/localentity builds
// entity declarations, forms and lists on top of it.
package localdb

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// DefaultKeyPath is the key path a store uses when KeyPath is not given.
const DefaultKeyPath = "id"

// DB is one declared IndexedDB database.
type DB struct {
	name string
}

// Store is one declared object store inside a DB.
type Store struct {
	db   string
	name string
}

// spec types are the manifest wire shape; the JSON tags are what the
// runtime module reads.
type dbSpec struct {
	Stores map[string]*storeSpec `json:"stores"`
}

type storeSpec struct {
	KeyPath string                `json:"keyPath"`
	AutoKey bool                  `json:"autoKey,omitempty"`
	Indexes map[string]*indexSpec `json:"indexes,omitempty"`
}

type indexSpec struct {
	KeyPath    []string `json:"keyPath"`
	Unique     bool     `json:"unique,omitempty"`
	MultiEntry bool     `json:"multiEntry,omitempty"`
}

var (
	mu  sync.RWMutex
	dbs = map[string]*dbSpec{}
)

// New declares a database. name is 1-64 bytes of lowercase letters,
// digits, '-' or '_'; the browser-side IndexedDB name is "gofastr." +
// name, so a declared database never collides with one the page's own
// scripts open. Declaring the same name twice panics: two packages
// that each think they own a database would each apply half a schema.
func New(name string) *DB {
	if !validName(name) {
		panic(fmt.Sprintf("localdb: invalid database name %q (use 1-64 lowercase letters, digits, '-' or '_')", name))
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := dbs[name]; dup {
		panic(fmt.Sprintf("localdb: database %q declared twice", name))
	}
	dbs[name] = &dbSpec{Stores: map[string]*storeSpec{}}
	return &DB{name: name}
}

// Name returns the declared database name (without the "gofastr."
// prefix the browser stores it under).
func (d *DB) Name() string { return d.name }

// StoreOption configures a store at declaration time.
type StoreOption func(*storeSpec)

// KeyPath sets the property each record is keyed by. Dotted paths
// reach into nested objects ("meta.slug"). Defaults to "id".
func KeyPath(path string) StoreOption {
	return func(s *storeSpec) { s.KeyPath = path }
}

// AutoKey makes the runtime mint a key when a record is written
// without one: a UUIDv7 string (time-ordered, 74 random bits from
// crypto.getRandomValues), so records created offline in two tabs
// never collide and sort by creation. Requires a single-segment key
// path.
func AutoKey() StoreOption {
	return func(s *storeSpec) { s.AutoKey = true }
}

// Index declares a secondary index over one property, or a compound
// index over several (paths in order). Lists can then be read in index
// order, which is how a local list sorts: by IndexedDB, never by page
// script.
func Index(name string, paths ...string) StoreOption {
	return addIndex(name, &indexSpec{KeyPath: paths})
}

// UniqueIndex is Index with a uniqueness constraint: a write that
// would duplicate an indexed value fails with the runtime error code
// "constraint".
func UniqueIndex(name string, paths ...string) StoreOption {
	return addIndex(name, &indexSpec{KeyPath: paths, Unique: true})
}

// MultiEntryIndex indexes each element of an array property separately
// (a record tagged ["fire","flying"] is found under both).
func MultiEntryIndex(name, path string) StoreOption {
	return addIndex(name, &indexSpec{KeyPath: []string{path}, MultiEntry: true})
}

func addIndex(name string, ix *indexSpec) StoreOption {
	return func(s *storeSpec) {
		if !validName(name) {
			panic(fmt.Sprintf("localdb: invalid index name %q (use 1-64 lowercase letters, digits, '-' or '_')", name))
		}
		if _, dup := s.Indexes[name]; dup {
			panic(fmt.Sprintf("localdb: index %q declared twice", name))
		}
		if len(ix.KeyPath) == 0 {
			panic(fmt.Sprintf("localdb: index %q needs at least one key path", name))
		}
		for _, p := range ix.KeyPath {
			if err := checkKeyPath(p); err != nil {
				panic(fmt.Sprintf("localdb: index %q: %v", name, err))
			}
		}
		if ix.MultiEntry && len(ix.KeyPath) != 1 {
			panic(fmt.Sprintf("localdb: index %q: a multi-entry index takes exactly one key path", name))
		}
		if s.Indexes == nil {
			s.Indexes = map[string]*indexSpec{}
		}
		s.Indexes[name] = ix
	}
}

// Store declares an object store in the database. Store names follow
// the database-name grammar. Declaring the same store twice panics.
func (d *DB) Store(name string, opts ...StoreOption) *Store {
	if !validName(name) {
		panic(fmt.Sprintf("localdb: invalid store name %q (use 1-64 lowercase letters, digits, '-' or '_')", name))
	}
	s := &storeSpec{KeyPath: DefaultKeyPath}
	for _, opt := range opts {
		opt(s)
	}
	if err := checkKeyPath(s.KeyPath); err != nil {
		panic(fmt.Sprintf("localdb: store %q: %v", name, err))
	}
	if s.AutoKey && strings.Contains(s.KeyPath, ".") {
		panic(fmt.Sprintf("localdb: store %q: AutoKey needs a single-segment key path, got %q", name, s.KeyPath))
	}
	mu.Lock()
	defer mu.Unlock()
	spec := dbs[d.name]
	if _, dup := spec.Stores[name]; dup {
		panic(fmt.Sprintf("localdb: store %q declared twice in database %q", name, d.name))
	}
	spec.Stores[name] = s
	return &Store{db: d.name, name: name}
}

// Name returns the store name.
func (s *Store) Name() string { return s.name }

// DB returns the name of the database the store belongs to.
func (s *Store) DB() string { return s.db }

// KeyPath returns the property the store keys records by.
func (s *Store) KeyPath() string {
	mu.RLock()
	defer mu.RUnlock()
	return dbs[s.db].Stores[s.name].KeyPath
}

// HasIndex reports whether the store declares an index named name.
func (s *Store) HasIndex(name string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := dbs[s.db].Stores[s.name].Indexes[name]
	return ok
}

// Names returns the declared database names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(dbs))
	for name := range dbs {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// ManifestJSON returns the manifest the runtime module reads, or nil
// when no database is declared. encoding/json sorts map keys, so the
// bytes are deterministic for a given set of declarations.
func ManifestJSON() []byte {
	mu.RLock()
	defer mu.RUnlock()
	if len(dbs) == 0 {
		return nil
	}
	buf, err := json.Marshal(dbs)
	if err != nil {
		return nil
	}
	return buf
}

// validName is the name grammar shared with runtime modules and
// compute assets: 1-64 bytes of lowercase letters, digits, '-' or '_'.
func validName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// checkKeyPath accepts dotted JavaScript identifiers ([A-Za-z_$][\w$]*
// per segment, ASCII only), at most 128 bytes, and refuses the
// segments that name an object's prototype machinery: a key path is
// read and written by the runtime as a property chain.
func checkKeyPath(p string) error {
	if p == "" || len(p) > 128 {
		return fmt.Errorf("key path %q must be 1-128 bytes", p)
	}
	for seg := range strings.SplitSeq(p, ".") {
		if !validSegment(seg) {
			return fmt.Errorf("key path %q: segment %q is not an ASCII identifier", p, seg)
		}
		switch seg {
		case "__proto__", "constructor", "prototype":
			return fmt.Errorf("key path %q: segment %q is reserved", p, seg)
		}
	}
	return nil
}

func validSegment(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == '$':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// reset clears every declaration. Tests only.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	dbs = map[string]*dbSpec{}
}
