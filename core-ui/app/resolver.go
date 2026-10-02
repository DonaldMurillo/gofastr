package app

// route resolvers, the "anything may
// be configured from what the route resolved" primitive
// (docs/DESIGN-layout-outlets.md, "Route resolution"). A screen or a
// screen group declares a resolver for a typed Key; guards, the
// screen's Load, fills and layout builds read the value with Key.Get.
// One cell per key per request: the first reader runs it, concurrent
// readers wait, and the value or the error is kept for the rest of the
// render.
//
// WHERE an error lands depends on the phase that first read it. The
// render pipeline stamps the current phase on the context before each
// stage (policy, Load, fills/areas/builds); a cell records the phase
// of its first read and the outcome follows it:
//
//   - policy phase (a guard), the screen's Load, or an eager run
//     (group.Requires): a WHOLE-PAGE outcome — ErrNotFound renders the
//     not-found page (404), anything else the error page (500, through
//     the root layout, no error text in the body, the error logged);
//   - first read inside a fill, an area, or a layout build: contained
//     to that region and logged, exactly like a failing fill.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ErrNotFound is the resolver error that renders the not-found page
// (status 404) instead of the error page: a resolver that looked up
// what the route named and the store does not hold it. Compare with
// errors.Is.
var ErrNotFound = errors.New("app: not found")

// ErrNoResolver is returned by Key.Get when the context carries no
// resolver declaration for that key: the screen, and every group it
// belongs to, declared none.
var ErrNoResolver = errors.New("app: no such resolver")

// resolverFn is the untyped resolver body a Resolver carries.
type resolverFn func(ctx context.Context) (any, error)

// resolverID is a key's identity. Keys compare by this pointer, never
// by name, so two packages that both name a key "project" never share
// a cell or a declaration.
type resolverID struct{ name string }

// Key names a route resolver and the type it resolves to:
//
//	var ProjectKey = app.NewKey[Project]("project")
//
//	group.Resolve(ProjectKey.From(loadProject))
//	group.Requires(ProjectKey)
//	p, err := ProjectKey.Get(ctx)
//
// The type travels with the key because a Go method cannot take its
// own type parameter; the name is only for messages. Declare keys as
// package vars and share the var: a second NewKey with the same name
// is a different key.
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

// Resolver is one resolver declaration, built by Key.From and passed
// to Screen.Resolve or ScreenGroup.Resolve.
type Resolver struct {
	id *resolverID
	fn resolverFn
}

// From declares fn as the resolver for k. The value fn returns is what
// k.Get reads for the rest of the request.
func (k *Key[T]) From(fn func(ctx context.Context) (T, error)) Resolver {
	return Resolver{id: k.id, fn: func(ctx context.Context) (any, error) {
		v, err := fn(ctx)
		if err != nil {
			return nil, err
		}
		return v, nil
	}}
}

// resolverDecl is one resolver a screen or group declares.
type resolverDecl struct {
	id    *resolverID
	fn    resolverFn
	eager bool // group.Requires: run in the policy phase, unconditionally
}

// resolvePhase names the pipeline stage a resolver was first read in.
type resolvePhase int

