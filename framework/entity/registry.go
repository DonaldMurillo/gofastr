package entity

import "fmt"

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
	// is registered. When multiple versions of the name exist and none is
	// unversioned, Get returns an ambiguity error.
	//
	// Get is NOT safe for resolving a relation target: it prefers the
	// unversioned entity, so a v1 relation would resolve an unversioned
	// declaration with different Hidden columns and different scopes. Use
	// ResolveTarget for anything that drives visibility or scoping.
	Get(name string) (*Entity, error)
}

// VersionedRegistry is the optional capability a registry advertises when it
// can resolve a specific version of an entity name.
//
// Deliberately NOT part of Registry: that interface has a dozen
// implementations (test stubs, package-local fakes), and a registry with no
// concept of versions cannot hold two versions of a name, so Get is already
// unambiguous for it. Requiring the method would break every implementor to
// describe a capability most of them cannot exercise.
type VersionedRegistry interface {
	// GetVersioned retrieves the entity registered under name at exactly
	// version (the route-group prefix it was mounted at; "" for the
	// unversioned App.Entity registration).
	GetVersioned(name, version string) (*Entity, error)
}

// ResolveTarget resolves a relation target for a source entity, preferring the
// target registered at the SOURCE's own version.
//
// Relation resolution drives the Hidden-column scrub, the owner/tenant scopes,
// the soft-delete filter and the scoped-filter allow-list. Resolving by name
// alone picks the unversioned declaration when one exists, so a request under
// /api/v1 could inherit an internal entity's visibility and scoping rules
// instead of v1's — disclosing columns v1 marks hidden and returning rows v1's
// scopes exclude.
//
// Order: the source's own version first; then unversioned, which is the shared
// declaration a versioned entity legitimately points at; then a sole version
// when exactly one exists. Ambiguity is an ERROR, never a silent pick — the
// caller must fail closed.
func ResolveTarget(reg Registry, source *Entity, targetName string) (*Entity, error) {
	if reg == nil {
		return nil, fmt.Errorf("entity: no registry to resolve %q", targetName)
	}
	// A registry without the versioned capability cannot hold two versions of
	// a name, so Get is already unambiguous for it.
	vr, ok := reg.(VersionedRegistry)
	if !ok {
		return reg.Get(targetName)
	}
	if source != nil && source.Version != "" {
		if e, err := vr.GetVersioned(targetName, source.Version); err == nil {
			return e, nil
		}
	}
	if e, err := vr.GetVersioned(targetName, ""); err == nil {
		return e, nil
	}
	// Neither same-version nor unversioned: fall back to Get, which returns
	// the sole version when there is exactly one and an ambiguity error when
	// there are several. Propagate the error rather than choosing.
	return reg.Get(targetName)
}
