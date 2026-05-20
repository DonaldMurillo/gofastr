// Package factory provides Rails-style fixture / factory helpers for
// GoFastr tests and dev-time seeders. Each Factory binds to one
// entity's CRUD handler and produces fresh row bodies — typically by
// layering caller overrides on top of a base function — so test setup
// reads "make me a user with admin=true" instead of "construct a map
// with every required field by hand."
//
// The package leans on the in-process [crud.CrudHandler.CreateOne]
// API so factories run through the exact same Before/After hook
// chain, transaction, and event emission as real HTTP traffic. That
// keeps integration-style tests close to production behaviour.
//
// Wiring:
//
//	users := framework.NewApp(...).Entity("users", ...) // registers CRUD
//	userFactory := factory.New(app.Registry, "users", func() map[string]any {
//	    n := userSeq.Next()
//	    return map[string]any{
//	        "email": fmt.Sprintf("user%d@example.com", n),
//	        "name":  fmt.Sprintf("User %d", n),
//	    }
//	})
//
//	u, _ := userFactory.Create(ctx)
//	admin, _ := userFactory.Create(ctx, map[string]any{"role": "admin"})
package factory

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EntityRegistry is the minimal surface the factory needs from the
// framework's registry — a way to look up an entity by name. The
// factory accepts the interface so tests can supply a hand-rolled
// registry; in production this is the App.Registry.
type EntityRegistry interface {
	Get(name string) (*entity.Entity, error)
}

// BaseFunc returns a fresh body map. Called for each Create / Build
// — returning a new map each time keeps tests independent of each
// other.
type BaseFunc func() map[string]any

// Factory builds and persists rows for one entity. Construct via [New].
//
// The handler defaults to the entity's bundled CRUD handler. Override
// it via [WithHandler] when your tests need a different DB connection
// (sandboxed schema, in-memory fork, …).
type Factory struct {
	entityName string
	base       BaseFunc
	handler    *crud.CrudHandler
}

// Option configures a Factory.
type Option func(*Factory)

// WithHandler overrides the CRUD handler the factory dispatches
// through. Useful when tests build their own handler with a custom
// DB connection.
func WithHandler(h *crud.CrudHandler) Option {
	return func(f *Factory) { f.handler = h }
}

// New constructs a Factory bound to the named entity in the registry.
// The base function MUST return a fresh map per call — sharing a map
// would let later overrides leak into earlier factory results.
//
// Returns an error when the entity is unknown or has no DB attached.
func New(reg EntityRegistry, entityName string, base BaseFunc, opts ...Option) (*Factory, error) {
	if base == nil {
		return nil, errors.New("factory: nil base func")
	}
	if reg == nil {
		return nil, errors.New("factory: nil registry")
	}
	e, err := reg.Get(entityName)
	if err != nil {
		return nil, fmt.Errorf("factory: entity %q: %w", entityName, err)
	}
	if e.DB == nil {
		return nil, fmt.Errorf("factory: entity %q has no DB attached", entityName)
	}
	f := &Factory{
		entityName: entityName,
		base:       base,
		handler:    crud.NewCrudHandler(e, e.DB),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f, nil
}

// Build returns the row body that Create would insert — useful when
// a test wants to assert on the request shape without actually
// hitting the database. Overrides apply left-to-right; later
// overrides win on key conflict.
func (f *Factory) Build(overrides ...map[string]any) map[string]any {
	out := f.base()
	if out == nil {
		out = map[string]any{}
	}
	for _, o := range overrides {
		for k, v := range o {
			out[k] = v
		}
	}
	return out
}

// Create inserts a fresh row through the full CRUD pipeline (Before
// hooks → INSERT → After hooks → event emission) and returns the
// persisted record as a snake_cased map.
func (f *Factory) Create(ctx context.Context, overrides ...map[string]any) (map[string]any, error) {
	body := f.Build(overrides...)
	return f.handler.CreateOne(ctx, body)
}

// CreateMany inserts n rows. The optional perIndex callback runs
// once per row and its return is merged on top of base + caller
// overrides — handy when each row needs a unique value derived from
// its index (e.g. ordering tests).
func (f *Factory) CreateMany(ctx context.Context, n int, perIndex func(i int) map[string]any) ([]map[string]any, error) {
	if n <= 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		var ix map[string]any
		if perIndex != nil {
			ix = perIndex(i)
		}
		row, err := f.Create(ctx, ix)
		if err != nil {
			return out, fmt.Errorf("factory: create #%d of %d: %w", i+1, n, err)
		}
		out = append(out, row)
	}
	return out, nil
}

// ----- registry -------------------------------------------------------------

// Registry is the optional collection of named factories shared
// across a test suite. Use [Suite] as the entry point in tests.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]*Factory
}

// NewRegistry returns an empty factory registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]*Factory{}}
}

// Register adds (or replaces) a factory under the supplied name. Use
// the entity name as the registry key by convention.
func (r *Registry) Register(name string, f *Factory) *Registry {
	r.mu.Lock()
	r.factories[name] = f
	r.mu.Unlock()
	return r
}

// MustGet returns the factory or panics. Tests are the only
// real caller; production code should use [Registry.Get] instead.
func (r *Registry) MustGet(name string) *Factory {
	f, err := r.Get(name)
	if err != nil {
		panic(err)
	}
	return f
}

// Get returns the factory or an error when none is registered.
func (r *Registry) Get(name string) (*Factory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("factory: no factory registered for %q", name)
	}
	return f, nil
}

// Create looks up the named factory and creates one row. Convenience
// wrapper for tests that don't keep a local Factory reference.
func (r *Registry) Create(ctx context.Context, name string, overrides ...map[string]any) (map[string]any, error) {
	f, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return f.Create(ctx, overrides...)
}

// ----- Sequence -------------------------------------------------------------

// Sequence is a tiny atomic counter for unique base values inside
// factory BaseFuncs. Concurrent-test-safe: Next never returns the
// same integer twice within a process lifetime.
type Sequence struct {
	n atomic.Int64
}

// Next returns the next integer.
func (s *Sequence) Next() int64 {
	return s.n.Add(1)
}

// NextString returns "prefix" + Next() as a string. Useful for
// per-row unique fields like email addresses.
func (s *Sequence) NextString(prefix string) string {
	return prefix + strconv.FormatInt(s.Next(), 10)
}
