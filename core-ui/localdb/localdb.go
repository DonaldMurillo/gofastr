// Package localdb declares browser-side IndexedDB databases in Go.
package localdb

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

//go:embed localdb.js
var moduleJS string

var _ = registry.RegisterBehavior("localdb", moduleJS, registry.OnRequest())

func init() { registry.RegisterDataBlock("gofastr-localdb", ManifestJSON) }

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
	mu       sync.RWMutex
	dbs      = map[string]*dbSpec{}
	manifest []byte
)

// New declares a database; the browser opens it as "gofastr." + name.
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
	manifest = nil
	return &DB{name: name}
}

// Name returns the declared database name, without the "gofastr." prefix.
func (d *DB) Name() string { return d.name }

// StoreOption configures a store at declaration time.
type StoreOption func(*storeSpec)

// KeyPath sets the dotted property path each record is keyed by.
func KeyPath(path string) StoreOption {
	return func(s *storeSpec) { s.KeyPath = path }
}

// AutoKey makes the runtime mint a UUIDv7 key for a record written without one.
func AutoKey() StoreOption {
	return func(s *storeSpec) { s.AutoKey = true }
}

// Index declares an index over one property, or a compound index over several.
func Index(name string, paths ...string) StoreOption {
	return addIndex(name, &indexSpec{KeyPath: paths})
}

// UniqueIndex is Index with a uniqueness constraint.
func UniqueIndex(name string, paths ...string) StoreOption {
	return addIndex(name, &indexSpec{KeyPath: paths, Unique: true})
}

// MultiEntryIndex indexes each element of an array property separately.
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

// Store declares an object store in the database.
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
	manifest = nil
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

// ManifestJSON returns the manifest the runtime module reads, or nil when none is declared.
func ManifestJSON() []byte {
	mu.RLock()
	cached := manifest
	mu.RUnlock()
	if cached != nil {
		return cached
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dbs) == 0 {
		return nil
	}
	if manifest == nil {
		buf, err := json.Marshal(dbs)
		if err != nil {
			return nil
		}
		manifest = buf
	}
	return manifest
}

func validName(name string) bool { return compute.ValidName(name) }

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

func reset() {
	mu.Lock()
	defer mu.Unlock()
	dbs = map[string]*dbSpec{}
	manifest = nil
}
