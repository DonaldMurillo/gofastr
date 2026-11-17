package entity

import (
	"fmt"
	"strings"
)

// Registry is the read side of the framework's entity registry.
type Registry interface {
	// All returns every registered entity keyed by name, in map order.
	All() map[string]*Entity

	// AllSorted returns every registered entity sorted by name.
	AllSorted() []*Entity

	// Get returns the entity named name, erroring when none or, with several
	// versions and none unversioned, when ambiguous. Use ResolveTarget for relations.
	Get(name string) (*Entity, error)
}

// VersionedRegistry is a Registry that can resolve one version of a name.
type VersionedRegistry interface {
	// GetVersioned returns the entity registered under name at exactly version ("" for unversioned).
	GetVersioned(name, version string) (*Entity, error)
}

// PivotEntity returns the entity owning a many_to_many's Through table, or nil, resolved
// by version as ResolveTarget does.
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

// TableSchemaSpellingDiffers reports whether a and b differ only in that one names a schema and the other does not.
func TableSchemaSpellingDiffers(a, b string) bool {
	ia, ib := strings.LastIndexByte(a, '.'), strings.LastIndexByte(b, '.')
	if (ia < 0) == (ib < 0) {
		return false
	}
	return strings.EqualFold(a[ia+1:], b[ib+1:])
}

// ResolveTarget resolves a relation target for source: its own version first,
// then unversioned, then a sole version. Ambiguity is an error.
func ResolveTarget(reg Registry, source *Entity, targetName string) (*Entity, error) {
	if reg == nil {
		return nil, fmt.Errorf("entity: no registry to resolve %q", targetName)
	}
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
	return reg.Get(targetName)
}