const (
	// resolvePhasePolicy: guards, eager runs. Whole-page outcome.
	resolvePhasePolicy resolvePhase = iota
	// resolvePhaseLoad: the screen's own Load. Whole-page outcome.
	resolvePhaseLoad
	// resolvePhaseRegion: fills, route areas, layout builds. Contained
	// to the region.
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

// resolverCell is one key's per-request memo: the first reader runs
// the body (recovering a panic into an error), every later reader —
// concurrent or not — waits on the done channel and gets the same
// value or error. Fills load concurrently under the spike's variant C,
// so a plain check-then-run map would race; the channel close is the
// happens-before edge for val/err/phase.
type resolverCell struct {
	mu     sync.Mutex
	done   chan struct{}
	val    any
	err    error
	phase  resolvePhase
	failed bool
}

// resolve runs (or waits for) the cell's body and returns its outcome.
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

// run executes the resolver body with panic-to-error containment: a
// panicking resolver is the same channel a returning error takes, and
// the panicked-on value is scrubbed before it reaches a log line.
func (c *resolverCell) run(ctx context.Context, fn resolverFn) (val any, err error) {
	defer func() {
		if r := recover(); r != nil {
			val, err = nil, errors.New("resolver panicked: "+textsafe.Recovered(r))
		}
	}()
	return fn(ctx)
}

// resolverState is the per-request resolver carrier: the screen whose
// declaration set is visible, and the phase the pipeline is currently
// in. The store is shared by every context derived from the one it was
// installed on; the phase travels with the context, so a fill goroutine
// started in the region phase stamps region even though the parent
// context moves on.
type resolverState struct {
	screen *Screen
	store  *resolverStore
	phase  resolvePhase
}

// resolverStore holds the cells of one request.
type resolverStore struct {
	mu    sync.Mutex
	cells map[*resolverID]*resolverCell
	order []*resolverID // first-read order, for deterministic error scans

	// guards holds the request's region-guard decisions, computed
	// ONCE in the policy phase (before any fill goroutine exists, so
	// the unlocked read below it is race-free) and read by every
	// later stage of the same request — the base render and the
	// subtree partial's fills pass agree on one set of decisions.
	guards        regionGuards
	guardRedirect Decision
	guardsDone    bool
}

// cellFor returns (creating) the cell for id.
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

// resolverDecls lists the resolvers visible to screen: the screen's
// own first, then the group chain innermost → outermost — the same
// order fills consult. A key declared twice resolves to the FIRST
// declaration (the screen's own beats a group's).
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

// resolverContextKey carries the resolverState.
type resolverContextKey struct{}

// withResolverState installs a FRESH per-request state for screen (or
// reuses the one already present: renderPartialFrom calls through two
// render bodies that would otherwise build two stores and run a
// resolver twice — "one cell per key per request" is per REQUEST, not
// per render body). The phase starts at policy; the pipeline advances
// it with withResolvePhase.
func withResolverState(ctx context.Context, screen *Screen) context.Context {
	if st, ok := ctx.Value(resolverContextKey{}).(*resolverState); ok {
		if st.screen == nil && screen != nil {
			// A state installed before the screen was known
			// (renderPartialFrom) gets it filled in, keeping the ONE
			// store the request's cells share.
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

// withResolvePhase returns a context whose resolver reads stamp phase
// as their first-read phase. The store is shared with the parent.
func withResolvePhase(ctx context.Context, phase resolvePhase) context.Context {
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return ctx
	}
	return context.WithValue(ctx, resolverContextKey{}, &resolverState{screen: st.screen, store: st.store, phase: phase})
}

// runEagerResolvers executes every eager declaration (group.Requires,
// expanded against the screen's visible set) in declaration order, in
// the policy phase. Errors are recorded in the cells; the caller reads
// them with resolverPageError.
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

// ResolverError is the error Key.Get returns (and a failing fill or
// Load propagates): the resolver key's name, its error, and the phase that
// first read it. The phase decides the outcome — whole-page for policy
// and Load reads, contained to the region otherwise.
type ResolverError struct {
	Name  string
	Phase resolvePhase
	Err   error
}

func (e *ResolverError) Error() string {
	return fmt.Sprintf("app: resolver %q failed in the %s phase: %v", e.Name, e.Phase, e.Err)
}

func (e *ResolverError) Unwrap() error { return e.Err }

// contained reports whether a first read in this phase stays inside the
// region that read it.
func (e *ResolverError) contained() bool { return e.Phase == resolvePhaseRegion }

// resolverPageError scans the request's cells for the first (in
// declaration order) error whose first read was a whole-page phase,
// and wraps it as a *PageError: ErrNotFound renders the not-found page,
// anything else the error page. Nil when no whole-page resolver failed.
// The name ordering keeps the outcome deterministic under variant C's
// concurrent fills.
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

// PageError is the whole-page outcome a resolver failure produces (see
// renderScreenPage): NotFound renders the not-found page with status
// 404; otherwise the host's error page with status 500 — through the
// root layout, no error text in the body, the error logged here once.
type PageError struct {
	Path     string
	Err      error
	NotFound bool
}

func (e *PageError) Error() string {
	kind := "error"
	if e.NotFound {
		kind = "not-found"
	}
	return fmt.Sprintf("app: page %s outcome for %q: %v", kind, e.Path, e.Err)
}

func (e *PageError) Unwrap() error { return e.Err }

// Resolve declares a resolver on the screen. The value is readable
// anywhere in the render with the key's Get; the screen's own
// declaration beats a group's for the same key.
func (s *Screen) Resolve(r Resolver) *Screen {
	s.resolvers = append(s.resolvers, resolverDecl{id: r.id, fn: r.fn})
	return s
}

// Resolve declares a resolver on the group, visible to every member
// screen (and sub-group) that does not declare its own for the same
// key.
func (g *ScreenGroup) Resolve(r Resolver) *ScreenGroup {
	g.resolvers = append(g.resolvers, resolverDecl{id: r.id, fn: r.fn})
	return g
}

// Requires marks a resolver EAGER: it runs in the policy phase, before
// any Load, whether or not anything would have read it. The key must
// have a declaration visible to every member screen — this group's
// own, or the screen's (declare on the group when only some members
// read the value; the others still run it eagerly, inertly). A key
// with no declaration is a silently dropped policy check, so
// ValidateRequires panics at mount naming the group and the screen,
// and the render paths re-check for a screen registered after mount.
func (g *ScreenGroup) Requires(k AnyKey) *ScreenGroup {
	g.requires = append(g.requires, k)
	return g
}

// Get reads the key's resolver from the context: the first reader
// runs the body, concurrent readers wait, and the value or error is
// kept for the request. The error is a *ResolverError whose phase
// decides what a failure does to the page (see this file's header).
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
		// From only stores a T, so the one miss is a nil interface or
		// pointer the resolver returned: that is T's zero value.
		return zero, nil
	}
	return v, nil
}

// validateRequires is the render-time half of Router.ValidateRequires:
// every key the screen's group chain Requires must have a resolver
// declaration visible to that screen (its own or a group's in its
// chain — the same set resolverDecls builds). A key nothing declares
// is a silently dropped policy check — Requires(IssueKey) on a group
// whose issue resolver sits on a sibling group compiles, boots, and
// never runs the check it meant to add — so it panics here, naming the
// group and the screen, in the voice of fill.go's fill validation. Nil
// screen is a no-op, matching validateFills.
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

// eagerDeclsFor expands the group chain's Requires keys against the
// declaration set visible to screen. validateRequires has run by the
// time this is reached (at mount, and at every render entry point), so
// every Requires key provably has a declaration: the unmatched key
// this loop would otherwise skip is exactly the dropped policy check
// the validation refuses.
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
