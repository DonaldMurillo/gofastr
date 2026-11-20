package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ErrNotFound is the resolver error that renders the not-found page (status 404) instead of the
// error page: a resolver that looked up what the route named and the store does not hold it.
var ErrNotFound = errors.New("app: not found")

// ErrNoResolver is returned by Key.Get when the context carries no resolver declaration for that
// key: the screen, and every group it belongs to, declared none.
var ErrNoResolver = errors.New("app: no such resolver")

type resolverFn func(ctx context.Context) (any, error)

type resolverID struct{ name string }

// Key names a route resolver and the type it resolves to.
type Key[T any] struct{ id *resolverID }

// NewKey returns a new resolver key for values of type T.
func NewKey[T any](name string) *Key[T] {
	return &Key[T]{id: &resolverID{name: name}}
}

// Name returns the name the key was created with.
func (k *Key[T]) Name() string { return k.id.name }

func (k *Key[T]) resolverID() *resolverID { return k.id }

// AnyKey is any *Key[T]: what ScreenGroup.Requires takes.
type AnyKey interface {
	Name() string
	resolverID() *resolverID
}

// Resolver is one resolver declaration, built by Key.From and passed to Screen.Resolve or
// ScreenGroup.Resolve.
type Resolver struct {
	id *resolverID
	fn resolverFn
}

// From declares fn as the resolver for k.
func (k *Key[T]) From(fn func(ctx context.Context) (T, error)) Resolver {
	return Resolver{id: k.id, fn: func(ctx context.Context) (any, error) {
		v, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return v, nil
	}}
}

type resolverDecl struct {
	id    *resolverID
	fn    resolverFn
	eager bool // group.Requires: run in the policy phase, unconditionally
}

type resolvePhase int

const (
	resolvePhasePolicy resolvePhase = iota
	resolvePhaseLoad
	resolvePhaseRegion
)

func (p resolvePhase) String() string {
	switch p {
	case resolvePhasePolicy:
		return "policy"
	case resolvePhaseLoad:
		return "load"
	default:
		return "region"
	}
}

type resolverCell struct {
	mu     sync.Mutex
	done   chan struct{}
	val    any
	err    error
	phase  resolvePhase
	failed bool
}

func (c *resolverCell) resolve(ctx context.Context, fn resolverFn, phase resolvePhase) (any, error) {
	c.mu.Lock()
	if c.done != nil {
		ch := c.done
		c.mu.Unlock()
		<-ch
		return c.val, c.err
	}
	c.phase = phase
	c.done = make(chan struct{})
	c.mu.Unlock()

	val, err := c.run(ctx, fn)

	c.mu.Lock()
	c.val, c.err = val, err
	c.failed = err != nil
	c.mu.Unlock()
	close(c.done)
	return val, err
}

func (c *resolverCell) run(ctx context.Context, fn resolverFn) (val any, err error) {
	defer func() {
		if r := recover(); r != nil {
			val, err = nil, errors.New("resolver panicked: "+textsafe.Recovered(r))
		}
	}()
	return fn(ctx)
}

type resolverState struct {
	screen *Screen
	store  *resolverStore
	phase  resolvePhase
}

type resolverStore struct {
	mu    sync.Mutex
	cells map[*resolverID]*resolverCell
	order []*resolverID // first-read order, for deterministic error scans

	guards        regionGuards
	guardRedirect Decision
	guardsDone    bool
}

func (s *resolverStore) cellFor(id *resolverID) *resolverCell {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cells[id]; ok {
		return c
	}
	c := &resolverCell{}
	if s.cells == nil {
		s.cells = make(map[*resolverID]*resolverCell)
	}
	s.cells[id] = c
	s.order = append(s.order, id)
	return c
}

func resolverDecls(screen *Screen) []resolverDecl {
	if screen == nil {
		return nil
	}
	var out []resolverDecl
	seen := map[*resolverID]bool{}
	add := func(ds []resolverDecl) {
		for _, d := range ds {
			if seen[d.id] {
				continue
			}
			seen[d.id] = true
			out = append(out, d)
		}
	}
	add(screen.resolvers)
	for g := screen.group; g != nil; g = g.parent {
		add(g.resolvers)
	}
	return out
}

type resolverContextKey struct{}

func withResolverState(ctx context.Context, screen *Screen) context.Context {
	if st, ok := ctx.Value(resolverContextKey{}).(*resolverState); ok {
		if st.screen == nil && screen != nil {
			return context.WithValue(ctx, resolverContextKey{}, &resolverState{screen: screen, store: st.store, phase: st.phase})
		}
		return ctx
	}
	return context.WithValue(ctx, resolverContextKey{}, &resolverState{
		screen: screen,
		store:  &resolverStore{},
		phase:  resolvePhasePolicy,
	})
}

func withResolvePhase(ctx context.Context, phase resolvePhase) context.Context {
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, resolverContextKey{}, &resolverState{screen: st.screen, store: st.store, phase: phase})
}

