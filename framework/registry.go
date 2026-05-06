package framework

import (
	"database/sql"
	"fmt"
	"sync"
)

// Registry stores and retrieves Entity definitions by name.
// It is safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	entities map[string]*Entity
	db       *sql.DB
}

// NewRegistry creates a new empty entity registry.
func NewRegistry() *Registry {
	return &Registry{
		entities: make(map[string]*Entity),
	}
}

// Register adds an Entity to the registry.
// Returns an error if an entity with the same name already exists.
func (r *Registry) Register(entity *Entity) error {
	if entity == nil {
		return fmt.Errorf("registry: entity must not be nil")
	}
	if entity.Config.Name == "" {
		return fmt.Errorf("registry: entity name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entities[entity.Config.Name]; exists {
		return fmt.Errorf("registry: entity %q already registered", entity.Config.Name)
	}

	// Propagate registry-level DB if the entity doesn't have one
	if entity.DB == nil && r.db != nil {
		entity.DB = r.db
	}

	r.entities[entity.Config.Name] = entity
	return nil
}

// Get retrieves an Entity by name.
// Returns an error if no entity with that name is registered.
func (r *Registry) Get(name string) (*Entity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.entities[name]
	if !ok {
		return nil, fmt.Errorf("registry: entity %q not found", name)
	}
	return e, nil
}

// All returns a copy of the map of all registered entities.
func (r *Registry) All() map[string]*Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]*Entity, len(r.entities))
	for k, v := range r.entities {
		out[k] = v
	}
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
