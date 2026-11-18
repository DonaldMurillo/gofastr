package apiversions

import (
	"fmt"
	"maps"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Projection shapes one version's fields: Include allow-lists field names
// (empty keeps all), Exclude hides names after that, Rename sets wire keys.
type Projection struct {
	Version string
	Include []string
	Exclude []string
	Rename  map[string]string
}

// ProjectionSet groups multiple projections for an entity.
type ProjectionSet struct {
	Default  *Projection
	Versions map[string]*Projection
}

// NewProjectionSet keys each projection by its Version.
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

// ApplyToEntityConfig returns base with Hidden and WireName set by the version's
// projection; the columns never change, and an unknown field name panics.
func ApplyToEntityConfig(base entity.EntityConfig, ps *ProjectionSet, version string) entity.EntityConfig {
	p := ps.For(version)
	if p == nil {
		return base
	}
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
				continue
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
