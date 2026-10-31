package entity

import (
	"fmt"
	"strings"
)

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

// PivotEntity returns the registered entity that owns a many_to_many's
// Through table, or nil when no entity does and the migrator creates the
// table itself. Tables match ignoring case, and a Through that differs from
// an entity's table only by naming a schema the other leaves off is an
// error (TableSchemaSpellingDiffers). Of several entities sharing the
// table it takes the source's own version, then the unversioned
// declaration, then the only one. Several other versions are an error, as
// in ResolveTarget: each can scope its rows differently, so picking one
// could read links another owner, tenant or soft delete hides.
func PivotEntity(reg Registry, source *Entity, table string) (*Entity, error) {
	if reg == nil || table == "" {
		return nil, nil
	}
	var unversioned *Entity
	var others []*Entity
	for _, e := range reg.AllSorted() {
		if !strings.EqualFold(e.GetTable(), table) {
			if TableSchemaSpellingDiffers(e.GetTable(), table) {
				return nil, fmt.Errorf("entity: many_to_many through table %q and entity %q's table %q may name one table with and without its schema: spell Through the way the join entity's Table spells it", table, e.GetName(), e.GetTable())
			}
			continue
		}
		if source != nil && source.Version != "" && e.Version == source.Version {
			return e, nil
		}
		if e.Version == "" && unversioned == nil {
			unversioned = e
		}
		others = append(others, e)
	}
	switch {
	case unversioned != nil:
		return unversioned, nil
	case len(others) == 1:
		return others[0], nil
	case len(others) > 1:
		return nil, fmt.Errorf("entity: pivot table %q is owned by %d versions and none matches the source's: declare it at the source's version or unversioned", table, len(others))
	}
	return nil, nil
}

// TableSchemaSpellingDiffers reports whether a and b differ only in that one
// names a schema and the other does not (public.memberships, memberships).
// Under the connection's search_path the two may be one table, so a string
// match would miss a join entity whose gates the loader then skips while it
// still joins the table. Two different schemas (a.m, b.m) are two tables.
func TableSchemaSpellingDiffers(a, b string) bool {
	ia, ib := strings.LastIndexByte(a, '.'), strings.LastIndexByte(b, '.')
	if (ia < 0) == (ib < 0) {
		return false
	}
	return strings.EqualFold(a[ia+1:], b[ib+1:])
}

// ResolveTarget resolves a relation target for a source entity, preferring the
// target registered at the SOURCE's own version.
//
// Relation resolution drives the Hidden-column scrub, the owner/tenant scopes,
// the soft-delete filter and the scoped-filter allow-list. Resolving by name
// alone picks the unversioned declaration when one exists, so a request under
// /api/v1 could inherit an internal entity's visibility and scoping rules
// instead of v1's, disclosing columns v1 marks hidden and returning rows v1's
// scopes exclude.
//
// Order: the source's own version first; then unversioned, which is the shared
// declaration a versioned entity legitimately points at; then a sole version
// when exactly one exists. Ambiguity is an ERROR, never a silent pick, the
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
