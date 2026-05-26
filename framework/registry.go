package framework

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Registry stores and retrieves Entity definitions by name.
// It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	entities map[string]*entity.Entity
	db       *sql.DB
}

// NewRegistry creates a new empty entity registry.
func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[string]*entity.Entity),
	}
}

// Register adds an Entity to the registry.
// Returns an error if an entity with the same name already exists.
func (r *Registry) Register(ent *entity.Entity) error {
	if ent == nil {
		return fmt.Errorf("registry: entity must not be nil")
	}
	if ent.Config.Name == "" {
		return fmt.Errorf("registry: entity name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[ent.Config.Name]; exists {
		return fmt.Errorf("registry: entity %q already registered", ent.Config.Name)
	}

	// Propagate registry-level DB if the entity doesn't have one
	if ent.DB == nil && r.db != nil {
		ent.DB = r.db
	}

	r.entities[ent.Config.Name] = ent
	return nil
}

// Get retrieves an Entity by name.
// Returns an error if no entity with that name is registered.
func (r *Registry) Get(name string) (*entity.Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entities[name]
	if !ok {
		return nil, fmt.Errorf("registry: entity %q not found", name)
	}
	return e, nil
}

// All returns a copy of the map of all registered entities. Map
// iteration order is randomised by Go; use AllSorted() for stable
// iteration in code paths that emit order-sensitive output.
func (r *Registry) All() map[string]*entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*entity.Entity, len(r.entities))
	for k, v := range r.entities {
		out[k] = v
	}
	return out
}

// AllSorted returns every registered entity in alphabetical order by
// name. Use this whenever the iteration order affects bytes-on-the-wire
// (OpenAPI tag emission, LLM-markdown, codegen output) so the same
// registry produces the same artefact across restarts.
func (r *Registry) AllSorted() []*entity.Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*entity.Entity, 0, len(r.entities))
	for _, e := range r.entities {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Config.Name < out[j].Config.Name
	})
	return out
}

// SetDB sets the database connection on the registry and propagates it
// to all registered entities.
func (r *Registry) SetDB(db *sql.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.db = db
	for _, e := range r.entities {
		e.DB = db
	}
}
