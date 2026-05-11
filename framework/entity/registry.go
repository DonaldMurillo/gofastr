package entity

// Registry is the minimal contract subpackages need from the framework's
// entity registry: enumerate every registered entity in stable order.
//
// The concrete *framework.Registry type satisfies this implicitly. Splitting
// it out here lets framework/migrate, framework/dsl, and others depend on
// the entity model without pulling in the full framework package.
type Registry interface {
	All() map[string]*Entity
	Get(name string) (*Entity, error)
}