func runEagerResolvers(ctx context.Context, screen *Screen) {
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return
	}
	for _, d := range eagerDeclsFor(screen) {
		cell := st.store.cellFor(d.id)
		if cell.done != nil {
			continue // a guard read it before the eager pass
		}
		cell.resolve(ctx, d.fn, st.phase)
	}
}

// ResolverError is the error Key.Get returns (and a failing fill or Load propagates): the resolver
// key's name, its error, and the phase that first read it.
type ResolverError struct {
	Name  string
	Phase resolvePhase
	Err   error
}

func (e *ResolverError) Error() string {
	return fmt.Sprintf("app: resolver %q failed in the %s phase: %v", e.Name, e.Phase, e.Err)
}

func (e *ResolverError) Unwrap() error { return e.Err }

func (e *ResolverError) contained() bool { return e.Phase == resolvePhaseRegion }

func resolverPageError(ctx context.Context, path string) error {
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return nil
	}
	st.store.mu.Lock()
	defer st.store.mu.Unlock()
	for _, id := range st.store.order {
		c := st.store.cells[id]
		if c == nil {
			continue
		}
		c.mu.Lock()
		failed, phase, err := c.failed, c.phase, c.err
		c.mu.Unlock()
		if !failed || phase == resolvePhaseRegion {
			continue
		}
		return &PageError{
			Path:     path,
			Err:      &ResolverError{Name: id.name, Phase: phase, Err: err},
			NotFound: errors.Is(err, ErrNotFound),
		}
	}
	return nil
}

// PageError is the whole-page outcome of a resolver failure: not-found (404) or the error page (500).
type PageError struct {
	Path     string
	Err      error
	NotFound bool
	// Policy marks the not-found outcome a policy chose with decide.NotFound, as opposed to a resolver
	// that failed.
	Policy bool
}

func (e *PageError) Error() string {
	kind := "error"
	if e.NotFound {
		kind = "not-found"
	}
	return fmt.Sprintf("app: page %s outcome for %q: %v", kind, e.Path, e.Err)
}

func (e *PageError) Unwrap() error { return e.Err }

// Resolve declares a resolver on the screen.
func (s *Screen) Resolve(r Resolver) *Screen {
	s.resolvers = append(s.resolvers, resolverDecl{id: r.id, fn: r.fn})
	return s
}

// Resolve declares a resolver on the group, visible to every member screen (and sub-group) that
// does not declare its own for the same key.
func (g *ScreenGroup) Resolve(r Resolver) *ScreenGroup {
	g.resolvers = append(g.resolvers, resolverDecl{id: r.id, fn: r.fn})
	return g
}

// Requires marks a resolver EAGER: it runs in the policy phase, before any Load, whether or not
// anything would have read it.
func (g *ScreenGroup) Requires(k AnyKey) *ScreenGroup {
	g.requires = append(g.requires, k)
	return g
}

// Get reads the key's resolver from the context: the first reader runs the body, concurrent readers
// wait, and the value or error is kept for the request.
func (k *Key[T]) Get(ctx context.Context) (T, error) {
	var zero T
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return zero, fmt.Errorf("app: %q read outside a render: %w", k.id.name, ErrNoResolver)
	}
	var decl resolverDecl
	found := false
	for _, d := range resolverDecls(st.screen) {
		if d.id == k.id {
			decl, found = d, true
			break
		}
	}
	if !found {
		return zero, fmt.Errorf("%w: %q", ErrNoResolver, k.id.name)
	}
	cell := st.store.cellFor(k.id)
	val, err := cell.resolve(ctx, decl.fn, st.phase)
	if err != nil {
		return zero, &ResolverError{Name: k.id.name, Phase: cell.phase, Err: err}
	}
	v, ok := val.(T)
	if !ok {
		return zero, nil
	}
	return v, nil
}

func validateRequires(screen *Screen) {
	if screen == nil {
		return
	}
	eager := false
	for g := screen.group; g != nil; g = g.parent {
		if len(g.requires) > 0 {
			eager = true
			break
		}
	}
	if !eager {
		return // no group requires anything: skip the declaration walk
	}
	decls := resolverDecls(screen)
	for g := screen.group; g != nil; g = g.parent {
		for _, k := range g.requires {
			id := k.resolverID()
			if !slices.ContainsFunc(decls, func(d resolverDecl) bool { return d.id == id }) {
				panic(fmt.Sprintf("app: group %q requires resolver %q, but no resolver for that key is visible to %q", g.prefix, id.name, screen.Path))
			}
		}
	}
}

func eagerDeclsFor(screen *Screen) []resolverDecl {
	validateRequires(screen)
	var req []*resolverID
	for g := screen.group; g != nil; g = g.parent {
		for _, k := range g.requires {
			req = append(req, k.resolverID())
		}
	}
	if len(req) == 0 {
		return nil
	}
	var out []resolverDecl
	for _, d := range resolverDecls(screen) {
		if slices.Contains(req, d.id) {
			d.eager = true
			out = append(out, d)
		}
	}
	return out
}
