package entity

// Registry is the minimal contract subpackages need from the framework's
// entity registry: enumerate every registered entity.
//
// All() returns the entities keyed by name. Go's map iteration is
// randomised, so callers that emit order-sensitive output (OpenAPI
// tags, LLM markdown, generated code) must use AllSorted() to keep
// output stable across runs. Callers that only care about presence
// (counts, hash lookups, contains-checks) can use All() directly.
//
// The concrete *framework.Registry type satisfies this implicitly.
// Splitting it out here lets framework/migrate, framework/dsl, and
// others depend on the entity model without pulling in the full
// framework package.
type Registry interface {
	// All returns a snapshot of every registered entity keyed by name.
	// Map iteration order is randomised by Go; for stable iteration use
	// AllSorted().
	All() map[string]*Entity

	// AllSorted returns every registered entity in alphabetical order
	// by name. Use this when emitting bytes whose ordering matters
	// (OpenAPI, generated code, golden-file tests, ETag-cached
	// responses).
	AllSorted() []*Entity

	// Get retrieves one entity by name, or an error when no such entity
	// is registered.
	Get(name string) (*Entity, error)
}
