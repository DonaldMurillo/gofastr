package apiversions

import (
	"fmt"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Projection defines which fields are visible in a specific API version.
type Projection struct {
	Version string

	// Include lists field names to include. If empty, all fields are included.
	Include []string

	// Exclude lists field names to exclude. Applied after Include.
	Exclude []string

	// Rename maps field names to different JSON keys for this version.
	Rename map[string]string
}

// ProjectionSet groups multiple projections for an entity.
type ProjectionSet struct {
	Default  *Projection
	Versions map[string]*Projection
}

// NewProjectionSet creates a projection set with the given version projections.
func NewProjectionSet(versions ...*Projection) *ProjectionSet {
	ps := &ProjectionSet{
		Versions: make(map[string]*Projection),
	}
	for _, p := range versions {
		ps.Versions[p.Version] = p
	}
	return ps
}

// For returns the projection for the given version, or the default.
func (ps *ProjectionSet) For(version string) *Projection {
	if ps == nil {
		return nil
	}
	if p, ok := ps.Versions[version]; ok {
		return p
	}
	return ps.Default
}

// ApplyToEntityConfig returns a modified EntityConfig with fields' visibility
// and wire names adjusted according to the projection for the given version.
// Does not mutate the original, and does NOT change the field set
// or the DB column names: Exclude hides a field (Hidden=true), Include hides
// everything not allow-listed, and Rename sets the wire-name override
// (WireName), the underlying table schema is identical across versions so
// two versions of one entity can share one table safely.
func ApplyToEntityConfig(base entity.EntityConfig, ps *ProjectionSet, version string) entity.EntityConfig {
	p := ps.For(version)
	if p == nil {
		return base
	}
	// The columns Define injects (id, timestamps, scope columns) are
	// columns of the entity too. A projection that excludes or renames one
	// declares it here, as Define would, so the adjustment lands on it;
	// Include shapes only the declared fields, so a version's Include
	// never hides a system column it did not name before.
	declared := make(map[string]bool, len(base.Fields))
	for _, f := range base.Fields {
		declared[f.Name] = true
	}
	full := base.WithSystemFields().Fields
	checkProjectionNames(full, p)

	adjusted := make([]schema.Field, 0, len(full))
	for _, f := range full {
		if declared[f.Name] {
			f.Hidden = f.Hidden || !shouldInclude(f.Name, p)
		} else {
			_, renamed := p.Rename[f.Name]
			if !renamed && !slices.Contains(p.Exclude, f.Name) {
				continue // Define injects it unchanged
			}
			f.Hidden = f.Hidden || slices.Contains(p.Exclude, f.Name)
		}
		if rename, ok := p.Rename[f.Name]; ok {
			f.WireName = rename
		}
		adjusted = append(adjusted, f)
	}

	cfg := base
	cfg.Fields = adjusted
	return cfg
}

func shouldInclude(fieldName string, p *Projection) bool {
	if slices.Contains(p.Exclude, fieldName) {
		return false
	}
	if len(p.Include) > 0 {
		return slices.Contains(p.Include, fieldName)
	}
	return true
}

// checkProjectionNames panics when p names a column the entity will not
// have, the way Version panics on a bad prefix: at startup, never in the
// request path. fields is the base config's Fields plus the columns
// Define injects. Exclude is how a version hides a column; a name that
// matched nothing (a typo, or the column's wire key instead of its field
// name) left that column on the version's wire output silently.
func checkProjectionNames(fields []schema.Field, p *Projection) {
	declared := make(map[string]bool, len(fields))
	for _, f := range fields {
		declared[f.Name] = true
	}
	check := func(kind, name string) {
		if !declared[name] {
			panic(fmt.Sprintf("apiversions: projection %q: %s names field %q, which the entity does not declare and Define does not inject (use the field name, not its wire key)", p.Version, kind, name))
		}
	}
	for _, n := range p.Include {
		check("Include", n)
	}
	for _, n := range p.Exclude {
		check("Exclude", n)
	}
	for _, n := range slices.Sorted(maps.Keys(p.Rename)) {
		check("Rename", n)
	}
}
