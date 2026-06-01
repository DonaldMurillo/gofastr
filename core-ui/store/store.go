// Package store is a typed, server-declared shared-state primitive for
// the GoFastr UI. A Store groups typed Slices; each Slice is one named
// reactive value that:
//
//   - seeds its initial value into the client signal bus at SSR (so
//     getSignal returns the server value on first paint, not undefined),
//   - emits read-only binding attributes for presentational consumers,
//   - and (Phase 2) connects an island/RPC producer's updates so they
//     fan out to every consumer client-side, with no server re-render.
//
// The design is two-layer: the *declaration* (name → type, default,
// scope) is process-global; the *value* is request-scoped (a producer
// calls Slice.Seed(ctx, v) at render time). The framework resolves the
// seed for a page by scanning the rendered HTML for referenced names
// (ScanReferenced) plus all app-global slices, then emitting one inert
// <script type="application/json" id="gofastr-signals"> block.
package store

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Scope controls when a slice's value is seeded into a page.
type Scope int

const (
	// ScopePage seeds only on pages whose HTML references the slice.
	ScopePage Scope = iota
	// ScopeGlobal seeds on every page and survives client-side
	// navigation (cart count, signed-in user, theme).
	ScopeGlobal
)

// Store namespaces a group of slices. New("org").String("companyName",…)
// declares the slice "org.companyName".
type Store struct{ ns string }

// New returns a Store whose slices are prefixed with namespace (a "."
// separator is inserted; an empty namespace yields unprefixed names).
func New(namespace string) *Store { return &Store{ns: namespace} }

// String declares a string-typed slice.
func (s *Store) String(name, def string) *Slice[string] { return declare(s, name, def) }

// Int declares an int-typed slice.
func (s *Store) Int(name string, def int) *Slice[int] { return declare(s, name, def) }

// Bool declares a bool-typed slice.
func (s *Store) Bool(name string, def bool) *Slice[bool] { return declare(s, name, def) }

// JSON declares a slice of an arbitrary JSON-serializable type. It is a
// free function because Go methods cannot introduce their own type
// parameters.
func JSON[T any](s *Store, name string, def T) *Slice[T] { return declare(s, name, def) }

// Computed declares a derived slice computed client-side: when any of the
// named dependency signals changes, the runtime runs the JS reducer
// registered under reducer (window.__gofastr._reducers[reducer]) over the
// current dep values and broadcasts the result to this slice's consumers.
// The reducer is a host-provided JS function (CSP-safe — no eval). The
// computed value is derived, so it is never part of the SSR seed.
//
//	var Greeting = store.Computed[string](Org, "greeting", "greet", "org.companyName")
func Computed[T any](s *Store, name, reducer string, deps ...string) *Slice[T] {
	full := name
	if s.ns != "" {
		full = s.ns + "." + name
	}
	validateName(full)
	validateName(reducer)
	registerComputed(full)
	var zero T
	return &Slice[T]{name: full, scope: ScopePage, def: zero, comp: &computedCfg{reducer: reducer, deps: deps}}
}

// ─── declaration registry (process-global) ──────────────────────────

type decl struct {
	name     string
	scope    Scope
	def      any
	computed bool // derived client-side; excluded from the seed
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

// register records a declaration. Identical re-declaration is idempotent
// (shared package-level decls, test re-runs); a conflicting default
// panics — two producers must not claim one name with different values.
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

func registerComputed(name string) {
	regMu.Lock()
	defer regMu.Unlock()
	if d, ok := declRegistry[name]; ok {
		if !d.computed {
			panic(fmt.Sprintf("store: %q is already a value slice; cannot redeclare as computed", name))
		}
		return
	}
	declRegistry[name] = &decl{name: name, scope: ScopePage, def: nil, computed: true}
}

func setScope(name string, scope Scope) {
	regMu.Lock()
	defer regMu.Unlock()
	if d, ok := declRegistry[name]; ok {
		d.scope = scope
	}
}

// validateName rejects names that could break out of a data-fui-* HTML
// attribute or the signal key. Allowed: letters, digits, '.', '_', '-'.
func validateName(name string) {
	if name == "" {
		panic("store: slice name must not be empty")
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			panic(fmt.Sprintf("store: invalid slice name %q (allowed: letters, digits, '.', '_', '-')", name))
		}
	}
}

// GlobalNames returns the sorted names of all ScopeGlobal slices. The
// host seeds these on every page regardless of whether the page
// references them.
func GlobalNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0)
	for n, d := range declRegistry {
		if d.scope == ScopeGlobal {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// resetForTest clears the declaration registry. Test-only.
func resetForTest() {
	regMu.Lock()
	declRegistry = map[string]*decl{}
	regMu.Unlock()
}
