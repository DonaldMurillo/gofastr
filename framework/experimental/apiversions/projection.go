package apiversions

import (
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

// ApplyToEntityConfig returns a modified EntityConfig with fields filtered
// and renamed according to the projection for the given version.
// Does not mutate the original.
func ApplyToEntityConfig(base entity.EntityConfig, ps *ProjectionSet, version string) entity.EntityConfig {
	p := ps.For(version)
	if p == nil {
		return base
	}

	filtered := make([]schema.Field, 0, len(base.Fields))
	for _, f := range base.Fields {
		if !shouldInclude(f.Name, p) {
			continue
		}
		if rename, ok := p.Rename[f.Name]; ok {
			// Rename by creating a new field with the JSON name overridden.
			// The framework's JSON case logic uses Name, so we replace it
			// with the renamed version and keep the original as a reference.
			renamed := f
			renamed.Name = rename
			filtered = append(filtered, renamed)
		} else {
			filtered = append(filtered, f)
		}
	}

	cfg := base
	cfg.Fields = filtered
	return cfg
}

func shouldInclude(fieldName string, p *Projection) bool {
	for _, ex := range p.Exclude {
		if ex == fieldName {
			return false
		}
	}
	if len(p.Include) > 0 {
		for _, inc := range p.Include {
			if inc == fieldName {
				return true
			}
		}
		return false
	}
	return true
}

// VersionConfig bundles entity configuration for a specific API version.
type VersionConfig struct {
	Version string
	Config  entity.EntityConfig
}
