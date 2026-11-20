// Package store is the typed, server-declared signal store: declared once,
// valued per request and seeded into the client signal bus at SSR.
package store

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Scope controls when a slice's value is seeded into a page.
type Scope int

const (
	// ScopePage seeds only on pages whose HTML references the slice.
	ScopePage Scope = iota
	// ScopeGlobal seeds on every page and survives client navigation.
	ScopeGlobal
)

// Store namespaces a group of slices.
type Store struct{ ns string }

// New returns a Store whose slice names are prefixed with namespace and a dot.
func New(namespace string) *Store { return &Store{ns: namespace} }

// String declares a string-typed slice.
func (s *Store) String(name, def string) *Slice[string] { return declare(s, name, def) }

// Int declares an int-typed slice.
func (s *Store) Int(name string, def int) *Slice[int] { return declare(s, name, def) }

// Bool declares a bool-typed slice.
func (s *Store) Bool(name string, def bool) *Slice[bool] { return declare(s, name, def) }

// JSON declares a slice of a JSON-serialisable type.
func JSON[T any](s *Store, name string, def T) *Slice[T] { return declare(s, name, def) }

// Computed declares a slice the runtime derives on the client by running the
// registered reducer over deps; it is never seeded.
func Computed[T any](s *Store, name, reducer string, deps ...string) *Slice[T] {
	full := name
	if s.ns != "" {
		full = s.ns + "." + name
	}
	validateName(full)
	validateName(reducer)
	for _, dep := range deps {
		validateDep(dep)
	}
	registerComputed(full, deps)
	var zero T
	return &Slice[T]{name: full, scope: ScopePage, def: zero, comp: &computedCfg{reducer: reducer, deps: deps}}
}

type decl struct {
	name     string
	scope    Scope
	def      any
	computed bool
	deps     []string
}

var (
	regMu        sync.RWMutex
	declRegistry = map[string]*decl{}
)

func declare[T any](s *Store, name string, def T) *Slice[T] {
	full := name
	if s.ns != "" {
		full = s.ns + "." + name
	}
	validateName(full)
	register(full, ScopePage, any(def))
	return &Slice[T]{name: full, scope: ScopePage, def: def}
}

func register(name string, scope Scope, def any) {
	regMu.Lock()
	defer regMu.Unlock()
	if d, ok := declRegistry[name]; ok {
		if !reflect.DeepEqual(d.def, def) {
			panic(fmt.Sprintf("store: slice %q re-declared with a different default (%v vs %v)", name, d.def, def))
		}
		return
	}
	declRegistry[name] = &decl{name: name, scope: scope, def: def}
}

func registerComputed(name string, deps []string) {
	regMu.Lock()
	defer regMu.Unlock()
	if d, ok := declRegistry[name]; ok {
		if !d.computed {
			panic(fmt.Sprintf("store: %q is already a value slice; cannot redeclare as computed", name))
		}
		return
	}
	declRegistry[name] = &decl{name: name, scope: ScopePage, def: nil, computed: true, deps: slices.Clone(deps)}
	if path := computedCycle(name); path != nil {
		delete(declRegistry, name)
		panic(fmt.Sprintf("store: computed %q has a dependency cycle: %s", name, strings.Join(path, " -> ")))
	}
}

func computedCycle(start string) []string {
	var path []string
	seen := map[string]bool{}
	var walk func(n string) bool
	walk = func(n string) bool {
		d, ok := declRegistry[n]
		if !ok || !d.computed || seen[n] {
			return false
		}
		seen[n] = true
		for _, dep := range d.deps {
			path = append(path, dep)
			if dep == start || walk(dep) {
				return true
			}
			path = path[:len(path)-1]
		}
		return false
	}
	if walk(start) {
		return append([]string{start}, path...)
	}
	return nil
}

func setScope(name string, scope Scope) {
	regMu.Lock()
	defer regMu.Unlock()
	if d, ok := declRegistry[name]; ok {
		d.scope = scope
	}
}

func validateName(name string) {
	if name == "" {
		panic("store: slice name must not be empty")
	}
	if strings.HasPrefix(name, "route.") {
		panic(fmt.Sprintf("store: slice name %q is reserved — the route.* prefix belongs to the router's snapshot (store.Route declares the family); declare your slice under another name", name))
	}
	switch name {
	case "__proto__", "constructor", "prototype":
		panic(fmt.Sprintf("store: slice name %q is reserved — the runtime kernel refuses every client write to __proto__/constructor/prototype, so the slice would be a dead binding", name))
	}
	validateNameChars(name)
}

func validateDep(name string) {
	if strings.HasPrefix(name, "route.") {
		validateNameChars(name)
		return
	}
	validateName(name)
}

func validateNameChars(name string) {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			panic(fmt.Sprintf("store: invalid slice name %q (allowed: letters, digits, '.', '_', '-')", name))
		}
	}
}

// GlobalNames returns the sorted names of every ScopeGlobal slice.
func GlobalNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(declRegistry))
	for n, d := range declRegistry {
		if d.scope == ScopeGlobal {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func resetForTest() {
	regMu.Lock()
	declRegistry = map[string]*decl{}
	for _, n := range RouteNames() {
		declRegistry[n] = &decl{name: n, scope: ScopePage, def: ""}
	}
	regMu.Unlock()
}
