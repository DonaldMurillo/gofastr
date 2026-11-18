// Package factory builds and inserts entity rows for tests and seeders.
package factory

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// EntityRegistry looks up an entity by name; App.Registry satisfies it.
type EntityRegistry interface {
	Get(name string) (*entity.Entity, error)
}

type wiredRegistry interface {
	CrudHandlerFor(*entity.Entity) (*crud.CrudHandler, error)
}

// BaseFunc returns the base body, called once per Build or Create.
type BaseFunc func() map[string]any

// Factory builds and inserts rows for one entity. Construct it with [New].
type Factory struct {
	entityName string
	base       BaseFunc
	handler    *crud.CrudHandler
}

// Option configures a Factory.
type Option func(*Factory)

// WithHandler replaces the CRUD handler the factory inserts through.
func WithHandler(h *crud.CrudHandler) Option {
	return func(f *Factory) { f.handler = h }
}

// New binds a Factory to the named entity; base must return a fresh map
// per call. It errors when the entity is unknown or has no DB.
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
	h := crud.NewCrudHandler(e, e.DB)
	if w, ok := reg.(wiredRegistry); ok {
		wired, err := w.CrudHandlerFor(e)
		if err != nil {
			return nil, fmt.Errorf("factory: entity %q: %w", entityName, err)
		}
		if wired != nil {
			h = wired
		}
	}
	f := &Factory{
		entityName: entityName,
		base:       base,
		handler:    h,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f, nil
}

// Build returns the body Create would insert; later overrides win.
func (f *Factory) Build(overrides ...map[string]any) map[string]any {
	out := f.base()
	if out == nil {
		out = map[string]any{}
	}
	for _, o := range overrides {
		maps.Copy(out, o)
	}
	return out
}

// Create inserts a row through the handler and returns it as the API would.
func (f *Factory) Create(ctx context.Context, overrides ...map[string]any) (map[string]any, error) {
	body := f.Build(overrides...)
	return f.handler.CreateOne(ctx, body)
}

// CreateMany inserts n rows, merging perIndex(i), when given, over each.
func (f *Factory) CreateMany(ctx context.Context, n int, perIndex func(i int) map[string]any) ([]map[string]any, error) {
	if n <= 0 {
		return nil, nil
	}
	out := make([]map[string]any, 0, n)
	for i := range n {
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

// Registry holds named factories shared across a test suite.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]*Factory
}

// NewRegistry returns an empty factory registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]*Factory{}}
}

// Register adds or replaces the factory under name.
func (r *Registry) Register(name string, f *Factory) *Registry {
	r.mu.Lock()
	r.factories[name] = f
	r.mu.Unlock()
	return r
}

// MustGet returns the named factory or panics.
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

// Create creates one row with the named factory.
func (r *Registry) Create(ctx context.Context, name string, overrides ...map[string]any) (map[string]any, error) {
	f, err := r.Get(name)
	if err != nil {
		return nil, err
	}
	return f.Create(ctx, overrides...)
}

// Sequence is a counter for unique base values, safe for concurrent use.
type Sequence struct {
	n atomic.Int64
}

// Next returns the next integer.
func (s *Sequence) Next() int64 {
	return s.n.Add(1)
}

// NextString returns prefix followed by Next().
func (s *Sequence) NextString(prefix string) string {
	return prefix + strconv.FormatInt(s.Next(), 10)
}
